package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/task/models"
)

// resumeQueuedSessionsOnStartup gives queued work one delivery trigger after a
// backend restart. Every ordinary drain trigger (turn end, agent boot,
// enqueue, settling into WAITING_FOR_INPUT) belongs to a live runtime or a new
// message, so a primary session that was idle with queue rows when the backend
// stopped kept them indefinitely. For each such session this pass:
//   - continues a restart-interrupted turn from history (the queue drains when
//     that turn ends; a prompt with an unknown outcome is not re-sent);
//   - drains directly when the runtime survived the restart;
//   - otherwise resumes the runtime through the queue-drain ensure, whose boot
//     drains the queue and whose ceiling refusal is recorded for replay.
//
// It runs once, after the watcher is subscribed, and keeps every existing
// admission rule: paused queues, Office tasks, archived tasks, non-primary
// sessions, coordinator conversations, and parked dynamic routes stay untouched.
func (s *Service) resumeQueuedSessionsOnStartup(ctx context.Context) {
	if s.messageQueue == nil || s.repo == nil {
		return
	}
	refs, err := s.messageQueue.ListQueuedSessions(ctx)
	if err != nil {
		s.logger.Warn("startup queue delivery skipped: queued sessions could not be listed", zap.Error(err))
		return
	}
	sessions := make(map[string]string, len(refs))
	for _, ref := range refs {
		sessions[ref.SessionID] = ref.TaskID
	}
	if s.messageQueue.PendingQueueDispatchPersistenceAvailable() {
		// A dispatch whose pre-restart outcome is unknown is parked outside the
		// visible queue; its session still needs the recovery that releases it.
		if pending, pendingErr := s.messageQueue.ListPendingQueueDispatches(ctx); pendingErr == nil {
			for i := range pending {
				if _, ok := sessions[pending[i].Message.SessionID]; !ok {
					sessions[pending[i].Message.SessionID] = pending[i].Message.TaskID
				}
			}
		}
	}
	triggered := 0
	for sessionID := range sessions {
		if ctx.Err() != nil {
			return
		}
		if s.resumeQueuedSessionOnStartup(ctx, sessionID) {
			triggered++
		}
	}
	if triggered > 0 {
		s.logger.Info("startup queue delivery triggered queued sessions",
			zap.Int("sessions", triggered), zap.Int("candidates", len(sessions)))
	}
}

// queuedStartupCandidate loads a session the startup queue pass may trigger:
// an idle primary of a live, non-Office task. RUNNING/STARTING sessions drain
// at their turn end; CREATED, IDLE (Office between runs) and terminal sessions
// have their own owners.
func (s *Service) queuedStartupCandidate(ctx context.Context, sessionID string) (*models.Task, *models.TaskSession, bool) {
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil || session == nil || session.TaskID == "" ||
		!session.IsPrimary || session.State != models.TaskSessionStateWaitingForInput {
		return nil, nil, false
	}
	task, err := s.repo.GetTask(ctx, session.TaskID)
	if err != nil || task == nil || taskArchived(task) {
		return nil, nil, false
	}
	if isOffice, officeErr := s.lookupOfficeTask(ctx, task.ID); officeErr != nil || isOffice {
		return nil, nil, false
	}
	return task, session, true
}

func (s *Service) resumeQueuedSessionOnStartup(ctx context.Context, sessionID string) bool {
	task, session, ok := s.queuedStartupCandidate(ctx, sessionID)
	if !ok {
		return false
	}
	taskID := task.ID
	status := s.messageQueue.GetStatus(ctx, sessionID)
	if !status.AutoRun {
		return false
	}
	block, err := s.GetOpenSessionRecoveryBlock(ctx, sessionID)
	if err != nil {
		return false
	}
	if block != nil {
		if !interruptedTurnRecoveryReasons[block.Reason] {
			return false
		}
		if block.Reason != unknownQueueDispatchRecoveryReason && status.Count == 0 {
			return false
		}
		return s.AutoRecoverBlockedPrompt(ctx, taskID, sessionID, nil)
	}
	if status.Count == 0 {
		return false
	}
	if allowed, _ := s.autoResumeEligibility(ctx, task, session); !allowed {
		return false
	}
	if s.sessionRuntimeLive(ctx, sessionID) {
		s.tryFastPathDrainAfterEnqueue(ctx, taskID, sessionID)
		return true
	}
	s.scheduleAutoResumeForWorkflowQueue(ctx, sessionID, status.Entries[0].ID)
	return true
}
