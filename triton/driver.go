package triton

import (
	"context"
	"fmt"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	"github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/joyent/triton-go/compute"
	"github.com/joyent/triton-go/network"
)

const (
	// pluginName is the name of the plugin
	// this is used for logging and (along with the version) for uniquely
	// identifying plugin binaries fingerprinted by the client
	pluginName = "triton"

	// pluginVersion allows the client to identify and use newer versions of
	// an installed plugin
	pluginVersion = "v0.1.0"

	// fingerprintPeriod is the interval at which the plugin will send
	// fingerprint responses
	fingerprintPeriod = 30 * time.Second

	// taskHandleVersion is the version of task handle which this plugin sets
	// and understands how to decode
	// this is used to allow modification and migration of the task schema
	// used by the plugin
	taskHandleVersion = 1
)

var (
	// pluginInfo describes the plugin
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDriver,
		PluginApiVersions: []string{drivers.ApiVersion010},
		PluginVersion:     pluginVersion,
		Name:              pluginName,
	}

	// capabilities indicates what optional features this driver supports
	// this should be set according to the target run time.
	capabilities = &drivers.Capabilities{
		// The plugin's capabilities signal Nomad which extra functionalities
		// are supported. For a list of available options check the docs page:
		// https://godoc.org/github.com/hashicorp/nomad/plugins/drivers#Capabilities
		SendSignals: false,
		Exec:        false,
		FSIsolation: drivers.FSIsolationImage,
	}
)

// TaskState is the runtime state which is encoded in the handle returned to
// Nomad client.
// This information is needed to rebuild the task state and handler during
// recovery.
type TaskState struct {
	ReattachConfig *structs.ReattachConfig
	TaskConfig     *drivers.TaskConfig
	StartedAt      time.Time

	// The plugin keeps track of its running tasks in a in-memory data
	// structure. If the plugin crashes, this data will be lost, so Nomad
	// will respawn a new instance of the plugin and try to restore its
	// in-memory representation of the running tasks using the RecoverTask()
	// method below.
	APIType      string
	InstanceID   string
	FWRules      []string
	ExitStrategy string
}

// Driver is a nomad driver plugin for provisioning instances on triton
type Driver struct {
	// config is the plugin configuration set by the SetConfig RPC
	config *DriverConfig

	// nomadConfig is the client config from Nomad
	nomadConfig *base.ClientDriverConfig

	// tasks is the in memory datastore mapping taskIDs to driver handles
	tasks *taskStore

	// ctx is the context for the driver. It is passed to other subsystems to
	// coordinate shutdown
	ctx context.Context

	// signalShutdown is called when the driver is shutting down and cancels
	// the ctx passed to any subsystems
	signalShutdown context.CancelFunc

	// logger will log to the plugin output which is usually an 'executor.out'
	// file located in the root of the TaskDir
	logger hclog.Logger

	// tth encapsulates triton task remote calls
	tth *TritonTaskHandler
}

// NewTritonDriver returns a new DriverPlugin implementation
func NewTritonDriver(logger hclog.Logger) drivers.DriverPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	logger = logger.Named(pluginName)

	return &Driver{
		config:         &DriverConfig{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
		tth:            NewTritonTaskHandler(logger),
	}
}

// PluginInfo returns information describing the plugin.
func (d *Driver) PluginInfo() (*base.PluginInfoResponse, error) {
	return pluginInfo, nil
}

// ConfigSchema returns the plugin configuration schema.
func (d *Driver) ConfigSchema() (*hclspec.Spec, error) {
	return configSpec, nil
}

// SetConfig is called by the client to pass the configuration for the plugin.
func (d *Driver) SetConfig(cfg *base.Config) error {
	d.logger.Info("Inside SetConfig")

	var config DriverConfig
	if len(cfg.PluginConfig) != 0 {
		if err := base.MsgPackDecode(cfg.PluginConfig, &config); err != nil {
			return err
		}
	}

	// Save the configuration to the plugin
	d.config = &config

	// parse and validate any configuration value if necessary.
	//
	// If your driver agent configuration requires any complex validation
	// (some dependency between attributes) or special data parsing (the
	// string "10s" into a time.Interval) you can do it here and update the
	// value in d.config.
	//
	// In the example below we check if the shell specified by the user is
	// supported by the plugin.
	//shell := d.config.Shell
	//if shell != "bash" && shell != "fish" {
	//	return fmt.Errorf("invalid shell %s", d.config.Shell)
	//}

	// Save the Nomad agent configuration
	if cfg.AgentConfig != nil {
		d.nomadConfig = cfg.AgentConfig.Driver
	}

	// initialize any extra requirements if necessary.
	//
	// Here you can use the config values to initialize any resources that are
	// shared by all tasks that use this driver, such as a daemon process.

	return nil
}

// TaskConfigSchema returns the HCL schema for the configuration of a task.
func (d *Driver) TaskConfigSchema() (*hclspec.Spec, error) {
	d.logger.Info("Inside TaskConfigSchema")
	return taskConfigSpec, nil
}

// Capabilities returns the features supported by the driver.
func (d *Driver) Capabilities() (*drivers.Capabilities, error) {
	return capabilities, nil
}

// Fingerprint returns a channel that will be used to send health information
// and other driver specific node attributes.
func (d *Driver) Fingerprint(ctx context.Context) (<-chan *drivers.Fingerprint, error) {
	ch := make(chan *drivers.Fingerprint)
	go d.handleFingerprint(ctx, ch)
	return ch, nil
}

// handleFingerprint manages the channel and the flow of fingerprint data.
func (d *Driver) handleFingerprint(ctx context.Context, ch chan<- *drivers.Fingerprint) {
	defer close(ch)

	// Nomad expects the initial fingerprint to be sent immediately
	ticker := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			// after the initial fingerprint we can set the proper fingerprint
			// period
			ticker.Reset(fingerprintPeriod)
			ch <- d.buildFingerprint()
		}
	}
}

// buildFingerprint returns the driver's fingerprint data
func (d *Driver) buildFingerprint() *drivers.Fingerprint {
	// fp := &drivers.Fingerprint{
	// 	Attributes:        map[string]*structs.Attribute{},
	// 	Health:            drivers.HealthStateHealthy,
	// 	HealthDescription: drivers.DriverHealthy,
	// }

	// // TODO: implement fingerprinting logic to populate health and driver
	// // attributes.
	// //
	// // Fingerprinting is used by the plugin to relay two important information
	// // to Nomad: health state and node attributes.
	// //
	// // If the plugin reports to be unhealthy, or doesn't send any fingerprint
	// // data in the expected interval of time, Nomad will restart it.
	// //
	// // Node attributes can be used to report any relevant information about
	// // the node in which the plugin is running (specific library availability,
	// // installed versions of a software etc.). These attributes can then be
	// // used by an operator to set job constrains.
	// //
	// // In the example below we check if the shell specified by the user exists
	// // in the node.
	// shell := d.config.Shell

	// cmd := exec.Command("which", shell)
	// if err := cmd.Run(); err != nil {
	// 	return &drivers.Fingerprint{
	// 		Health:            drivers.HealthStateUndetected,
	// 		HealthDescription: fmt.Sprintf("shell %s not found", shell),
	// 	}
	// }

	// // We also set the shell and its version as attributes
	// cmd = exec.Command(shell, "--version")
	// if out, err := cmd.Output(); err != nil {
	// 	d.logger.Warn("failed to find shell version: %v", err)
	// } else {
	// 	re := regexp.MustCompile("[0-9]\\.[0-9]\\.[0-9]")
	// 	version := re.FindString(string(out))

	// 	fp.Attributes["driver.hello.shell_version"] = structs.NewStringAttribute(version)
	// 	fp.Attributes["driver.hello.shell"] = structs.NewStringAttribute(shell)
	// }

	// return fp

	health := drivers.HealthStateHealthy
	desc := "ready"
	attrs := map[string]*structs.Attribute{"driver.triton": structs.NewStringAttribute("1")}

	return &drivers.Fingerprint{
		Attributes:        attrs,
		Health:            health,
		HealthDescription: desc,
	}
}

// StartTask returns a task handle and a driver network if necessary.
func (d *Driver) StartTask(cfg *drivers.TaskConfig) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	d.logger.Info("Inside StartTask")
	if _, ok := d.tasks.Get(cfg.ID); ok {
		return nil, nil, fmt.Errorf("task with ID %q already started", cfg.ID)
	}

	var driverConfig TaskConfig
	if err := cfg.DecodeDriverConfig(&driverConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to decode driver config: %v", err)
	}

	// Assert we have either docker_api or cloud_api
	switch driverConfig.APIType {
	case "docker_api":
		break
	case "cloud_api":
		break
	default:
		return nil, nil, fmt.Errorf("Must supply an api_type of either docker_api or cloud_api")
	}

	switch driverConfig.ExitStrategy {
	case "stopped":
		break
	case "deleted":
		break
		// Default to stopped
	case "":
		driverConfig.ExitStrategy = "stopped"
		break
	default:
		return nil, nil, fmt.Errorf("Must supply an exit_strategy of either stopped or deleted")
	}

	d.logger.Info("starting triton task", "driver_cfg", hclog.Fmt("%+v", driverConfig))
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg

	// driver specific mechanism to start the task.
	//
	// Once the task is started you will need to store any relevant runtime
	// information in a taskHandle and TaskState. The taskHandle will be
	// stored in-memory in the plugin and will be used to interact with the
	// task.
	//
	// The TaskState will be returned to the Nomad client inside a
	// drivers.TaskHandle instance. This TaskHandle will be sent back to plugin
	// if the task ever needs to be recovered, so the TaskState should contain
	// enough information to handle that.
	//
	// In the example below we use an executor to fork a process to run our
	// greeter. The executor is then stored in the handle so we can access it
	// later and the the plugin.Client is used to generate a reattach
	// configuration that can be used to recover communication with the task.

	// Create a Triton Task
	tt, err := d.tth.NewTritonTask(cfg, driverConfig)
	if err != nil {
		return nil, nil, err
	}
	d.logger.Info("W00T_PLUGINSTANCE", tt.Instance)

	var fwruleids []string
	for _, v := range tt.FWRules {
		fwruleids = append(fwruleids, v.ID)
	}

	h := &taskHandle{
		tth:        d.tth,
		taskConfig: cfg,
		tritonTask: tt,
		procState:  drivers.TaskStateRunning,
		startedAt:  time.Now().Round(time.Millisecond),
		logger:     d.logger,
		waitCh:     make(chan struct{}),
	}

	d.logger.Info("W00T_PLUGINIP", tt.Instance.PrimaryIP)

	n := &drivers.DriverNetwork{
		IP:            tt.Instance.PrimaryIP,
		AutoAdvertise: true,
	}

	driverState := TaskState{
		APIType:      driverConfig.APIType,
		InstanceID:   tt.Instance.ID,
		FWRules:      fwruleids,
		TaskConfig:   cfg,
		StartedAt:    h.startedAt,
		ExitStrategy: tt.ExitStrategy,
	}

	//d.logger.Info(fmt.Sprintf("DRIVERSTATE: %s", driverState))

	if err := handle.SetDriverState(&driverState); err != nil {
		d.logger.Error("failed to start task, error setting driver state", "error", err)
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}

	d.tasks.Set(cfg.ID, h)

	go h.run()

	d.logger.Info("W00T_NETWORK", n)
	return handle, n, nil
}

// RecoverTask recreates the in-memory state of a task from a TaskHandle.
func (d *Driver) RecoverTask(handle *drivers.TaskHandle) error {
	d.logger.Info("Inside RecoverTask")
	if handle == nil {
		return fmt.Errorf("error: handle cannot be nil")
	}

	if _, ok := d.tasks.Get(handle.Config.ID); ok {
		return nil
	}

	var taskState TaskState
	if err := handle.GetDriverState(&taskState); err != nil {
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	var driverConfig TaskConfig
	if err := taskState.TaskConfig.DecodeDriverConfig(&driverConfig); err != nil {
		return fmt.Errorf("failed to decode driver config: %v", err)
	}

	// driver specific logic to recover a task.
	//
	// Recovering a task involves recreating and storing a taskHandle as if the
	// task was just started.
	//
	// In the example below we use the executor to re-attach to the process
	// that was created when the task first started.

	// Build Context
	ctx := context.Background()
	sctx, cancel := context.WithCancel(ctx)

	// Instance
	c, err := d.tth.client.Compute()
	if err != nil {
		return err
	}

	pi, err := c.Instances().Get(sctx, &compute.GetInstanceInput{ID: taskState.InstanceID})
	if err != nil {
		return err
	}

	n, err := d.tth.client.Network()
	if err != nil {
		return err
	}

	// FWRules
	var fwrules []*network.FirewallRule
	for _, v := range taskState.FWRules {
		pr, err := n.Firewall().GetRule(sctx, &network.GetRuleInput{
			ID: v,
		})
		if err != nil {
			return err
		}
		fwrules = append(fwrules, pr)
	}

	tt := &TritonTask{
		Instance:     pi,
		Ctx:          sctx,
		Shutdown:     cancel,
		FWRules:      fwrules,
		ExitStrategy: taskState.ExitStrategy,
	}

	h := &taskHandle{
		tth:        d.tth,
		taskConfig: taskState.TaskConfig,
		tritonTask: tt,
		procState:  drivers.TaskStateRunning,
		startedAt:  taskState.StartedAt,
		logger:     d.logger,
		waitCh:     make(chan struct{}),
	}

	d.tasks.Set(taskState.TaskConfig.ID, h)

	go h.run()

	return nil
}

// WaitTask returns a channel used to notify Nomad when a task exits.
func (d *Driver) WaitTask(ctx context.Context, taskID string) (<-chan *drivers.ExitResult, error) {
	d.logger.Info("Inside WaitTask")
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	ch := make(chan *drivers.ExitResult)
	go d.handleWait(ctx, handle, ch)
	return ch, nil
}

func (d *Driver) handleWait(ctx context.Context, handle *taskHandle, ch chan *drivers.ExitResult) {
	defer close(ch)
	// var result *drivers.ExitResult

	// driver specific logic to notify Nomad the task has been
	// completed and what was the exit result.
	//
	// When a result is sent in the result channel Nomad will stop the task and
	// emit an event that an operator can use to get an insight on why the task
	// stopped.

	<-handle.waitCh
	ch <- handle.exitResult
}

// StopTask stops a running task with the given signal and within the timeout window.
func (d *Driver) StopTask(taskID string, timeout time.Duration, signal string) error {
	d.logger.Info("Inside StopTask")
	d.logger.Info("TIMEOUT_W00t", timeout)
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	// driver specific logic to stop a task.
	//
	// The StopTask function is expected to stop a running task by sending the
	// given signal to it. If the task does not stop during the given timeout,
	// the driver must forcefully kill the task.
	//
	// In the example below we let the executor handle the task shutdown
	// process for us, but you might need to customize this for your own
	// implementation.
	if err := d.tth.ShutdownInstance(handle.tritonTask); err != nil {
		return fmt.Errorf("triton ShutdownInstance failed: %v", err)
	}

	return nil
}

// DestroyTask cleans up and removes a task that has terminated.
func (d *Driver) DestroyTask(taskID string, force bool) error {
	d.logger.Info("Inside DestroyTask")
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	if handle.IsRunning() && !force {
		return fmt.Errorf("cannot destroy running task")
	}

	// driver specific logic to destroy a complete task.
	//
	// Destroying a task includes removing any resources used by task and any
	// local references in the plugin. If force is set to true the task should
	// be destroyed even if it's currently running.

	// grace period is chosen arbitrary here
	if err := d.tth.DestroyTritonTask(handle, force); err != nil {
		handle.logger.Error("failed to destroy executor", "err", err)
	}

	d.tasks.Delete(taskID)
	return nil
}

// InspectTask returns detailed status information for the referenced taskID.
func (d *Driver) InspectTask(taskID string) (*drivers.TaskStatus, error) {
	d.logger.Info("Inside InspectTask")
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	return handle.TaskStatus(), nil
}

// TaskStats returns a channel which the driver should send stats to at the given interval.
func (d *Driver) TaskStats(ctx context.Context, taskID string, interval time.Duration) (<-chan *drivers.TaskResourceUsage, error) {
	d.logger.Info("Inside TaskStats")
	// handle, ok := d.tasks.Get(taskID)
	// if !ok {
	// 	return nil, drivers.ErrTaskNotFound
	// }

	// TODO: implement driver specific logic to send task stats.
	//
	// This function returns a channel that Nomad will use to listen for task
	// stats (e.g., CPU and memory usage) in a given interval. It should send
	// stats until the context is canceled or the task stops running.
	//
	// In the example below we use the Stats function provided by the executor,
	// but you can build a set of functions similar to the fingerprint process.
	// return handle.exec.Stats(ctx, interval)
	return nil, nil
}

// TaskEvents returns a channel that the plugin can use to emit task related events.
func (d *Driver) TaskEvents(ctx context.Context) (<-chan *drivers.TaskEvent, error) {
	d.logger.Info("Inside TaskEvents")
	return make(chan *drivers.TaskEvent), nil
}

// SignalTask forwards a signal to a task.
// This is an optional capability.
func (d *Driver) SignalTask(taskID string, signal string) error {
	d.logger.Info("Inside SignalTask")
	// handle, ok := d.tasks.Get(taskID)
	// if !ok {
	// 	return drivers.ErrTaskNotFound
	// }

	// driver specific signal handling logic.
	//
	// The given signal must be forwarded to the target taskID. If this plugin
	// doesn't support receiving signals (capability SendSignals is set to
	// false) you can just return nil.

	return nil
}

// ExecTask returns the result of executing the given command inside a task.
// This is an optional capability.
func (d *Driver) ExecTask(taskID string, cmd []string, timeout time.Duration) (*drivers.ExecTaskResult, error) {
	d.logger.Info("Inside ExecTask")
	// driver specific logic to execute commands in a task.
	return nil, nil
}
