package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

func TestOfflineRetentionContinuesPastProtectedDiscovery(t *testing.T) {
	calls := 0
	repo := &fakeDeliveryRetentionRepo{
		retentionFn: func(context.Context, models.AgentDeliveryRetentionBatchOptions) (models.AgentDeliveryRetentionBatchResult, error) {
			calls++
			switch calls {
			case 1:
				return models.AgentDeliveryRetentionBatchResult{Candidates: 200, DiscoveryAdvanced: true}, nil
			case 2:
				return models.AgentDeliveryRetentionBatchResult{Candidates: 100, EventRowsRetired: 100, DiscoveryAdvanced: true}, nil
			default:
				return models.AgentDeliveryRetentionBatchResult{}, nil
			}
		},
		orphanFn: func(context.Context, models.AgentDeliveryOrphanBatchOptions) (models.AgentDeliveryOrphanBatchResult, error) {
			return models.AgentDeliveryOrphanBatchResult{}, nil
		},
		retirementFn: func(context.Context, models.AgentDeliveryRetirementBatchOptions) (models.AgentDeliveryRetirementBatchResult, error) {
			return models.AgentDeliveryRetirementBatchResult{}, nil
		},
	}
	result, err := executeDeliveryRetention(context.Background(), repo, models.AgentDeliveryRetentionBatchOptions{})
	if err != nil || result.RetiredDeliveryEventReceipts != 100 {
		t.Fatalf("offline result=%+v error=%v; want 100 committed inbox/effects retirements", result, err)
	}
}

func TestOfflineRetentionDrainsRealInboxAndEffects(t *testing.T) {
	fixture := newTestFixture(t)
	seedMaintenanceRetentionEvents(fixture, 3)
	outcome, err := Run(context.Background(), t.TempDir(), "sqlite", fixture.dbPath,
		RunOptions{Execute: true, DeliveryRetention: models.AgentDeliveryRetentionBatchOptions{MaxCandidates: 1}}, nil)
	if (err != nil && !errors.Is(err, ErrPartial)) || outcome.Execution.RetiredDeliveryEventReceipts != 3 {
		t.Fatalf("offline result=%+v error=%v; want 3 retired rows", outcome.Execution, err)
	}
	for _, table := range []string{"agent_delivery_inbox", "agent_delivery_effects"} {
		var count int
		if err := fixture.repo.DB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE stream_id = ?", "maintenance-retention-stream").Scan(&count); err != nil || count != 0 {
			t.Fatalf("remaining %s rows=%d error=%v", table, count, err)
		}
	}
}
