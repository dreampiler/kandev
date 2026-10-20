package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
)

// TestCompleteActiveClarificationBundlePersistsAnsweredBy proves the answer
// channel travels through the responses map's bundle-wide key and lands on
// every durable message row, for both terminal statuses.
func TestCompleteActiveClarificationBundlePersistsAnsweredBy(t *testing.T) {
	for _, status := range []string{clarificationStatusAnswered, clarificationStatusRejected} {
		t.Run(status, func(t *testing.T) {
			repo := newRepoForSessionTests(t)
			ctx := context.Background()
			base := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
			pendingID := "pending-answered-by-" + status
			taskID := "task-answered-by-" + status
			sessionID := "session-answered-by-" + status
			seedPendingActionSession(t, repo, taskID, sessionID)
			createPendingActionTurn(t, repo, taskID, sessionID, "turn-answered-by-"+status, base, base)
			createClarificationBundleMessage(
				t, repo, "message-answered-by-"+status, taskID, sessionID,
				"turn-answered-by-"+status, pendingID, "q1", base,
			)

			responses := map[string]interface{}{
				"q1": map[string]interface{}{"question_id": "q1", "custom_text": "thread reply"},
				models.ClarificationAnsweredByResponseKey: models.ClarificationAnsweredBy{
					Channel: "slack_thread",
					Ref:     "C012AB345CD/1729000000.000100",
				},
			}
			claimed, ok, err := repo.CompleteActiveClarificationBundle(ctx, pendingID, status, responses)
			if err != nil || !ok {
				t.Fatalf("CompleteActiveClarificationBundle = claimed %v, %v; want true, nil", ok, err)
			}
			if len(claimed) != 1 {
				t.Fatalf("claimed rows = %d, want 1", len(claimed))
			}

			stored, err := repo.GetMessage(ctx, "message-answered-by-"+status)
			if err != nil {
				t.Fatalf("GetMessage: %v", err)
			}
			raw, present := stored.Metadata[clarificationAnsweredByMetadataKey]
			if !present {
				t.Fatalf("metadata = %#v, want answered_by marker", stored.Metadata)
			}
			by, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("answered_by = %#v, want map[string]any", raw)
			}
			if by["channel"] != "slack_thread" || by["ref"] != "C012AB345CD/1729000000.000100" {
				t.Fatalf("answered_by = %#v, want slack_thread channel and ref", by)
			}

			restored, restoredOK, err := repo.RestoreActiveClarificationBundle(ctx, pendingID, status, claimed)
			if err != nil || !restoredOK {
				t.Fatalf("RestoreActiveClarificationBundle = %v, %v; want true, nil", restoredOK, err)
			}
			if len(restored) != 1 {
				t.Fatalf("restored rows = %d, want 1", len(restored))
			}
			if _, present := restored[0].Metadata[clarificationAnsweredByMetadataKey]; present {
				t.Fatalf("restored metadata = %#v, want no answered_by marker", restored[0].Metadata)
			}
		})
	}
}
