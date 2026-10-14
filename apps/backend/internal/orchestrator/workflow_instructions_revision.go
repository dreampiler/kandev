package orchestrator

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/sysprompt"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// workflowInstructionsBlockWithRevision is workflowInstructionsBlock plus the
// workflow's last-modified time, which a per-session delivery record carries so
// an unchanged workflow can be recognised without comparing the block.
func (s *Service) workflowInstructionsBlockWithRevision(
	ctx context.Context, step *wfmodels.WorkflowStep, taskID string,
) (string, string, time.Time) {
	if s.workflowStepGetter == nil || step == nil || step.WorkflowID == "" {
		return "", "", time.Time{}
	}
	meta, err := s.getWorkflowMeta(ctx, step.WorkflowID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to get workflow prompt for prompt building",
				zap.String("workflow_id", step.WorkflowID),
				zap.Error(err))
		}
		return "", "", time.Time{}
	}
	prompt := strings.TrimSpace(meta.Prompt)
	if prompt == "" {
		return "", "", meta.PromptUpdatedAt
	}
	interpolated := sysprompt.InterpolatePlaceholders(prompt, taskID)
	interpolated = s.interpolateStepEntryNumberIfPresent(ctx, interpolated, taskID, step.ID)
	// The title is substituted last so its text is never scanned for tokens.
	interpolated, title := s.interpolateTaskTitleIfPresent(ctx, interpolated, taskID)
	interpolated = strings.TrimSpace(interpolated)
	if interpolated == "" {
		return "", "", meta.PromptUpdatedAt
	}
	// Drop any accidental end-marker text from user content so chat split
	// cannot cut the block early (frontend also prefers the final marker).
	interpolated = strings.ReplaceAll(interpolated, workflowInstructionsEnd, "")
	interpolated = strings.TrimSpace(interpolated)
	if interpolated == "" {
		return "", "", meta.PromptUpdatedAt
	}
	return workflowInstructionsHeading + "\n\n" + interpolated + "\n\n" + workflowInstructionsEnd, title, meta.PromptUpdatedAt
}
