package triton

import (
	"strconv"
	"sync"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/hashicorp/nomad/drivers/shared/executor"
	"github.com/hashicorp/nomad/plugins/drivers"
)

// taskHandle should store all relevant runtime information
// such as process ID if this is a local task or other meta
// data if this driver deals with external APIs
type taskHandle struct {
	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	logger       hclog.Logger
	exec         executor.Executor
	pluginClient *plugin.Client
	taskConfig   *drivers.TaskConfig
	procState    drivers.TaskState
	startedAt    time.Time
	completedAt  time.Time
	exitResult   *drivers.ExitResult

	// extra relevant information about the task.
	tth        *TritonTaskHandler
	tritonTask *TritonTask
	waitCh     chan struct{}
}

func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	h.logger.Info("InsideTaskStatus")
	h.logger.Info("W00T", h.tritonTask.Instance.Brand)

	return &drivers.TaskStatus{
		ID:          h.taskConfig.ID,
		Name:        h.taskConfig.Name,
		State:       h.procState,
		StartedAt:   h.startedAt,
		CompletedAt: h.completedAt,
		ExitResult:  h.exitResult,
		DriverAttributes: map[string]string{
			"Brand":           h.tritonTask.Instance.Brand,
			"ComputeNode":     h.tritonTask.Instance.ComputeNode,
			"FirewallEnabled": strconv.FormatBool(h.tritonTask.Instance.FirewallEnabled),
			"Image":           h.tritonTask.Instance.Image,
			"Nname":           h.tritonTask.Instance.Name,
			"Package":         h.tritonTask.Instance.Package,
			"PrimaryIP":       h.tritonTask.Instance.PrimaryIP,
			"Type":            h.tritonTask.Instance.Type,
		},
	}
}

func (h *taskHandle) IsRunning() bool {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()
	return h.procState == drivers.TaskStateRunning
}

func (h *taskHandle) run() {
	h.stateLock.Lock()
	if h.exitResult == nil {
		h.exitResult = &drivers.ExitResult{}
	}
	h.stateLock.Unlock()

	// wait for your task to complete and upate its state.
	defer close(h.waitCh)

	h.logger.Info("in the run loop")
	//h.logger.Info(fmt.Sprintln(h.TaskStatus()))

	h.tth.GetInstStatus(h.tritonTask)

	h.logger.Info("returned from instStatus")

	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	h.procState = drivers.TaskStateExited
	h.completedAt = time.Now()
}
