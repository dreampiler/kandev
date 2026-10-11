package sqlite

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

func TestDeliveryRetentionPreview(t *testing.T) {
	for _, engine := range []string{"SQLite", "Postgres"} {
		t.Run(engine, func(t *testing.T) {
			repo, _ := seedRetentionBoundsStream(t, engine, "preview", 3, 3)
			before := measureRetentionStorage(t, repo)
			preview, err := repo.AnalyzeAgentDeliveryRetention(context.Background(), models.AgentDeliveryRetentionBatchOptions{})
			if err != nil || !preview.Available || preview.EventPayloadCandidates != 2 || preview.EventReceiptCandidates != 2 {
				t.Fatalf("bounded preview: %+v error=%v", preview, err)
			}
			after := measureRetentionStorage(t, repo)
			if after.InboxRows != before.InboxRows || after.RawPayloadBytes != before.RawPayloadBytes ||
				after.GenericEffects != before.GenericEffects || after.CompactRows != before.CompactRows ||
				after.CanonicalBytes != before.CanonicalBytes {
				t.Fatalf("preview changed recovery evidence: before=%+v after=%+v", before, after)
			}
			result, err := repo.RunAgentDeliveryRetentionBatch(context.Background(), models.AgentDeliveryRetentionBatchOptions{})
			if err != nil || result.EventPayloadsPruned != preview.EventPayloadCandidates || result.EventRowsRetired != preview.EventReceiptCandidates || result.PayloadBytesPruned != preview.EventPayloadBytes {
				t.Fatalf("live batch differs from preview: preview=%+v result=%+v error=%v", preview, result, err)
			}
		})
	}
}
