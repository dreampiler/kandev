package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
)

func retentionTestEvent(streamID string, sequence int64, at time.Time, terminal bool, submissionID string) *models.AgentDeliveryEvent {
	eventType := "assistant_message"
	if terminal {
		eventType = "complete"
	}
	return &models.AgentDeliveryEvent{
		SessionID:         "retention-session",
		IncarnationID:     "retention-incarnation",
		HarnessGeneration: 1,
		StreamID:          streamID,
		Sequence:          sequence,
		SubmissionID:      submissionID,
		EventType:         eventType,
		Payload:           []byte(`{"type":"assistant_message"}`),
		Terminal:          terminal,
		ReceivedAt:        at,
	}
}

func receiveAndProjectRetentionEvent(t *testing.T, repo *Repository, event *models.AgentDeliveryEvent) {
	t.Helper()
	inserted, err := repo.ReceiveAgentDeliveryEvent(context.Background(), event, 0)
	if err != nil || !inserted {
		t.Fatalf("receive seq %d: inserted=%v err=%v", event.Sequence, inserted, err)
	}
	if _, err := repo.ProjectAgentDeliveryEvent(context.Background(), event, genericRetentionEffect(event)); err != nil {
		t.Fatalf("project seq %d: %v", event.Sequence, err)
	}
}

func genericRetentionEffect(event *models.AgentDeliveryEvent) *models.AgentDeliveryEffect {
	now := time.Now().UTC()
	return &models.AgentDeliveryEffect{
		EffectKey:   genericAgentDeliveryEffectKey(event.StreamID, event.Sequence),
		StreamID:    event.StreamID,
		Sequence:    event.Sequence,
		EffectType:  "agent_delivery.event",
		State:       models.DeliveryEffectCompleted,
		CreatedAt:   now,
		CompletedAt: &now,
	}
}

func TestRetireAgentDeliveryPrefixRemovesSettledProjectedPrefix(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	streamID := "retention-stream"
	base := time.Now().UTC().Add(-3 * time.Hour)
	for seq := int64(1); seq <= 5; seq++ {
		receiveAndProjectRetentionEvent(t, repo, retentionTestEvent(streamID, seq, base.Add(time.Duration(seq)*time.Minute), false, ""))
	}

	result, err := repo.RetireAgentDeliveryPrefix(ctx, 0, time.Now().UTC(), models.AgentDeliveryRetirementBudget{Streams: 10, Rows: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsRemoved != 5 {
		t.Fatalf("rows removed = %d, want 5", result.RowsRemoved)
	}
	cursor, err := repo.GetAgentDeliveryCursor(ctx, streamID)
	if err != nil {
		t.Fatal(err)
	}
	if cursor.ReceivedSequence != 5 || cursor.ProjectedSequence != 5 {
		t.Fatalf("cursor after retirement = %+v", cursor)
	}

	_, err = repo.ReceiveAgentDeliveryEvent(ctx, retentionTestEvent(streamID, 3, base, false, ""), 0)
	if !errors.Is(err, repoerrors.ErrAgentDeliveryReplayExpired) {
		t.Fatalf("expired replay receive err = %v, want ErrAgentDeliveryReplayExpired", err)
	}
	_, err = repo.ProjectAgentDeliveryEvent(ctx, retentionTestEvent(streamID, 3, base, false, ""), genericRetentionEffect(retentionTestEvent(streamID, 3, base, false, "")))
	if !errors.Is(err, repoerrors.ErrAgentDeliveryReplayExpired) {
		t.Fatalf("expired replay project err = %v, want ErrAgentDeliveryReplayExpired", err)
	}

	inserted, err := repo.ReceiveAgentDeliveryEvent(ctx, retentionTestEvent(streamID, 6, base, false, ""), 0)
	if err != nil || !inserted {
		t.Fatalf("later sequence receive: inserted=%v err=%v", inserted, err)
	}
	events, err := repo.ListUnprojectedAgentDeliveryEvents(ctx, streamID, 10)
	if err != nil || len(events) != 1 || events[0].Sequence != 6 {
		t.Fatalf("unprojected after retirement = %#v, err=%v", events, err)
	}
}

func TestRetireAgentDeliveryPrefixStopsAtUnprojectedAndGraceRows(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	streamID := "retention-gap-stream"
	base := time.Now().UTC().Add(-3 * time.Hour)
	receiveAndProjectRetentionEvent(t, repo, retentionTestEvent(streamID, 1, base, false, ""))
	if _, err := repo.ReceiveAgentDeliveryEvent(ctx, retentionTestEvent(streamID, 2, base, false, ""), 0); err != nil {
		t.Fatal(err)
	}

	result, err := repo.RetireAgentDeliveryPrefix(ctx, 0, time.Now().UTC(), models.AgentDeliveryRetirementBudget{Streams: 10, Rows: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsRemoved != 1 {
		t.Fatalf("rows removed with unprojected gap = %d, want 1", result.RowsRemoved)
	}

	freshStream := "retention-grace-stream"
	receiveAndProjectRetentionEvent(t, repo, retentionTestEvent(freshStream, 1, time.Now().UTC(), false, ""))
	result, err = repo.RetireAgentDeliveryPrefix(ctx, time.Hour, time.Now().UTC(), models.AgentDeliveryRetirementBudget{Streams: 10, Rows: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsRemoved != 0 {
		t.Fatalf("fresh rows removed within grace = %d, want 0", result.RowsRemoved)
	}
}

func TestRetireAgentDeliveryPrefixProtectsUnsettledTerminal(t *testing.T) {
	repo := newRepoForSessionTests(t)
	ctx := context.Background()
	streamID := "retention-terminal-stream"
	base := time.Now().UTC().Add(-3 * time.Hour)
	submission := &models.AgentDeliverySubmission{
		ID:            "retention-submission",
		SessionID:     "retention-session",
		IncarnationID: "retention-incarnation",
		PayloadHash:   "hash",
		Payload:       []byte(`{}`),
		State:         models.DeliverySubmissionPrepared,
	}
	if _, err := repo.PrepareAgentDeliverySubmission(ctx, submission); err != nil {
		t.Fatal(err)
	}
	terminal := retentionTestEvent(streamID, 1, base, true, submission.ID)
	receiveAndProjectRetentionEvent(t, repo, terminal)

	result, err := repo.RetireAgentDeliveryPrefix(ctx, 0, time.Now().UTC(), models.AgentDeliveryRetirementBudget{Streams: 10, Rows: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsRemoved != 0 {
		t.Fatalf("unsettled terminal removed = %d, want 0", result.RowsRemoved)
	}

	if _, err := repo.TransitionAgentDeliverySubmission(ctx, submission.ID, models.DeliverySubmissionPrepared, models.DeliverySubmissionCompleted, "completed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	result, err = repo.RetireAgentDeliveryPrefix(ctx, 0, time.Now().UTC(), models.AgentDeliveryRetirementBudget{Streams: 10, Rows: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsRemoved != 1 {
		t.Fatalf("settled terminal removed = %d, want 1", result.RowsRemoved)
	}
}
