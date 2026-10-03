package dynamic

import (
	"context"
	"math/rand/v2"
	"time"
)

// SelectionHistory reports when each concrete candidate of a logical profile
// was last selected. Round-robin selection continues from it, so the rotation
// survives a restart. The route attempt log is its durable source.
type SelectionHistory interface {
	LastSelections(ctx context.Context, logicalProfileID string) (map[string]time.Time, error)
}

// WithSelectionHistory enables round-robin continuation across restarts.
func WithSelectionHistory(history SelectionHistory) EngineOption {
	return func(engine *Engine) { engine.history = history }
}

// WithRandomPick replaces the random index source, for deterministic tests.
func WithRandomPick(pick func(n int) int) EngineOption {
	return func(engine *Engine) { engine.pick = pick }
}

func defaultRandomPick(n int) int { return rand.IntN(n) }

// rankInputs gathers everything one decision ranks against, outside the engine
// lock: the usage snapshot, the random source and the round-robin history.
func (e *Engine) rankInputs(ctx context.Context, profile Profile) RankOptions {
	return RankOptions{
		Scores:     e.usageSnapshot(ctx, profile),
		Pick:       e.pick,
		LastPicked: LastPickedByTier(profile, e.lastSelections(ctx, profile.ID)),
	}
}

// lastSelections merges the durable history with selections this process made
// since, so two quick decisions do not both continue from the same entry.
func (e *Engine) lastSelections(ctx context.Context, profileID string) map[string]time.Time {
	merged := make(map[string]time.Time)
	if e.history != nil {
		if stored, err := e.history.LastSelections(ctx, profileID); err == nil {
			for id, at := range stored {
				merged[id] = at
			}
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, at := range e.recentPicks[profileID] {
		if at.After(merged[id]) {
			merged[id] = at
		}
	}
	return merged
}

// notePickLocked records a selection for round-robin continuation. The caller
// holds e.mu.
func (e *Engine) notePickLocked(profileID string, candidateID string, at time.Time) {
	if e.recentPicks == nil {
		e.recentPicks = make(map[string]map[string]time.Time)
	}
	picks := e.recentPicks[profileID]
	if picks == nil {
		picks = make(map[string]time.Time)
		e.recentPicks[profileID] = picks
	}
	picks[candidateID] = at
}

// LastPickedByTier maps each tier head to the tier member selected most
// recently, which is where a round-robin tier continues from.
func LastPickedByTier(profile Profile, last map[string]time.Time) map[string]string {
	picked := make(map[string]string)
	for _, tier := range DeriveTiers(profile.Candidates) {
		var latest time.Time
		for _, candidate := range tier.Candidates {
			if at, ok := last[candidate.ID]; ok && at.After(latest) {
				latest = at
				picked[tier.HeadID] = candidate.ID
			}
		}
	}
	return picked
}

// at stamps the decision clock onto gathered inputs.
func (o RankOptions) at(now time.Time) RankOptions {
	o.Now = now
	return o
}
