package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
)

// primaryCandidateRank orders sessions for an automatic primary promotion.
// The ranking is deliberately preference-only: an unmatched session still wins
// when nothing matches, because a task with no primary at all strands its queue
// and its session list, which is a worse outcome than promoting a session the
// workflow has merely moved past.
type primaryCandidateRank int

const (
	// primaryCandidateParked is a session a previous profile switch deliberately
	// parked. It stays answerable for its own conversation, but it is the last
	// thing an automatic promotion should reach for.
	primaryCandidateParked primaryCandidateRank = iota
	// primaryCandidateOther is a live session whose profile is not the current
	// step's effective profile.
	primaryCandidateOther
	// primaryCandidateCompatible is a live session already running under the
	// current step's effective profile.
	primaryCandidateCompatible
)

// sessionStatePreference keeps the historical RUNNING > active > idle ordering
// as the tiebreak inside one rank, so this selector changes which session wins
// across ranks and never how a set of equals is ordered.
func sessionStatePreference(state models.TaskSessionState) int {
	if state == models.TaskSessionStateRunning {
		return 2
	}
	if isActiveSessionState(state) {
		return 1
	}
	return 0
}

// workflowCompatibleSession reports whether a session's profile equals the
// current workflow step's effective profile. A task with no step, an
// unresolvable step, or a step without its own profile imposes no constraint,
// and neither does a dynamic profile whose concrete resolution is chosen at
// launch: an unmatched session stays eligible, it merely ranks lower.
func (s *Service) workflowCompatibleSession(
	ctx context.Context, task *models.Task, session *models.TaskSession, effectiveProfileID string,
) bool {
	if task == nil || task.WorkflowStepID == "" || effectiveProfileID == "" {
		return true
	}
	if session == nil {
		return false
	}
	return session.AgentProfileID == effectiveProfileID
}

// effectiveWorkflowProfileForTask resolves the profile the task's current step
// expects, or "" when the task is not workflow-owned or the step declares no
// profile of its own.
func (s *Service) effectiveWorkflowProfileForTask(ctx context.Context, task *models.Task) string {
	if task == nil || task.WorkflowStepID == "" || s.workflowStepGetter == nil {
		return ""
	}
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil || step == nil {
		return ""
	}
	return s.resolveStepAgentProfileForTask(ctx, task, step)
}

// bestPrimarySessionCandidate returns the session an automatic promotion should
// take, or "" when no live session remains. Terminal sessions are never
// candidates: a completed predecessor cannot answer for the task.
func (s *Service) bestPrimarySessionCandidate(
	ctx context.Context, taskID string, sessions []*models.TaskSession, excludeSessionID string,
) string {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		task = nil
	}
	effectiveProfileID := s.effectiveWorkflowProfileForTask(ctx, task)

	var (
		bestSession *models.TaskSession
		bestRank    primaryCandidateRank
		bestState   int
	)
	for _, session := range sessions {
		if session == nil || session.ID == excludeSessionID {
			continue
		}
		if isTerminalSessionState(session.State) {
			continue
		}
		rank := primaryCandidateOther
		if _, parked := models.LoadWorkflowParking(session.Metadata); parked {
			rank = primaryCandidateParked
		} else if s.workflowCompatibleSession(ctx, task, session, effectiveProfileID) {
			rank = primaryCandidateCompatible
		}
		state := sessionStatePreference(session.State)
		if bestSession == nil || rank > bestRank || (rank == bestRank && state > bestState) {
			bestSession, bestRank, bestState = session, rank, state
		}
	}
	if bestSession == nil {
		return ""
	}
	return bestSession.ID
}
