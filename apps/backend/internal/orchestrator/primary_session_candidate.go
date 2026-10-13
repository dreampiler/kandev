package orchestrator

import (
	"context"

	"github.com/kandev/kandev/internal/task/models"
)

// primaryCandidateRank orders sessions after removal of the primary.
// Removal uses this preference ordering when the former primary no longer
// exists. Terminal-state promotion instead requires a current-profile successor.
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
// current workflow step's effective profile. A task with no step or a
// step without its own profile imposes no constraint. Dynamic profiles are
// compared by logical identity; execution candidates do not define ownership.
func workflowCompatibleSession(
	task *models.Task, session *models.TaskSession, effectiveProfileID string,
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
func (s *Service) effectiveWorkflowProfileForTask(ctx context.Context, task *models.Task) (string, bool) {
	if task == nil || task.WorkflowStepID == "" {
		return "", true
	}
	if s.workflowStepGetter == nil {
		return "", false
	}
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil || step == nil {
		return "", false
	}
	return s.resolveStepAgentProfileForTask(ctx, task, step), true
}

// bestPrimarySessionCandidate returns the session an automatic promotion should
// take, or "" when no eligible session remains.
//
// Terminal-state promotion requires a newest same-profile active successor.
// Removal can fall back to any nonterminal session once the former primary
// no longer exists.
//
// Terminal sessions are never candidates under either rule: a completed
// predecessor cannot answer for the task.
func (s *Service) bestPrimarySessionCandidate(
	ctx context.Context, taskID string, sessions []*models.TaskSession, excludeSessionID string, activeOnly bool,
) string {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		task = nil
	}
	effectiveProfileID, profileKnown := s.effectiveWorkflowProfileForTask(ctx, task)
	if activeOnly {
		if !profileKnown {
			return ""
		}
		return currentProfilePrimarySuccessor(task, sessions, excludeSessionID, effectiveProfileID)
	}

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
		} else if workflowCompatibleSession(task, session, effectiveProfileID) {
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

// currentProfilePrimarySuccessor keeps the logical profile and never moves
// primary ownership backwards to an older conversation. The newest same-profile
// session is authoritative even when terminal or parked; an older live runtime
// must not reclaim work from its replacement.
func currentProfilePrimarySuccessor(
	task *models.Task, sessions []*models.TaskSession, stoppedID, effectiveProfileID string,
) string {
	if task == nil {
		return ""
	}
	var stopped *models.TaskSession
	for _, session := range sessions {
		if session != nil && session.ID == stoppedID {
			stopped = session
			break
		}
	}
	if stopped == nil || stopped.AgentProfileID == "" ||
		!workflowCompatibleSession(task, stopped, effectiveProfileID) {
		return ""
	}
	newest := newestSameProfileSession(sessions, stopped)
	if newest.ID == stoppedID || !isActiveSessionState(newest.State) {
		return ""
	}
	if _, parked := models.LoadWorkflowParking(newest.Metadata); parked {
		return ""
	}
	return newest.ID
}

func newestSameProfileSession(sessions []*models.TaskSession, stopped *models.TaskSession) *models.TaskSession {
	newest := stopped
	for _, session := range sessions {
		if session == nil || session.AgentProfileID != stopped.AgentProfileID {
			continue
		}
		if session.StartedAt.After(newest.StartedAt) ||
			(session.StartedAt.Equal(newest.StartedAt) && sessionStatePreference(session.State) > sessionStatePreference(newest.State)) {
			newest = session
		}
	}
	return newest
}
