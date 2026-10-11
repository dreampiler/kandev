package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/kandev/kandev/internal/task/deliveryretention"
	"github.com/kandev/kandev/internal/task/models"
)

func TestDeliveryRetentionStorageAccounting(t *testing.T) {
	for _, engine := range []string{"SQLite", "Postgres"} {
		for _, outcome := range []string{"acknowledged", "rollback", "lost_commit_ack", "consecutive_lost_commit_acks"} {
			t.Run(engine+"/"+outcome, func(t *testing.T) {
				repo, events := seedRetentionBoundsStream(t, engine, "accounting", 1, 0)
				const payloadSize, rows = 1024, deliveryretention.BackgroundBatchRows
				seedRetentionStorageRows(t, repo, events[0], rows*4, payloadSize)
				before := retentionCanonicalContent(t, repo, events[0].SessionID)
				if outcome == "rollback" {
					repo.agentDeliveryRetentionFailpoint = func(point string) error {
						if point == "after_progress" {
							repo.agentDeliveryRetentionFailpoint = nil
							return context.DeadlineExceeded
						}
						return nil
					}
				}
				measured := &retentionStorageCommitAckRepository{Repository: repo, loseAck: outcome == "lost_commit_ack", loseEveryAck: outcome == "consecutive_lost_commit_acks"}
				result, retries, removedBytes := runRetentionStorageAccountedBatch(t, measured, events[0], payloadSize, rows)
				wantRows, wantRetries := rows, 0
				if outcome != "acknowledged" {
					wantRetries = 1
				}
				wantAcknowledgedRows := rows
				if outcome == "lost_commit_ack" || outcome == "consecutive_lost_commit_acks" {
					wantAcknowledgedRows = 0
				}
				wantBytes := int64((wantRows-1)*payloadSize + len(events[0].Payload))
				if removedBytes != wantBytes || retries != wantRetries || result.EventRowsRetired != wantAcknowledgedRows {
					t.Fatalf("durable removal bytes=%d want=%d retries=%d want=%d result=%+v", removedBytes, wantBytes, retries, wantRetries, result)
				}
				assertRetentionRowCount(t, repo, "agent_delivery_inbox", "stream_id = ?", events[0].StreamID, rows*4-wantRows)
				if retentionCanonicalContent(t, repo, events[0].SessionID) != before {
					t.Fatal("commit acknowledgement recovery changed canonical output")
				}
			})
		}
	}
}

type retentionStorageCommitAckRepository struct {
	*Repository
	loseAck      bool
	loseEveryAck bool
}

func (r *retentionStorageCommitAckRepository) RunAgentDeliveryRetentionBatch(ctx context.Context, options models.AgentDeliveryRetentionBatchOptions) (models.AgentDeliveryRetentionBatchResult, error) {
	result, err := r.Repository.RunAgentDeliveryRetentionBatch(ctx, options)
	if err == nil && (r.loseAck || r.loseEveryAck) {
		r.loseAck = false
		return models.AgentDeliveryRetentionBatchResult{}, fmt.Errorf("commit acknowledgement: %w", context.DeadlineExceeded)
	}
	return result, err
}
