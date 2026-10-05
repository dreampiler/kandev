package orchestrator

import (
	"context"
	"strings"
	"time"

	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// workflowInstructionsDelivery records which common-instructions block a
// session has already been given. blockLength is the byte length of the block
// as it was composed; comparing it against the block a later prompt would carry
// is how a changed workflow prompt is told apart from an unchanged one.
type workflowInstructionsDelivery struct {
	blockLength int
	deliveredAt time.Time
}

func (s *Service) workflowInstructionsDeliveredFor(sessionID string) (workflowInstructionsDelivery, bool) {
	if sessionID == "" {
		return workflowInstructionsDelivery{}, false
	}
	value, ok := s.workflowInstructionsDelivered.Load(sessionID)
	if !ok {
		return workflowInstructionsDelivery{}, false
	}
	delivery, ok := value.(workflowInstructionsDelivery)
	return delivery, ok
}

// recordWorkflowInstructionsDelivered marks block as delivered to sessionID.
// Callers record only once the prompt carrying it has been accepted for
// dispatch, so a refused or failed turn leaves the session eligible again.
func (s *Service) recordWorkflowInstructionsDelivered(sessionID, block string) {
	if sessionID == "" || block == "" {
		return
	}
	s.workflowInstructionsDelivered.Store(sessionID, workflowInstructionsDelivery{
		blockLength: len(block),
		deliveredAt: time.Now(),
	})
}

// pendingWorkflowInstructionsForSession reports whether block should still be
// delivered to sessionID, and records it as delivered when it should. Callers
// on the step-entry and launch paths compose their prompt and dispatch it
// immediately, so the record is written at composition; the dispatch failure
// paths that leave the session alive clear it again.
func (s *Service) pendingWorkflowInstructionsForSession(sessionID, block string) bool {
	if sessionID == "" {
		return true
	}
	if delivered, ok := s.workflowInstructionsDeliveredFor(sessionID); ok &&
		delivered.blockLength == len(block) {
		return false
	}
	s.recordWorkflowInstructionsDelivered(sessionID, block)
	return true
}

// clearWorkflowInstructionsDelivered forgets a session's delivery record. A
// context reset restarts the conversation in place under the same session ID,
// which counts as a new session for prompt delivery; a session that is deleted
// no longer needs its entry at all.
func (s *Service) clearWorkflowInstructionsDelivered(sessionID string) {
	if sessionID == "" {
		return
	}
	s.workflowInstructionsDelivered.Delete(sessionID)
}

// workflowInstructionsForPrompt gives a session its first copy of the workflow
// common instructions, whichever path the prompt arrived on. It returns the
// prompt to dispatch, the delivery to record once the turn is accepted, and
// whether a block was appended. An internal continuation drops the block like
// it drops the other prompt transforms, so nothing is recorded for it.
func (s *Service) workflowInstructionsForPrompt(
	ctx context.Context, taskID, sessionID, prompt string, internalContinuation bool,
) (string, workflowInstructionsDelivery, bool) {
	if internalContinuation {
		return prompt, workflowInstructionsDelivery{}, false
	}
	return s.appendWorkflowInstructionsIfPending(ctx, taskID, sessionID, prompt)
}

// appendWorkflowInstructionsIfPending prepends the task's workflow-level common
// instructions to prompt when this session has not already been given them. It
// returns the (possibly unchanged) prompt, the delivery to record once the
// prompt is accepted, and whether a block was appended.
//
// A prompt that already carries the block is returned untouched: a step-entry
// launch composes the block before handing the prompt to PromptTask, and both
// paths must not add it twice.
func (s *Service) appendWorkflowInstructionsIfPending(
	ctx context.Context, taskID, sessionID, prompt string,
) (string, workflowInstructionsDelivery, bool) {
	if strings.Contains(prompt, workflowInstructionsEnd) {
		return prompt, workflowInstructionsDelivery{}, false
	}
	block := s.workflowInstructionsBlockForTask(ctx, taskID)
	if block == "" {
		return prompt, workflowInstructionsDelivery{}, false
	}
	if delivered, ok := s.workflowInstructionsDeliveredFor(sessionID); ok &&
		delivered.blockLength == len(block) {
		return prompt, workflowInstructionsDelivery{}, false
	}
	return block + "\n\n" + prompt, workflowInstructionsDelivery{
		blockLength: len(block),
		deliveredAt: time.Now(),
	}, true
}

// workflowInstructionsBlockForTask returns the task workflow's
// common-instructions block, or "" when the task is not on a workflow step or
// the workflow has no prompt. It is the message-path counterpart of the
// step-entry composition in buildWorkflowPromptWithTrustedContextOptions, which
// already holds the step.
//
// The step is reconstructed from the task row rather than loaded: only its ID
// and workflow ID are needed to render the block, and the message path must not
// spend a workflow-step lookup that stale-dispatch handling has explicitly
// forbidden.
func (s *Service) workflowInstructionsBlockForTask(ctx context.Context, taskID string) string {
	if s.workflowStepGetter == nil || s.repo == nil || taskID == "" {
		return ""
	}
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil || task == nil || task.WorkflowStepID == "" || task.WorkflowID == "" {
		return ""
	}
	step := &wfmodels.WorkflowStep{ID: task.WorkflowStepID, WorkflowID: task.WorkflowID}
	return s.workflowInstructionsBlock(ctx, step, taskID)
}
