package orchestrator

import (
	"context"
	"errors"

	"github.com/kandev/kandev/internal/task/models"
)

// settleExecutionLessSessionStop handles a stop for a session with no live
// execution. A settled or terminal session (or a missing row) is settled by
// closing its open turn, keeping the session row and state. Any other state may
// still be registering its execution, so it is not settled and the caller keeps
// the original not-found error. A turn lookup or settlement failure is returned.
//
// The open turn is captured before the session state is read, and is closed
// through the expected-turn settlement, so a launch or successor turn that
// appears concurrently fails closed instead of being swept.
func (s *Service) settleExecutionLessSessionStop(ctx context.Context, sessionID string) (bool, error) {
	var turn *models.Turn
	if s.turnService != nil {
		active, err := s.turnService.GetActiveTurn(ctx, sessionID)
		switch {
		case err == nil:
			turn = active
		case !isNoActiveTurnError(err):
			return false, err
		}
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if errors.Is(err, models.ErrTaskSessionNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if session == nil {
		return true, nil
	}
	switch session.State {
	case models.TaskSessionStateWaitingForInput,
		models.TaskSessionStateCompleted,
		models.TaskSessionStateFailed,
		models.TaskSessionStateCancelled:
	default:
		return false, nil
	}
	if turn == nil {
		return true, nil
	}
	if err := s.completeTurnForTaskSessionCheckedOwned(ctx, session.TaskID, sessionID, turn.ID); err != nil {
		return false, err
	}
	return true, nil
}
