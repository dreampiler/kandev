package dynamic

import "github.com/kandev/kandev/internal/agent/runtime/routingerr"

func accountBlockKey(executionProfileID string) string {
	if executionProfileID == "" {
		return ""
	}
	return ResourceKey(ScopeAccountBlock, executionProfileID)
}

// HoldAccountBlock keeps one execution profile out of every later selection
// until its owner releases it. Credential, model, and other profiles'
// circuits are never read or changed.
func (e *Engine) HoldAccountBlock(executionProfileID string, code routingerr.Code) {
	if e == nil || e.circuits == nil {
		return
	}
	e.circuits.Hold(accountBlockKey(executionProfileID), code)
}

// ReleaseAccountBlock closes the owner-release hold of one execution profile
// and reports whether a hold was closed.
func (e *Engine) ReleaseAccountBlock(executionProfileID string) (bool, error) {
	if e == nil || e.circuits == nil {
		return false, nil
	}
	return e.circuits.ReleaseHold(accountBlockKey(executionProfileID))
}
