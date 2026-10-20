package orchestrator

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/system/hostmemory"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
)

// ceilingReasonMemoryPressure refuses a launch that would add an agent process
// while the host is low on available memory or above its commit ceiling. It is
// distinct from the session-count reasons so an operator can tell a host
// resource limit from a count limit.
const ceilingReasonMemoryPressure = "memory_pressure"

// memoryPressureReader is the narrow host-memory seam the admission controller
// consults. It is satisfied by hostmemory.Reader.
type memoryPressureReader interface {
	Read(ctx context.Context) (hostmemory.Reading, error)
}

// memoryGateExemption reports whether a task's launches are exempt from the
// host-memory condition. Office coordinator conversations are exempt so the
// agent that manages the other sessions keeps running.
type memoryGateExemption func(ctx context.Context, taskID string) bool

// errMemoryReadingIncomplete marks a usable reader that reported an incomplete
// reading (an unsupported platform, for example). It is not a host failure.
var errMemoryReadingIncomplete = errors.New("host memory reading is incomplete")

// configureMemoryGate enables the host-memory condition on the controller. A
// disabled gate leaves the reader nil, so the host is never read.
func (c *sessionCeilingController) configureMemoryGate(
	gate hostmemory.Gate, reader memoryPressureReader, exempt memoryGateExemption,
) {
	if c == nil || !gate.Enabled || reader == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memoryGate = gate
	c.memoryReader = reader
	c.memoryExempt = exempt
}

// memoryPressuredLocked reports whether the host is under memory pressure for
// this launch. A disabled gate, an exempt task, or an unreadable or incomplete
// reading report "not pressured" (fail open) so an unmeasurable host never
// stalls launches.
func (c *sessionCeilingController) memoryPressuredLocked(ctx context.Context, req admissionRequest) bool {
	if c == nil || !c.memoryGate.Enabled || c.memoryReader == nil {
		return false
	}
	if c.memoryExempt != nil && c.memoryExempt(ctx, req.taskID) {
		return false
	}
	reading, err := c.memoryReader.Read(ctx)
	if err != nil {
		c.logMemoryUnavailableLocked(err)
		return false
	}
	pressured, unknown := c.memoryGate.Pressured(reading)
	if unknown {
		c.logMemoryUnavailableLocked(errMemoryReadingIncomplete)
		return false
	}
	c.memoryReadFailed = false
	return pressured
}

// logMemoryUnavailableLocked reports the first failure of a failure streak so
// an operator sees why the guard is inactive without one warning per admission.
func (c *sessionCeilingController) logMemoryUnavailableLocked(err error) {
	if c.memoryReadFailed {
		return
	}
	c.memoryReadFailed = true
	c.logger.Warn("session memory guard could not read host memory; failing open", zap.Error(err))
}

// coordinatorTaskMemoryExemption exempts Office coordinator conversations from
// the host-memory condition. A task that cannot be read is not exempt.
func (s *Service) coordinatorTaskMemoryExemption(ctx context.Context, taskID string) bool {
	if s == nil || s.repo == nil || taskID == "" {
		return false
	}
	task, err := s.repo.GetTask(ctx, taskID)
	return err == nil && task != nil && task.Origin == models.TaskOriginCoordinator
}
