package sqlite

import (
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// TestAgentDeliveryMaintenanceBudgetSplit proves the offline maintenance
// flag selects a longer finite per-step budget while the online default
// (250ms) stays unchanged.
func TestAgentDeliveryMaintenanceBudgetSplit(t *testing.T) {
	t.Run("online_default_is_unchanged", func(t *testing.T) {
		orphan := normalizeAgentDeliveryOrphanOptions(models.AgentDeliveryOrphanBatchOptions{WorkTimeout: time.Hour})
		if orphan.WorkTimeout != agentDeliveryRetirementTimeout {
			t.Fatalf("online orphan timeout = %v, want %v", orphan.WorkTimeout, agentDeliveryRetirementTimeout)
		}
		retirement := normalizeAgentDeliveryRetirementOptions(models.AgentDeliveryRetirementBatchOptions{WorkTimeout: time.Hour})
		if retirement.WorkTimeout != agentDeliveryRetirementTimeout {
			t.Fatalf("online retirement timeout = %v, want %v", retirement.WorkTimeout, agentDeliveryRetirementTimeout)
		}
		retention := normalizeAgentDeliveryRetentionOptions(models.AgentDeliveryRetentionBatchOptions{WorkTimeout: time.Hour})
		if retention.WorkTimeout != agentDeliveryRetentionWorkTimeout {
			t.Fatalf("online retention timeout = %v, want %v", retention.WorkTimeout, agentDeliveryRetentionWorkTimeout)
		}
	})

	t.Run("offline_default_uses_maintenance_budget", func(t *testing.T) {
		orphan := normalizeAgentDeliveryOrphanOptions(models.AgentDeliveryOrphanBatchOptions{OfflineMaintenance: true})
		if orphan.WorkTimeout != agentDeliveryMaintenanceWorkTimeout {
			t.Fatalf("offline orphan default timeout = %v, want %v", orphan.WorkTimeout, agentDeliveryMaintenanceWorkTimeout)
		}
		retirement := normalizeAgentDeliveryRetirementOptions(models.AgentDeliveryRetirementBatchOptions{OfflineMaintenance: true})
		if retirement.WorkTimeout != agentDeliveryMaintenanceWorkTimeout {
			t.Fatalf("offline retirement default timeout = %v, want %v", retirement.WorkTimeout, agentDeliveryMaintenanceWorkTimeout)
		}
	})

	t.Run("offline_explicit_in_range_budget_is_preserved", func(t *testing.T) {
		orphan := normalizeAgentDeliveryOrphanOptions(models.AgentDeliveryOrphanBatchOptions{
			OfflineMaintenance: true, WorkTimeout: time.Minute,
		})
		if orphan.WorkTimeout != time.Minute {
			t.Fatalf("offline explicit timeout = %v, want %v", orphan.WorkTimeout, time.Minute)
		}
	})

	t.Run("offline_oversized_budget_is_clamped", func(t *testing.T) {
		orphan := normalizeAgentDeliveryOrphanOptions(models.AgentDeliveryOrphanBatchOptions{
			OfflineMaintenance: true, WorkTimeout: time.Hour,
		})
		if orphan.WorkTimeout != agentDeliveryMaintenanceWorkTimeout {
			t.Fatalf("offline oversized timeout = %v, want %v", orphan.WorkTimeout, agentDeliveryMaintenanceWorkTimeout)
		}
	})
}
