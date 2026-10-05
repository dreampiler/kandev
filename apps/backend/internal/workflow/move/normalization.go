package move

import (
	"strings"

	"github.com/kandev/kandev/internal/workflow/models"
)

// NormalizeEntryOptions trims all string fields, folds the legacy top-level
// prompt into instructions, and returns nil for an empty options object.
//
// The legacy prompt is accepted only when the nested instructions field is
// absent. Supplying both non-blank values is rejected, even when they match,
// so callers use one unambiguous request shape.
func NormalizeEntryOptions(options *EntryOptions, legacyPrompt string) (*EntryOptions, error) {
	legacyPrompt = strings.TrimSpace(legacyPrompt)
	normalized := normalizedCopy(options)
	if normalized == nil {
		normalized = &EntryOptions{}
	}

	if normalized.Instructions != "" && legacyPrompt != "" {
		return nil, &ConflictingInstructionsError{}
	}
	if normalized.Instructions == "" {
		normalized.Instructions = legacyPrompt
	}
	if *normalized == (EntryOptions{}) {
		return nil, nil
	}
	return normalized, nil
}

// ValidateEntryOptions rejects agent-facing options for a move that does not
// actually change workflow step. Empty options are always a no-op and remain
// valid for position-only or no-step moves.
func ValidateEntryOptions(options *EntryOptions, change MoveChange) error {
	if normalizedCopy(options) == nil {
		return nil
	}
	if change != MoveChangeStep {
		return &EntryOptionsNotAllowedError{Change: change}
	}
	return nil
}

// ValidateEntryTarget rejects agent-facing entry options when the target step
// does not start an agent to receive them. A step without an auto_start_agent
// on_enter action (for example Waiting, Blocked, Hold, or Done) cannot deliver a
// one-shot hand-off, so accepting the options would silently drop them. A nil
// step is treated as unknown and does not reject, so the move path reports its
// own canonical not-found error instead.
func ValidateEntryTarget(step *models.WorkflowStep) error {
	if step == nil || step.HasOnEnterAction(models.OnEnterAutoStartAgent) {
		return nil
	}
	return ErrEntryTargetIsAgentless
}
