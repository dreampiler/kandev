package sqlite

import (
	"context"
	"testing"

	agentusage "github.com/kandev/kandev/internal/agent/usage"
)

func TestProviderTierLearningDoesNotReplaceOwnerSettings(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	ownerValue, learnedValue := 90.0, 80.0
	owner := agentusage.AccountTierConfig{Tiers: map[agentusage.BillingTier]agentusage.TierLimits{
		agentusage.TierFree: {RequestsPerDay: &ownerValue},
	}}
	learned := agentusage.AccountTierConfig{Tiers: map[agentusage.BillingTier]agentusage.TierLimits{
		agentusage.TierFree: {RequestsPerDay: &learnedValue, Learned: true},
	}}
	if err := repo.SaveAccountTierOverrides(ctx, "test-account", owner); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveAccountTierLearned(ctx, "test-account", learned); err != nil {
		t.Fatal(err)
	}
	stored, evidence, err := repo.LoadAccountTierConfig(ctx, "test-account")
	if err != nil {
		t.Fatal(err)
	}
	if *stored.Tiers[agentusage.TierFree].RequestsPerDay != ownerValue || *evidence.Tiers[agentusage.TierFree].RequestsPerDay != learnedValue {
		t.Fatalf("owner/learned columns were mixed: %+v, %+v", stored, evidence)
	}
	if err := repo.SaveAccountTierOverrides(ctx, "test-account", agentusage.AccountTierConfig{Tiers: map[agentusage.BillingTier]agentusage.TierLimits{}}); err != nil {
		t.Fatal(err)
	}
	stored, evidence, err = repo.LoadAccountTierConfig(ctx, "test-account")
	if err != nil || len(stored.Tiers) != 0 || *evidence.Tiers[agentusage.TierFree].RequestsPerDay != learnedValue {
		t.Fatalf("clearing overrides erased learning: %+v, %+v, %v", stored, evidence, err)
	}
}
