package orchestrator

import (
	"context"
	"errors"
	"sync"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
)

var (
	errRestartStopBusy      = errors.New("session execution changed or is already stopping")
	errRestartStopUnsettled = errors.New("session inference ownership is still unsettled")
)

// stopLegacyForRestart awaits the observed execution before the caller can resume.
func (s *Service) stopLegacyForRestart(ctx context.Context, sessionID, reason string) error {
	if err := s.authorizeSessionControl(ctx, sessionID); err != nil {
		return err
	}
	release, err := s.acquireRestartStopAdmission(ctx, sessionID)
	if err != nil {
		return err
	}
	defer release()
	if s.agentManager == nil {
		return errRestartStopBusy
	}
	executionID, err := s.agentManager.GetExecutionIDForSession(ctx, sessionID)
	absent := errors.Is(err, agentruntime.ErrNoExecutionForSession) && executionID == ""
	if !absent && (err != nil || executionID == "") {
		return errors.Join(errRestartStopBusy, err)
	}
	if err := s.CheckRestartStopBindings(ctx, sessionID, executionID); err != nil {
		return err
	}
	if err := s.checkInferenceClosureAdmission(ctx, sessionID, false); err != nil {
		return err
	}
	if absent {
		return s.settleAbsentRestartStop(ctx, sessionID, err)
	}
	claim, owned := s.claimExecutionTeardownWithToken(sessionID, executionID, executionTeardownIntentGraceful)
	if !owned {
		return errRestartStopBusy
	}
	release()
	return s.awaitRestartStop(ctx, sessionID, executionID, reason, claim)
}

func (s *Service) acquireRestartStopAdmission(ctx context.Context, sessionID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	releaseLifecycle, acquired := s.tryAcquireSessionLifecycleLock(sessionID)
	if !acquired {
		return nil, errRestartStopBusy
	}
	guard, releaseGuard := s.acquireCancelInFlightGuard(sessionID)
	// A launch can hold the cancellation guard before requesting lifecycle.
	if !guard.TryLock() {
		releaseGuard()
		releaseLifecycle()
		return nil, errRestartStopBusy
	}
	release := sync.OnceFunc(func() {
		guard.Unlock()
		releaseGuard()
		releaseLifecycle()
	})
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (s *Service) settleAbsentRestartStop(ctx context.Context, sessionID string, absentErr error) error {
	if _, resuming := s.resumeAttemptStore().current(sessionID); resuming || s.isCancelInFlight(sessionID) {
		return errRestartStopBusy
	}
	// Settle a previously stopped turn without looking up another execution.
	s.resetTransientRetryWithContext(ctx, sessionID, true)
	if s.lspLeases != nil {
		s.lspLeases.StopLSPLeasesForSession(sessionID)
	}
	settled, err := s.settleExecutionLessSessionTurn(ctx, sessionID)
	if !settled && err == nil {
		return absentErr
	}
	return err
}

func (s *Service) awaitRestartStop(ctx context.Context, sessionID, executionID, reason string, claim executionTeardownClaim) error {
	stop, err := s.StopSessionAwaited(ctx, sessionID, executionID, reason)
	if err != nil {
		if stop == nil || !stop.Accepted {
			s.executionTeardownClaims.CompareAndDelete(terminalExecutionKey(sessionID, executionID), claim)
		}
		return err
	}
	if observation := stop.Observation; observation != nil && observation.Settlement != nil && observation.Settlement.Unresolved != 0 {
		return errRestartStopUnsettled
	}
	if verifier, ok := s.agentManager.(interface {
		VerifyRestartStopBindings(context.Context, string, string) error
	}); ok {
		return verifier.VerifyRestartStopBindings(ctx, sessionID, executionID)
	}
	return s.CheckRestartStopBindings(ctx, sessionID, executionID)
}

// CheckRestartStopBindings looks for unresolved ownership of the session and its execution.
func (s *Service) CheckRestartStopBindings(ctx context.Context, sessionID, executionID string) error {
	if s.sessionCeiling == nil {
		return nil
	}
	capacity := s.sessionCeiling
	capacity.mu.Lock()
	defer capacity.mu.Unlock()
	if capacity.inferenceStore == nil {
		return nil
	}
	bindings, err := capacity.inferenceStore.LoadInferenceBindings(ctx)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.SessionID == "" || binding.SessionID == sessionID ||
			executionID != "" && (binding.InstanceID == executionID || binding.ParentExecutionID == executionID) {
			return errRestartStopUnsettled
		}
	}
	return nil
}
