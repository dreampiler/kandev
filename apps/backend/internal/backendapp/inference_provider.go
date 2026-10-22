package backendapp

import (
	"context"
	"os"
	"strings"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	"github.com/kandev/kandev/internal/agentctl/server/utility"
	"github.com/kandev/kandev/internal/system/sessioncapacity"
	tasksqlite "github.com/kandev/kandev/internal/task/repository/sqlite"
)

const inferenceSettlementProvider = "provider_turn"

func (b *sharedInferenceConsumer) admitLegacyInferenceProvider(ctx context.Context, binding sessioncapacity.InferenceBinding, req *utility.PromptRequest) error {
	if b.provider == nil {
		return nil
	}
	s := b.provider
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	turn := inferenceTurnRequest(binding, req)
	bound, _ := s.profileBinding(ctx, turn.ProfileID, turn.Model, turn.Environment)
	return s.unresolvedProviderTurn(ctx, turn, bound)
}

func inferenceTurnRequest(binding sessioncapacity.InferenceBinding, req *utility.PromptRequest) agentruntime.ProviderTurnRequest {
	var environment map[string]string
	if req.InferenceConfig != nil {
		environment = req.InferenceConfig.Env
	}
	return agentruntime.ProviderTurnRequest{ExecutionID: binding.Key, SessionID: binding.SessionID, ProfileID: binding.ExecutionProfileID,
		AgentID: req.AgentID, ExecutorType: binding.ExecutorType, Model: req.Model, Generation: 1, Environment: environment}
}

func (b *sharedInferenceConsumer) admitInferenceProvider(ctx context.Context, client *agentruntime.InferenceClient, binding sessioncapacity.InferenceBinding, req *utility.PromptRequest) error {
	if b.provider == nil {
		return nil
	}
	s := b.provider
	s.admissionMu.Lock()
	defer s.admissionMu.Unlock()
	turn := inferenceTurnRequest(binding, req)
	bound, err := s.profileBinding(ctx, turn.ProfileID, turn.Model, turn.Environment)
	if err != nil || !s.inferenceAccountKnown(ctx, client, binding, req, bound) {
		if err := s.unresolvedProviderTurn(ctx, turn, bound); err != nil {
			return err
		}
		return b.recordInferenceProvider(ctx, binding, "", "", false)
	}
	turn.Population = b.providerPopulation
	unmetered := false
	turn.RecordUnmetered = func() { unmetered = true }
	_, err = s.admitBoundProviderTurn(ctx, turn, bound, func() error {
		return b.recordInferenceProvider(ctx, binding, bound.key, bound.entry.Provider, true)
	})
	if err != nil {
		return err
	}
	if unmetered {
		return b.clearUnmeteredInferenceProvider(ctx, binding, bound.key, bound.entry.Provider)
	}
	return nil
}

// Both identities must name the configured account. Profile defaults alone are
// not evidence of the environment inherited by the native inference child.
func (s *providerTurnAllowances) inferenceAccountKnown(ctx context.Context, client *agentruntime.InferenceClient, binding sessioncapacity.InferenceBinding, req *utility.PromptRequest, bound snapshotBinding) bool {
	cfg := req.InferenceConfig
	if client == nil || cfg == nil || !localAllowanceExecution(binding.ExecutorType) || req.AgentID != bound.agentType ||
		len(cfg.CommandPrefix) != 0 || cfg.ProviderGatewayAuth != nil || !allowanceBindingKnown(bound, s.home) || bound.entry.BindingScope != snapshotHostDefault {
		return false
	}
	policy := agentusage.ExecutionUsagePolicy{ProfileID: binding.ExecutionProfileID, AgentID: req.AgentID, ExecutorType: binding.ExecutorType}
	if req.UsagePolicy != nil {
		policy = *req.UsagePolicy
	}
	policy.ProfileID, policy.AgentID, policy.ExecutorType = binding.ExecutionProfileID, req.AgentID, binding.ExecutorType
	report, err := client.PreviewInferenceUsage(ctx, &utility.UsagePreviewRequest{ExecutionID: binding.AttemptID, AgentID: req.AgentID, Model: req.Model, Policy: policy, InferenceConfig: cfg})
	if err != nil || !knownInferenceUsage(report, req.Model, bound.entry.Provider) {
		return false
	}
	input := agentusage.ExecutionInputFromEnvironment(policy, binding.Key, req.Model, os.Environ())
	provider, account, organization, known := s.usage.PeekPreparedExecutionAccount(ctx, input)
	return known && provider == report.Current.Provider && account == report.Current.AccountRef && organization == report.Current.OrganizationRef
}

func knownInferenceUsage(report *agentusage.ExecutionUsageReport, model, provider string) bool {
	return report != nil && report.SourceKnown && report.Reason == "ok" && !report.BindingChanged && report.Model == model &&
		report.Current != nil && report.Current.Provider == provider && report.Current.AccountRef != ""
}

// Extra inference incumbents participate in ordinary as well as child turns.
// Existing unmetered period usage is never retroactively charged.
func (b *sharedInferenceConsumer) inferenceProviderPopulation(ctx context.Context, bound snapshotBinding, current string) ([]tasksqlite.ProviderTurnClaim, error) {
	row, err := b.provider.claims.GetProviderTurnAllowance(ctx, bound.key)
	if err != nil || row == nil {
		return nil, err
	}
	policy, err := agentusage.DecodeTurnAllowance(row.Owner)
	if err != nil {
		return nil, err
	}
	if policy.ConcurrentTurns == nil || *policy.ConcurrentTurns == 0 {
		return nil, nil
	}
	b.refresh(ctx)
	if !b.capacity.InferenceInventoryKnown() {
		return nil, agentusage.ErrTurnAllowanceUnknown
	}
	bindings, err := b.capacity.InferenceCapacityBindings(ctx)
	if err != nil {
		return nil, err
	}
	return inferenceProviderClaims(bindings, bound.key, row.Provider, current)
}

func inferenceProviderClaims(bindings map[string]sessioncapacity.InferenceBinding, key, provider, current string) ([]tasksqlite.ProviderTurnClaim, error) {
	var result []tasksqlite.ProviderTurnClaim
	for _, held := range bindings {
		if held.Key == current || held.State == "reserved" && held.SettlementKind == inferenceSettlementNone && held.ProviderKey == "" {
			continue
		}
		if held.ProviderKnown && held.ProviderKey != "" {
			if held.ProviderKey == key {
				result = append(result, providerTurnClaim(agentruntime.ProviderTurnRequest{ExecutionID: held.Key, SessionID: held.SessionID, Generation: 1}))
			}
			continue
		}
		if held.ProviderFamilies != "" && !containsProviderFamily(held.ProviderFamilies, provider) {
			continue
		}
		return nil, agentusage.ErrTurnAllowanceUnknown
	}
	return result, nil
}

func containsProviderFamily(families, provider string) bool {
	for _, family := range strings.Split(families, ",") {
		if family == provider {
			return true
		}
	}
	return false
}
