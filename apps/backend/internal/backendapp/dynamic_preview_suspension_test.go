package backendapp

import (
	"context"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

// stubSuspensionHealth answers detailed resource health per candidate.
type stubSuspensionHealth struct {
	suspensions map[string]dynamicruntime.CandidateSuspension
}

func (s stubSuspensionHealth) OpenCircuit(context.Context, string, time.Time) (bool, bool) {
	return false, true
}

func (s stubSuspensionHealth) CandidateSuspension(
	_ context.Context,
	executionProfileID string,
	_ time.Time,
) (dynamicruntime.CandidateSuspension, bool) {
	suspension, ok := s.suspensions[executionProfileID]
	if !ok {
		return dynamicruntime.CandidateSuspension{State: dynamicruntime.ResourceAvailable}, true
	}
	return suspension, true
}

func TestPreviewSeparatesExpiredFromRunningSuspension(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	waiting := automaticCandidate("waiting", dynamicruntime.UsageNone, dynamicruntime.CostFree)
	expired := automaticCandidate("expired", dynamicruntime.UsageNone, dynamicruntime.CostFree)
	probing := automaticCandidate("probing", dynamicruntime.UsageNone, dynamicruntime.CostFree)
	profile := dynamicruntime.Profile{ID: "p", Version: 1, Candidates: []dynamicruntime.Candidate{waiting, expired, probing}}

	snapshot := newDynamicUsageSnapshot(nil, nil, func() time.Time { return now })
	snapshot.WithPreviewHealth(stubSuspensionHealth{suspensions: map[string]dynamicruntime.CandidateSuspension{
		"waiting": {State: dynamicruntime.ResourceWaiting, Until: now.Add(time.Hour), Scope: dynamicruntime.SuspensionScopeModel},
		"expired": {State: dynamicruntime.ResourceExpired, Until: now.Add(-time.Hour)},
		"probing": {State: dynamicruntime.ResourceProbing, Until: now.Add(10 * time.Second)},
	}})

	preview := snapshot.PreviewDynamicSelection(context.Background(), profile, nil, now)
	if preview.CandidateID != "expired" {
		t.Fatalf("preview chose %q, want the expired candidate the next selection retries", preview.CandidateID)
	}
	byID := make(map[string]dynamicruntime.PreviewEntry, len(preview.Considered))
	for _, entry := range preview.Considered {
		byID[entry.CandidateID] = entry
	}
	if entry := byID["waiting"]; entry.Eligible || entry.IneligibleReason != dynamicruntime.IneligibleCircuit ||
		entry.Suspension.State != dynamicruntime.ResourceWaiting {
		t.Fatalf("waiting entry = %#v", entry)
	}
	if entry := byID["probing"]; entry.Eligible || entry.IneligibleReason != dynamicruntime.IneligibleProbing {
		t.Fatalf("probing entry = %#v", entry)
	}
	if entry := byID["expired"]; !entry.Eligible || entry.Suspension.State != dynamicruntime.ResourceExpired {
		t.Fatalf("expired entry = %#v", entry)
	}
}
