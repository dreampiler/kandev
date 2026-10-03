package backendapp

import (
	"context"
	"sync"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	settingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
	agentusage "github.com/kandev/kandev/internal/agent/usage"
	sqliterepo "github.com/kandev/kandev/internal/task/repository/sqlite"
)

// accountLedger answers recorded usage per profile: each listed profile adds
// its own turns, so a sum proves which profiles a window counted.
type accountLedger struct {
	mu           sync.Mutex
	turns        map[string]int64
	spans        []time.Duration
	observations []sqliterepo.UsageLimitObservation
}

func (l *accountLedger) GetManualWindowUsage(
	ctx context.Context, id string, start, end time.Time,
) (sqliterepo.ManualWindowUsage, error) {
	return l.GetManualWindowUsageForProfiles(ctx, []string{id}, start, end)
}

func (l *accountLedger) GetManualWindowUsageForProfiles(
	_ context.Context, ids []string, start, end time.Time,
) (sqliterepo.ManualWindowUsage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.spans = append(l.spans, end.Sub(start))
	usage := sqliterepo.ManualWindowUsage{}
	for _, id := range ids {
		usage.EventCount += l.turns[id]
		usage.TokensTotal += 1000 * l.turns[id]
	}
	return usage, nil
}

func (l *accountLedger) InsertUsageLimitObservation(_ context.Context, observation sqliterepo.UsageLimitObservation) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.observations = append(l.observations, observation)
	return nil
}

func zenAccountAdapter(t *testing.T) *usageProviderAdapter {
	t.Helper()
	store := &agentRowSettingsStore{
		agents: map[string]*settingsmodels.Agent{"oc-row": {ID: "oc-row", Name: openCodeACPAgentID}},
		profiles: map[string]*settingsmodels.AgentProfile{
			"zen-a": {ID: "zen-a", AgentID: "oc-row", Model: "opencode/ling-free"},
			"zen-b": {ID: "zen-b", AgentID: "oc-row", Model: "opencode/big-pickle"},
			"dp":    {ID: "dp", AgentID: "oc-row", Model: "llmgateway/deepseek"},
		},
	}
	return &usageProviderAdapter{
		svc: agentusage.NewUsageService(), settingsStore: store,
		bindings: &usageBindingResolver{home: t.TempDir(), getenv: func(string) string { return "" }},
	}
}

// TestUnknownCandidatesCarryTheirAccountsRecordedUsage pins internal
// accumulation: a candidate whose provider usage is unknown is ordered by what
// Kandev recorded for its whole account over the trailing day.
func TestUnknownCandidatesCarryTheirAccountsRecordedUsage(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	ledger := &accountLedger{turns: map[string]int64{"zen-a": 3, "zen-b": 4, "dp": 2}}
	adapter := zenAccountAdapter(t)
	snapshot := newDynamicUsageSnapshot(adapter, ledger, func() time.Time { return now }).
		WithInternalUsage(newAccountUsageReader(adapter, ledger))

	scores, err := snapshot.UsageSnapshot(context.Background(), dynamicruntime.Profile{
		Candidates: []dynamicruntime.Candidate{automaticCandidate("zen-a", dynamicruntime.UsageNone, "")},
	})
	if err != nil {
		t.Fatalf("UsageSnapshot: %v", err)
	}
	internal := scores["zen-a"].Internal
	if !internal.Known || internal.Turns != 7 {
		t.Fatalf("internal = %#v, want both Zen profiles' 7 recorded turns", internal)
	}
	if ledger.spans[0] != internalRankingWindow {
		t.Fatalf("window = %v, want the trailing day", ledger.spans[0])
	}
}

func TestLimitRecorderSnapshotsTheAccountAtTheHit(t *testing.T) {
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	ledger := &accountLedger{turns: map[string]int64{"zen-a": 30, "zen-b": 10}}
	adapter := zenAccountAdapter(t)
	recorder := newUsageLimitRecorder(newAccountUsageReader(adapter, ledger), ledger, nil)

	recorder.ObserveLimit(context.Background(), "zen-b", routingerr.CodeQuotaLimited, at)
	recorder.wait()

	if len(ledger.observations) != 1 {
		t.Fatalf("observations = %d, want one", len(ledger.observations))
	}
	observation := ledger.observations[0]
	if observation.TurnsDay != 40 || observation.Turns5h != 40 || observation.TurnsWeek != 40 {
		t.Fatalf("observation = %#v, want the account's 40 turns in each window", observation)
	}
	if observation.AccountKey == "" || observation.Code != string(routingerr.CodeQuotaLimited) || !observation.ObservedAt.Equal(at) {
		t.Fatalf("observation = %#v, want the account, code and hit time", observation)
	}
}

func TestLimitHitSummaryUsesTheLowerMedian(t *testing.T) {
	last := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	hits := []sqliterepo.UsageLimitObservation{
		{TurnsDay: 400, ObservedAt: last.Add(-48 * time.Hour)},
		{TurnsDay: 100, ObservedAt: last},
		{TurnsDay: 120, ObservedAt: last.Add(-24 * time.Hour)},
		{TurnsDay: 900, ObservedAt: last.Add(-72 * time.Hour)},
	}
	summary := summarizeLimitHits(hits)
	if summary.Count != 4 || summary.MedianTurnsDay != 120 || !summary.LastAt.Equal(last) {
		t.Fatalf("summary = %#v, want 4 hits, median 120 turns a day, last at the newest", summary)
	}
}
