package backendapp

import (
	"context"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

// previewSuspensionReader is the detailed health reader: it separates a
// running suspension from an expired one that the next selection retries.
type previewSuspensionReader interface {
	CandidateSuspension(ctx context.Context, executionProfileID string, now time.Time) (dynamicruntime.CandidateSuspension, bool)
}

// previewSuspensions reads every candidate's resource health when the health
// reader can explain it. A candidate missing from the result has an unknown
// verdict and falls back to the open-circuit answer.
func (s *dynamicUsageSnapshot) previewSuspensions(
	ctx context.Context,
	profile dynamicruntime.Profile,
	now time.Time,
) map[string]dynamicruntime.CandidateSuspension {
	reader, ok := s.health.(previewSuspensionReader)
	if !ok {
		return nil
	}
	suspensions := make(map[string]dynamicruntime.CandidateSuspension, len(profile.Candidates))
	for _, candidate := range profile.Candidates {
		if suspension, known := reader.CandidateSuspension(ctx, candidate.ID, now); known {
			suspensions[candidate.ID] = suspension
		}
	}
	return suspensions
}

// suspensionIneligibility maps a blocking suspension to its bounded code. An
// expired suspension is not a block: the next selection claims its probe.
func suspensionIneligibility(suspension dynamicruntime.CandidateSuspension) string {
	switch suspension.State {
	case dynamicruntime.ResourceWaiting:
		return dynamicruntime.IneligibleCircuit
	case dynamicruntime.ResourceProbing:
		return dynamicruntime.IneligibleProbing
	default:
		return ""
	}
}
