package messagequeue

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/stretchr/testify/require"
)

func newPeerReportBatchService(t *testing.T, repo Repository) *Service {
	t.Helper()
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "console", OutputPath: "stderr"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	return NewService(repo, 10, log)
}

func newPeerReportBatchFixture(t *testing.T, repo Repository) *Service {
	t.Helper()
	seedQueueSessionIdentity(t, repo, QueueSessionIdentity{
		TaskID:               "task",
		SessionID:            "session",
		SessionIncarnationID: "incarnation",
	})
	return newPeerReportBatchService(t, repo)
}

func queuePeerReport(t *testing.T, svc *Service, sender, content string) {
	t.Helper()
	_, err := svc.QueueMessageWithMetadata(context.Background(), "session", "task", content, "model",
		QueuedByAgent, false, nil, map[string]interface{}{MetadataSenderTaskID: sender})
	require.NoError(t, err)
}

func reservePeerReportBatch(t *testing.T, svc *Service, repo Repository) []*QueuedMessage {
	t.Helper()
	ctx := context.Background()
	identity, err := repo.ResolveSessionIdentity(ctx, "task", "session")
	require.NoError(t, err)
	batch, autoRun, err := svc.ReservePeerReportBatchWithAutoRunForSession(ctx, identity)
	require.NoError(t, err)
	require.True(t, autoRun)
	return batch
}

func reserveNextEntry(t *testing.T, repo Repository) *QueuedMessage {
	t.Helper()
	entry, ok, err := repo.ReserveHeadIfAutoRun(context.Background(), "session")
	require.NoError(t, err)
	require.True(t, ok)
	return entry
}

func TestReservePeerReportBatch_GroupsDifferentChildReportsInFIFOOrder(t *testing.T) {
	for name, repo := range repositoriesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			svc := newPeerReportBatchFixture(t, repo)
			queuePeerReport(t, svc, "child-a", "first report")
			queuePeerReport(t, svc, "child-b", "second report")
			queuePeerReport(t, svc, "child-c", "third report")

			batch := reservePeerReportBatch(t, svc, repo)

			require.Len(t, batch, 3)
			require.Equal(t, "child-a", PeerReportSenderTaskID(batch[0]))
			require.Equal(t, "child-b", PeerReportSenderTaskID(batch[1]))
			require.Equal(t, "child-c", PeerReportSenderTaskID(batch[2]))
		})
	}
}

func TestReservePeerReportBatch_StopsAtUserInstructionAndKeepsItQueued(t *testing.T) {
	for name, repo := range repositoriesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			svc := newPeerReportBatchFixture(t, repo)
			queuePeerReport(t, svc, "child-a", "report one")
			_, err := svc.QueueMessage(context.Background(), "session", "task", "operator instruction",
				"model", QueuedByUser, false, nil)
			require.NoError(t, err)
			queuePeerReport(t, svc, "child-b", "report two")

			batch := reservePeerReportBatch(t, svc, repo)

			require.Len(t, batch, 1)
			require.Equal(t, "report one", batch[0].Content)
			require.Equal(t, "operator instruction", reserveNextEntry(t, repo).Content)
		})
	}
}

func TestReservePeerReportBatch_StopsAtEntryBoundAndLeavesRemainderQueued(t *testing.T) {
	for name, repo := range repositoriesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			svc := newPeerReportBatchFixture(t, repo)
			total := MaxPeerReportBatchEntries + 2
			for index := range total {
				queuePeerReport(t, svc, fmt.Sprintf("child-%d", index), fmt.Sprintf("report %d", index))
			}

			batch := reservePeerReportBatch(t, svc, repo)

			require.Len(t, batch, MaxPeerReportBatchEntries)
			require.Equal(t, fmt.Sprintf("report %d", MaxPeerReportBatchEntries),
				reserveNextEntry(t, repo).Content)
		})
	}
}

func TestReservePeerReportBatch_StopsAtByteCeiling(t *testing.T) {
	for name, repo := range repositoriesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			svc := newPeerReportBatchFixture(t, repo)
			half := strings.Repeat("a", MaxPeerReportBatchBytes/2)
			queuePeerReport(t, svc, "child-a", half)
			queuePeerReport(t, svc, "child-b", half)
			queuePeerReport(t, svc, "child-c", "small")

			batch := reservePeerReportBatch(t, svc, repo)

			require.Len(t, batch, 2)
			require.Equal(t, "small", reserveNextEntry(t, repo).Content)
		})
	}
}

func TestReservePeerReportBatch_ReportsAutoRunOffWithoutReserving(t *testing.T) {
	for name, repo := range repositoriesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			svc := newPeerReportBatchFixture(t, repo)
			queuePeerReport(t, svc, "child-a", "report one")
			require.NoError(t, svc.SetAutoRun(context.Background(), "session", false))
			identity, err := repo.ResolveSessionIdentity(context.Background(), "task", "session")
			require.NoError(t, err)

			batch, autoRun, err := svc.ReservePeerReportBatchWithAutoRunForSession(
				context.Background(), identity)

			require.NoError(t, err)
			require.False(t, autoRun)
			require.Empty(t, batch)
		})
	}
}

func TestComposePeerReportBatch_KeepsPerSenderAttribution(t *testing.T) {
	batch := []*QueuedMessage{
		{ID: "one", TaskID: "task", SessionID: "session", QueuedBy: QueuedByAgent,
			Content:  "alpha",
			Metadata: map[string]interface{}{MetadataSenderTaskID: "child-a"}},
		{ID: "two", TaskID: "task", SessionID: "session", QueuedBy: QueuedByAgent,
			Content:  "beta",
			Metadata: map[string]interface{}{MetadataSenderTaskID: "child-b"}},
	}

	composed := ComposePeerReportBatch(batch)

	require.Equal(t, "one", composed.ID)
	require.Equal(t, "child-a", PeerReportSenderTaskID(composed))
	require.Contains(t, composed.Content, "[peer report 1/2] from task child-a\nalpha")
	require.Contains(t, composed.Content, "[peer report 2/2] from task child-b\nbeta")
	require.Equal(t, "alpha", batch[0].Content)
}

func TestIsOrdinaryPeerReport_RejectsNonAgentAndUnattributedEntries(t *testing.T) {
	require.False(t, IsOrdinaryPeerReport(nil))
	require.False(t, IsOrdinaryPeerReport(&QueuedMessage{QueuedBy: QueuedByUser}))
	require.False(t, IsOrdinaryPeerReport(&QueuedMessage{QueuedBy: QueuedByAgent}))
	require.False(t, IsOrdinaryPeerReport(&QueuedMessage{
		QueuedBy:    QueuedByAgent,
		Metadata:    map[string]interface{}{MetadataSenderTaskID: "child-a"},
		Attachments: []MessageAttachment{{AttachmentID: "attachment"}},
	}))
	require.True(t, IsOrdinaryPeerReport(&QueuedMessage{
		QueuedBy: QueuedByAgent,
		Metadata: map[string]interface{}{MetadataSenderTaskID: "child-a"},
	}))
}
