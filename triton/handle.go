package triton

import (
	"context"
	"fmt"
	"sync"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/client/stats"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/mitchellh/mapstructure"
)

const (
	tritonInstanceStatusDeleted     = "deleted"
	tritonInstanceStatusFailed      = "failed"
	tritonInstanceStatusProvisining = "provisining"
	tritonInstanceStatusRunning     = "running"
	tritonInstanceStatusStopped     = "stopped"
	tritonInstanceStatusStopping    = "stopping"
	tritonInstanceStatusUnknown     = "unkown"
)

type taskHandle struct {
	instUUID     string
	logger       hclog.Logger
	eventer      *eventer.Eventer
	tritonClient tritonClientInterface

	totalCpuStats  *stats.CpuStats
	userCpuStats   *stats.CpuStats
	systemCpuStats *stats.CpuStats

	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	templateSignal string
	taskConfig     *drivers.TaskConfig
	procState      drivers.TaskState
	startedAt      time.Time
	completedAt    time.Time
	exitResult     *drivers.ExitResult
	doneCh         chan struct{}

	// detach from ecs task instead of killing it if true.
	detach    bool
	rebooting bool

	ctx    context.Context
	cancel context.CancelFunc
}

func newTaskHandle(ctx context.Context, cancel context.CancelFunc, logger hclog.Logger, eventer *eventer.Eventer, ts TaskState, taskConfig *drivers.TaskConfig, tritonClient tritonClientInterface) *taskHandle {
	logger = logger.Named("handle").With("instuuid", ts.InstUUID)
	templateSignal := "unset"

	if len(taskConfig.Templates) > 0 {
		templateSignal = taskConfig.Templates[0].ChangeSignal
	}

	h := &taskHandle{
		instUUID:       ts.InstUUID,
		tritonClient:   tritonClient,
		taskConfig:     taskConfig,
		procState:      drivers.TaskStateRunning,
		startedAt:      ts.StartedAt,
		exitResult:     &drivers.ExitResult{},
		logger:         logger,
		eventer:        eventer,
		templateSignal: templateSignal,
		doneCh:         make(chan struct{}),
		detach:         false,
		rebooting:      false,
		ctx:            ctx,
		cancel:         cancel,
	}

	return h
}

func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	return &drivers.TaskStatus{
		ID:          h.taskConfig.ID,
		Name:        h.taskConfig.Name,
		State:       h.procState,
		StartedAt:   h.startedAt,
		CompletedAt: h.completedAt,
		ExitResult:  h.exitResult,
		DriverAttributes: map[string]string{
			"instuuid": h.instUUID,
		},
	}
}

func (h *taskHandle) IsRunning() bool {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()
	return h.procState == drivers.TaskStateRunning
}

func (h *taskHandle) run() {
	ExitCode := 0
	defer close(h.doneCh)
	h.stateLock.Lock()
	if h.exitResult == nil {
		h.exitResult = &drivers.ExitResult{}
	}
	h.stateLock.Unlock()
	// prevStatus is used for Emitting Status changes.
	prevStatus := tritonInstanceStatusUnknown

	// Block until stopped.
	for h.ctx.Err() == nil {
		select {
		case <-time.After(5 * time.Second):
			h.logger.Info("Polling Instance")
			status, err := h.tritonClient.DescribeTaskStatus(h.ctx, h.instUUID)
			if prevStatus != status {
				annotations := make(map[string]string)
				err := mapstructure.Decode(&StateChange{
					PreviousState: prevStatus,
					CurrentState:  status,
					InstanceUUID:  h.instUUID,
				}, &annotations)
				if err != nil {
					h.logger.Error("Failed to Decode Annotations", err)
				}

				h.eventer.EmitEvent(
					&drivers.TaskEvent{
						TaskID:         h.taskConfig.ID,
						TaskName:       h.taskConfig.Name,
						AllocID:        h.taskConfig.AllocID,
						Timestamp:      time.Now(),
						Message:        fmt.Sprintf("StateChange"),
						DisplayMessage: fmt.Sprintf("Instance: %s, StatusChange from %s to %s.", h.instUUID, prevStatus, status),
						Annotations:    annotations,
					})

				prevStatus = status
			}
			if err != nil {
				h.handleRunError(err, "failed to find Triton Instance")
				return
			}

			// Triton instance has terminal status phase, meaning the task is going to
			// stop. If we are in this phase, the driver should exit and pass
			// this to the servers so that a new allocation, and Triton task can
			// be started.
			h.logger.Info("Instance Status: %s", status)
			if !h.rebooting {
				if status == tritonInstanceStatusStopped {
					// If we must get the for the job to exit and get its exit code
					if h.taskConfig.JobType == JobTypeBatch {
						ec, err := h.tritonClient.DockerExitCode(h.ctx, h.instUUID)
						if err != nil {
							h.handleRunError(err, "Couldn't Get ExitCode")
						}
						ExitCode = ec
						h.ctx.Done()
						goto EXITCODES
					}
					h.handleRunError(fmt.Errorf("Triton instance status in terminal phase"), "task status: "+status)
					return
				}
			}

		case <-h.ctx.Done():
			h.logger.Info("Inside run(ctx.Done())")
		}
	}

	// Only stop task if we're not detaching.
	if !h.detach {
		if err := h.stopTask(); err != nil {
			h.handleRunError(err, "failed to stop Triton instance correctly")
			return
		}
	}

EXITCODES:
	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	h.procState = drivers.TaskStateExited
	h.exitResult.ExitCode = ExitCode
	h.exitResult.Signal = 0
	h.completedAt = time.Now()
}

func (h *taskHandle) stop(detach bool) {
	h.logger.Info("Inside stop(detach)")
	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	// Only allow transitioning from not-detaching to detaching.
	if !h.detach && detach {
		h.detach = detach
	}
	h.cancel()
}

// handleRunError is a convenience function to easily and correctly handle
// terminal errors during the task run lifecycle.
func (h *taskHandle) handleRunError(err error, context string) {
	h.stateLock.Lock()
	h.completedAt = time.Now()
	h.exitResult.ExitCode = 1
	h.exitResult.Err = fmt.Errorf("%s: %v", context, err)
	h.stateLock.Unlock()
}

// stopTask is used to stop the Triton instance, and monitor its status until it
// reaches the stopped state.
func (h *taskHandle) stopTask() error {
	h.logger.Info("Inside stopTask()")
	if err := h.tritonClient.StopTask(context.TODO(), h.instUUID); err != nil {
		return err
	}
	return nil
}

// stopTask is used to stop the Triton instance, and monitor its status until it
// reaches the stopped state.
func (h *taskHandle) destroyTask() error {
	h.logger.Info("Inside destroyTask")
	if err := h.tritonClient.DestroyTask(context.TODO(), h.instUUID); err != nil {
		return err
	}

	h.logger.Info("triton instance has successfully been destroyed")
	return nil
}
