package utility

import (
	"context"
	"github.com/kandev/kandev/internal/agent/usage"
)

// Exact same sanitized env slice is subsequently assigned to cmd.Env. Unknown
// providers do not borrow a host subscription; probes never call this path.
func (e *ACPInferenceExecutor) checkExecutionUsage(ctx context.Context, req *PromptRequest, env []string) *usage.ExecutionUsageReport {
	if req.UsagePolicy == nil {
		return nil
	}
	if req.InferenceConfig.ProviderGatewayAuth != nil {
		return unavailableSelectedUsage(req.UsagePolicy, "provider_selection_unknown", req.Model)
	}
	policy := *req.UsagePolicy
	input := nativeInferenceUsageInput(req, env)
	if !usage.NativeProviderSelectionKnown(input) {
		return unavailableSelectedUsage(req.UsagePolicy, "provider_selection_unknown", req.Model)
	}
	input, reason, _ := e.executionUsage.BindExecutionUsage(ctx, input)
	if selectedUsageChanged(policy, input, reason, req.Model) {
		return &usage.ExecutionUsageReport{Reason: "binding_changed", BindingChanged: true, Model: req.Model}
	}
	if reason != "ok" {
		return &usage.ExecutionUsageReport{Reason: reason, Model: req.Model}
	}
	return usage.ReadExecutionUsageReport(ctx, e.executionUsage, input)
}

func selectedUsageChanged(policy usage.ExecutionUsagePolicy, input usage.ExecutionUsageInput, reason, model string) bool {
	if policy.ExpectedAccountRef == "" {
		return false
	}
	if reason != "ok" || model != policy.ExpectedModel || input.ExpectedAccountRef != policy.ExpectedAccountRef || input.ExpectedOrganizationRef != policy.ExpectedOrganizationRef {
		return true
	}
	provider := "anthropic"
	if input.AgentID == "codex" || input.AgentID == "codex-acp" {
		provider = "openai"
	}
	return provider != policy.ExpectedProvider
}

func unavailableSelectedUsage(policy *usage.ExecutionUsagePolicy, reason, model string) *usage.ExecutionUsageReport {
	changed := policy != nil && policy.ExpectedAccountRef != ""
	if changed {
		reason = "binding_changed"
	}
	return &usage.ExecutionUsageReport{Reason: reason, Model: model, BindingChanged: changed}
}
