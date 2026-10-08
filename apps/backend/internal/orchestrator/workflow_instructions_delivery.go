package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/sysprompt"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// workflowInstructionsDelivery records which common-instructions block a
// session already holds, so a session receives the block once instead of on
// every step it moves through. Nothing about the block's content is stored,
// compared, digested, or signed.
type workflowInstructionsDelivery struct {
	blockLength       int
	workflowUpdatedAt time.Time
	deliveredAt       time.Time
}

// alreadyHolds reports whether the session still has the block a later prompt
// would carry. Both recorded signals must match: the length, so a rewritten
// prompt of a different size is seen as changed, and the workflow's
// last-modified time, so a rewrite that happens to keep the same size is seen
// as changed too. An equal timestamp never excuses a different length.
func (d workflowInstructionsDelivery) alreadyHolds(block string, workflowUpdatedAt time.Time) bool {
	return d.blockLength == len(block) && d.workflowUpdatedAt.Equal(workflowUpdatedAt)
}

func newWorkflowInstructionsDelivery(block string, workflowUpdatedAt time.Time) workflowInstructionsDelivery {
	return workflowInstructionsDelivery{
		blockLength:       len(block),
		workflowUpdatedAt: workflowUpdatedAt,
		deliveredAt:       time.Now(),
	}
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
) (string, workflowInstructionsDelivery, bool, string) {
	if internalContinuation {
		return prompt, workflowInstructionsDelivery{}, false, ""
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
) (string, workflowInstructionsDelivery, bool, string) {
	if strings.Contains(prompt, workflowInstructionsEnd) {
		return prompt, workflowInstructionsDelivery{}, false, ""
	}
	block, title, workflowUpdatedAt := s.workflowInstructionsForTask(ctx, taskID)
	if block == "" {
		return prompt, workflowInstructionsDelivery{}, false, ""
	}
	if delivered, ok := s.workflowInstructionsDeliveredFor(sessionID); ok &&
		delivered.alreadyHolds(block, workflowUpdatedAt) {
		return prompt, workflowInstructionsDelivery{}, false, ""
	}
	delivery := newWorkflowInstructionsDelivery(block, workflowUpdatedAt)
	return block + "\n\n" + prompt, delivery, true, title
}

func (s *Service) prepareWorkflowTitleReferences(
	ctx context.Context, prompt, title string, isPassthrough bool, options *promptTaskOptions,
) string {
	previous := options.promptReferenceContext
	updated := s.appendTitlePromptReferencesToTrustedContext(ctx, title, previous, isPassthrough)
	if updated == previous {
		return prompt
	}
	if previous == "" && !options.promptReferencesPrepared {
		prompt, updated = s.expandPromptReferencesWithContext(ctx, prompt, isPassthrough)
		options.promptReferencesPrepared = true
	}
	options.promptReferenceContext = updated
	if previous != "" {
		oldBlock, newBlock := sysprompt.Wrap(previous), sysprompt.Wrap(updated)
		prompt = strings.Replace(prompt, oldBlock, newBlock, 1)
		options.fallbackLaunchPrompt = strings.Replace(options.fallbackLaunchPrompt, oldBlock, newBlock, 1)
		options.fallbackRetryPrompt = strings.Replace(options.fallbackRetryPrompt, oldBlock, newBlock, 1)
	}
	prompt, _ = s.applyDirectPromptReferenceContext(ctx, prompt, isPassthrough, updated, true)
	return prompt
}

// pendingWorkflowInstructionsForSession reports whether the session still needs
// this block, and records it as delivered when it does. Callers on the step-entry
// and launch paths compose their prompt and dispatch it immediately, so the
// record is written at composition; the dispatch failure paths that leave the
// session alive clear it again.
func (s *Service) pendingWorkflowInstructionsForSession(sessionID, block string, workflowUpdatedAt time.Time) bool {
	if sessionID == "" {
		return true
	}
	if delivered, ok := s.workflowInstructionsDeliveredFor(sessionID); ok &&
		delivered.alreadyHolds(block, workflowUpdatedAt) {
		return false
	}
	s.workflowInstructionsDelivered.Store(sessionID, newWorkflowInstructionsDelivery(block, workflowUpdatedAt))
	return true
}

// workflowInstructionsForTask returns the task workflow's common-instructions
// block with the workflow's last-modified time, or an empty block when the task
// is not on a workflow step or the workflow has no prompt. It is the
// message-path counterpart of the step-entry composition in
// buildWorkflowPromptWithTrustedContextOptions, which already holds the step.
//
// The step is reconstructed from the task row rather than loaded: only its ID
// and workflow ID are needed to render the block, and the message path must not
// spend a workflow-step lookup that stale-dispatch handling has explicitly
// forbidden.
func (s *Service) workflowInstructionsForTask(ctx context.Context, taskID string) (string, string, time.Time) {
	if s.workflowStepGetter == nil || s.repo == nil || taskID == "" {
		return "", "", time.Time{}
	}
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil || task == nil || task.WorkflowStepID == "" || task.WorkflowID == "" {
		return "", "", time.Time{}
	}
	step := &wfmodels.WorkflowStep{ID: task.WorkflowStepID, WorkflowID: task.WorkflowID}
	return s.workflowInstructionsBlockWithRevision(ctx, step, taskID)
}
