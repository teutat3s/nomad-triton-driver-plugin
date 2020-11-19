package triton

import (
	"context"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/client/structs"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	pstructs "github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/joyent/triton-go"
	"github.com/joyent/triton-go/authentication"
)

// TaskState is the runtime state which is encoded in the handle returned to
// Nomad client.
// This information is needed to rebuild the task state and handler during
// recovery.
type TaskState struct {
	TaskConfig *drivers.TaskConfig
	StartedAt  time.Time
	// The plugin keeps track of its running tasks in a in-memory data
	// structure. If the plugin crashes, this data will be lost, so Nomad
	// will respawn a new instance of the plugin and try to restore its
	// in-memory representation of the running tasks using the RecoverTask()
	// method below.
	InstUUID string
}

// Driver is a nomad driver plugin for provisioning instances on triton
type Driver struct {
	// eventer is used to handle multiplexing of TaskEvents calls such that an
	// event can be broadcast to all callers
	eventer *eventer.Eventer

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

	// tritonClientInterface is the interface used for communicating with Joyent Triton
	client tritonClientInterface
}

// NewTritonDriver returns a new DriverPlugin implementation
func NewTritonDriver(logger hclog.Logger) drivers.DriverPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	logger = logger.Named(pluginName)

	return &Driver{
		eventer:        eventer.NewEventer(ctx, logger),
		config:         &DriverConfig{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
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

	client, err := d.newTritonClient(d.logger, d.eventer)
	if err != nil {
		return fmt.Errorf("failed to create Triton client: %v", err)
	}

	d.client = client

	return nil
}

func (d *Driver) newTritonClient(logger hclog.Logger, eventer *eventer.Eventer) (tritonClientInterface, error) {
	// Init the Triton Client
	keyID := triton.GetEnv("KEY_ID")
	accountName := triton.GetEnv("ACCOUNT")
	keyMaterial := triton.GetEnv("KEY_MATERIAL")
	userName := triton.GetEnv("USER")
	insecure := false
	if triton.GetEnv("INSECURE") != "" {
		insecure = true
	}

	var signer authentication.Signer
	var err error

	if keyMaterial == "" {
		input := authentication.SSHAgentSignerInput{
			KeyID:       keyID,
			AccountName: accountName,
			Username:    userName,
		}
		signer, err = authentication.NewSSHAgentSigner(input)
		if err != nil {
			log.Fatalf("Error Creating SSH Agent Signer: %s", err)
		}
	} else {
		var keyBytes []byte
		if _, err = os.Stat(keyMaterial); err == nil {
			keyBytes, err = ioutil.ReadFile(keyMaterial)
			if err != nil {
				log.Fatalf("Error reading key material from %s: %s",
					keyMaterial, err)
			}
			block, _ := pem.Decode(keyBytes)
			if block == nil {
				log.Fatalf(
					"Failed to read key material '%s': no key found", keyMaterial)
			}

			if block.Headers["Proc-Type"] == "4,ENCRYPTED" {
				log.Fatalf(
					"Failed to read key '%s': password protected keys are\n"+
						"not currently supported. Please decrypt the key prior to use.", keyMaterial)
			}

		} else {
			keyBytes = []byte(keyMaterial)
		}

		input := authentication.PrivateKeySignerInput{
			KeyID:              keyID,
			PrivateKeyMaterial: keyBytes,
			AccountName:        accountName,
			Username:           userName,
		}
		signer, err = authentication.NewPrivateKeySigner(input)
		if err != nil {
			log.Fatalf("Error Creating SSH Private Key Signer: %s", err)
		}
	}

	// Triton Client Config
	tritonConfig := &triton.ClientConfig{
		TritonURL:   triton.GetEnv("URL"),
		AccountName: accountName,
		Username:    userName,
		Signers:     []authentication.Signer{signer},
	}

	// Triton Docker Config
	dockerClient, err := docker.NewClientFromEnv()

	return tritonClient{
		tclient: &Client{
			Config:                tritonConfig,
			InsecureSkipTLSVerify: insecure,
			AffinityLock:          &sync.RWMutex{},
		},
		dclient: dockerClient,
		logger:  logger,
		eventer: eventer,
	}, nil

}

func (d *Driver) Shutdown(ctx context.Context) error {
	d.signalShutdown()
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
			ch <- d.buildFingerprint(ctx)
		}
	}
}

// buildFingerprint returns the driver's fingerprint data
func (d *Driver) buildFingerprint(ctx context.Context) *drivers.Fingerprint {
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
	attrs := map[string]*pstructs.Attribute{"driver.triton": pstructs.NewStringAttribute("1")}

	d.logger.Info("Inside buildFingerprint", d.config)
	if d.config.Enabled {
		if err := d.client.DescribeCluster(ctx); err != nil {
			d.logger.Info("Error on Describe")
			health = drivers.HealthStateUnhealthy
			desc = err.Error()
			attrs["driver.triton"] = pstructs.NewBoolAttribute(false)
		} else {
			d.logger.Info("pass on Describe")
			health = drivers.HealthStateHealthy
			desc = "Healthy"
			attrs["driver.triton"] = pstructs.NewBoolAttribute(true)
		}
	} else {
		health = drivers.HealthStateUndetected
		desc = "disabled"
	}

	return &drivers.Fingerprint{
		Attributes:        attrs,
		Health:            health,
		HealthDescription: desc,
	}
}

// StartTask returns a task handle and a driver network if necessary.
func (d *Driver) StartTask(cfg *drivers.TaskConfig) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	d.logger.Info("Inside StartTask")
	if !d.config.Enabled {
		return nil, nil, fmt.Errorf("disabled")
	}

	d.logger.Info("Showing TaskConfig", "TaskConfig", hclog.Fmt("%+v", cfg))

	if _, ok := d.tasks.Get(cfg.ID); ok {
		return nil, nil, fmt.Errorf("task with ID %q already started", cfg.ID)
	}

	var driverConfig TaskConfig
	if err := cfg.DecodeDriverConfig(&driverConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to decode driver config: %v", err)
	}

	d.logger.Info("starting triton task", "driver_cfg", hclog.Fmt("%+v", driverConfig))
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg

	// Create a Triton Task
	ctx, cancel := context.WithCancel(context.Background())
	instUUID, driverNetwork, err := d.client.RunTask(ctx, cfg, driverConfig)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed to start Triton instance: %v", err)
	}

	driverState := TaskState{
		TaskConfig: cfg,
		StartedAt:  time.Now(),
		InstUUID:   instUUID,
	}

	d.logger.Info("triton instance started", "instuuid", driverState.InstUUID, "started_at", driverState.StartedAt)

	h := newTaskHandle(ctx, cancel, d.logger, d.eventer, driverState, cfg, d.client)

	if err := handle.SetDriverState(&driverState); err != nil {
		d.logger.Error("failed to start task, error setting driver state", "error", err)
		h.stop(false)
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}

	d.tasks.Set(cfg.ID, h)

	go h.run()
	return handle, driverNetwork, nil
}

// RecoverTask recreates the in-memory state of a task from a TaskHandle.
func (d *Driver) RecoverTask(handle *drivers.TaskHandle) error {
	d.logger.Info("Inside RecoverTask")
	d.logger.Info("recovering triton instance", "version", handle.Version,
		"task_config.id", handle.Config.ID, "task_state", handle.State,
		"driver_state_bytes", len(handle.DriverState))
	if handle == nil {
		return fmt.Errorf("error: handle cannot be nil")
	}

	// If already attached to handle there's nothing to recover.
	if _, ok := d.tasks.Get(handle.Config.ID); ok {
		d.logger.Info("no triton instance to recover; task already exists",
			"task_id", handle.Config.ID,
			"task_name", handle.Config.Name,
		)
		return nil
	}

	// Handle doesn't already exist, try to reattach
	var taskState TaskState
	if err := handle.GetDriverState(&taskState); err != nil {
		d.logger.Error("failed to decode task state from handle", "error", err, "task_id", handle.Config.ID)
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	_, err := d.client.WaitForInstState(ctx, nil, taskState.InstUUID, tritonInstanceStatusRunning, 60, 5, false)
	if err != nil {
		d.logger.Error("failed to get instance state of running", "error", err, "task_id", handle.Config.ID)
		cancel()
		return fmt.Errorf("failed to get instance state of running: %v", err)
	}

	d.logger.Info("triton instance recovered", "instUUID", taskState.InstUUID,
		"started_at", taskState.StartedAt)

	h := newTaskHandle(ctx, cancel, d.logger, d.eventer, taskState, handle.Config, d.client)

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
	d.logger.Info("Inside handleWait")
	defer close(ch)
	// var result *drivers.ExitResult

	// driver specific logic to notify Nomad the task has been
	// completed and what was the exit result.
	//
	// When a result is sent in the result channel Nomad will stop the task and
	// emit an event that an operator can use to get an insight on why the task
	// stopped.

	var result *drivers.ExitResult
	select {
	case <-ctx.Done():
		return
	case <-d.ctx.Done():
		return
	case <-handle.doneCh:
		result = &drivers.ExitResult{
			ExitCode: handle.exitResult.ExitCode,
			Signal:   handle.exitResult.Signal,
			Err:      nil,
		}
	}

	select {
	case <-ctx.Done():
		return
	case <-d.ctx.Done():
		return
	case ch <- result:
	}
	d.logger.Info("Returned from handleWait")
}

// StopTask stops a running task with the given signal and within the timeout window.
func (d *Driver) StopTask(taskID string, timeout time.Duration, signal string) error {
	d.logger.Info("Inside StopTask")
	d.logger.Info("stopping triton instance", "task_id", taskID, "timeout", timeout, "signal", signal)
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
	// Detach if that's the signal, otherwise kill
	detach := signal == drivers.DetachSignal
	handle.stop(detach)

	// Wait for handle to finish
	select {
	case <-handle.doneCh:
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for triton task (id=%s) to stop (detach=%t)",
			taskID, detach)
	}

	d.logger.Info("triton task stopped", "task_id", taskID, "timeout", timeout,
		"signal", signal)
	return nil
}

// DestroyTask cleans up and removes a task that has terminated.
func (d *Driver) DestroyTask(taskID string, force bool) error {
	d.logger.Info("Inside DestroyTask")
	d.logger.Info("destroying triton task", "task_id", taskID, "force", force)
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

	// Safe to always kill here as detaching will have already happened
	handle.stop(false)

	if !handle.detach {
		d.logger.Info("In Handle.Detach")
		if err := handle.destroyTask(); err != nil {
			return err
		}
	}

	d.tasks.Delete(taskID)
	d.logger.Info("triton instance destroyed", "task_id", taskID, "force", force)
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
	d.logger.Info("sending triton instance stats", "task_id", taskID)
	_, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	// TODO: implement driver specific logic to send task stats.
	//
	// This function returns a channel that Nomad will use to listen for task
	// stats (e.g., CPU and memory usage) in a given interval. It should send
	// stats until the context is canceled or the task stops running.
	//
	// In the example below we use the Stats function provided by the executor,
	// but you can build a set of functions similar to the fingerprint process.
	// return handle.exec.Stats(ctx, interval)
	ch := make(chan *drivers.TaskResourceUsage)

	go func() {
		defer d.logger.Info("stopped sending triton instance stats", "task_id", taskID)
		defer close(ch)
		for {
			select {
			case <-time.After(interval):

				// Nomad core does not currently have any resource based
				// support for remote drivers. Once this changes, we may be
				// able to report actual usage here.
				//
				// This is required, otherwise the driver panics.
				ch <- &structs.TaskResourceUsage{
					ResourceUsage: &drivers.ResourceUsage{
						MemoryStats: &drivers.MemoryStats{},
						CpuStats:    &drivers.CpuStats{},
					},
					Timestamp: time.Now().UTC().UnixNano(),
				}
			case <-ctx.Done():
				return
			}

		}
	}()

	return ch, nil
}

// TaskEvents returns a channel that the plugin can use to emit task related events.
func (d *Driver) TaskEvents(ctx context.Context) (<-chan *drivers.TaskEvent, error) {
	d.logger.Info("retrieving task events")
	return d.eventer.TaskEvents(ctx)
}

// SignalTask forwards a signal to a task.
// This is an optional capability.
func (d *Driver) SignalTask(taskID string, signal string) error {
	d.logger.Info("Inside SignalTask")
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	// driver specific signal handling logic.
	//
	// The given signal must be forwarded to the target taskID. If this plugin
	// doesn't support receiving signals (capability SendSignals is set to
	// false) you can just return nil.
	instUUID := handle.instUUID

	switch signal {

	case handle.templateSignal:
		d.client.UploadTemplates(context.Background(), instUUID, handle.taskConfig)

	case "reboot":
		handle.rebooting = true
		if err := d.client.RebootTask(context.Background(), instUUID, handle.taskConfig); err != nil {
			handle.rebooting = false
			return err
		}
		handle.rebooting = false
	}

	return nil
}

// ExecTask returns the result of executing the given command inside a task.
// This is an optional capability.
func (d *Driver) ExecTask(taskID string, cmd []string, timeout time.Duration) (*drivers.ExecTaskResult, error) {
	d.logger.Info("Inside ExecTask")
	// driver specific logic to execute commands in a task.
	return nil, fmt.Errorf("Triton driver does not support exec")
}
