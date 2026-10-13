package orchestrator

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/system/hostmemory"
	"go.uber.org/zap"
)

// memoryPressureReader is the narrow host-memory seam the admission controller
// consults. It is satisfied by hostmemory.Reader.
type memoryPressureReader interface {
	Read(ctx context.Context) (hostmemory.Reading, error)
}

// errMemoryReadingIncomplete marks a usable reader that reported an incomplete
// reading (an unsupported platform, for example). It is not a host failure.
var errMemoryReadingIncomplete = errors.New("host memory reading is incomplete")

// memoryPressuredLocked reports whether the host is under memory pressure for a
// launch in the given lane. Only the worker lane is subject to the memory gate:
// the control lane and Office coordinator keep running so reclamation decisions
// do not stop with the work they manage. A disabled gate, a control-lane
// launch, or an unreadable/incomplete reading all report "not pressured" (fail
// open) so an unmeasurable host never stalls launches.
func (c *sessionCeilingController) memoryPressuredLocked(ctx context.Context, class ceilingClass) bool {
	if c == nil || !c.memoryGate.Enabled || c.memoryReader == nil || class != ceilingClassWorker {
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
// an operator sees why the guard is inactive without one WARN per admission.
func (c *sessionCeilingController) logMemoryUnavailableLocked(err error) {
	if c.memoryReadFailed {
		return
	}
	c.memoryReadFailed = true
	c.logger.Warn("session memory guard could not read host memory; failing open", zap.Error(err))
}
