package runtime

import (
	"math/big"
	"testing"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

func bigRat(value string) *big.Rat {
	parsed, _ := new(big.Rat).SetString(value)
	return parsed
}

const dynamicSelectionRouteDocument = `{
	"version": 1,
	"transient": {"retry": {"enabled": true, "max_retries": 2, "initial_interval_seconds": 5},
		"wait_for_reset": {"enabled": true, "max_wait_seconds": 300}, "on_exhausted": "skip"},
	"hard": {"on_exhausted": "stop"},
	"unclassified": {"enabled": true, "consecutive_failure_threshold": 3},
	"selection": {
		"join_previous": true,
		"model": {
			"cost": "metered",
			"usage_source": "manual",
			"reserved_user_share_pct": 25,
			"windows": [{
				"period": "month", "unit": "money", "limit": "120.50",
				"reset": {"anchor": "09:00", "timezone": "Asia/Seoul"}
			}]
		}
	}
}`

func TestDecodeDynamicRoutePolicySplitsSelectionFromFailurePolicy(t *testing.T) {
	document, legacy, selection, err := decodeDynamicRoutePolicy(dynamicSelectionRouteDocument)
	if err != nil {
		t.Fatalf("decodeDynamicRoutePolicy: %v", err)
	}
	if legacy != nil {
		t.Fatalf("legacy rules = %#v, want none for a versioned document", legacy)
	}
	if !document.Transient.Retry.Enabled || document.Transient.Retry.MaxRetries != 2 ||
		document.Transient.WaitForReset.MaxWaitSeconds != 300 ||
		document.Transient.OnExhausted != "skip" || document.Hard.OnExhausted != "stop" {
		t.Fatalf("failure policy was not preserved: %#v", document)
	}
	if !selection.JoinPrevious {
		t.Fatalf("join = false, want a joined row")
	}
	if selection.Tier != nil {
		t.Fatalf("tier = %#v, want none on a joined row", selection.Tier)
	}
	if selection.Model.Cost != dynamic.CostMetered || selection.Model.UsageSource != dynamic.UsageManual {
		t.Fatalf("model = %#v", selection.Model)
	}
	if selection.Model.ReservedUserSharePct != 25 {
		t.Fatalf("reserved share = %d, want 25", selection.Model.ReservedUserSharePct)
	}
	if len(selection.Model.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(selection.Model.Windows))
	}
	window := selection.Model.Windows[0]
	if window.Period != "month" || window.Unit != "money" || window.Limit != "120.50" ||
		window.ResetAnchor != "09:00" || window.Timezone != "Asia/Seoul" {
		t.Fatalf("window = %#v", window)
	}
	// A money allowance must keep subcent precision rather than round-trip
	// through a binary float.
	limit, ok := window.LimitValue()
	if !ok {
		t.Fatalf("LimitValue rejected %q", window.Limit)
	}
	if limit.Cmp(bigRat("120.50")) != 0 {
		t.Fatalf("limit = %s, want exactly 120.50", limit.RatString())
	}
}

func TestDecodeDynamicRoutePolicyHeadCarriesTier(t *testing.T) {
	_, _, selection, err := decodeDynamicRoutePolicy(`{
		"version": 1,
		"transient": {"on_exhausted": "skip"},
		"hard": {"on_exhausted": "skip"},
		"selection": {
			"join_previous": false,
			"tier": {"mode": "pace", "on_failure": "next_tier"},
			"model": {"cost": "free", "usage_source": "none", "reserved_user_share_pct": 0}
		}
	}`)
	if err != nil {
		t.Fatalf("decodeDynamicRoutePolicy: %v", err)
	}
	if selection.JoinPrevious || selection.Tier == nil {
		t.Fatalf("selection = %#v, want an unjoined head owning a tier", selection)
	}
	if selection.Tier.Mode != dynamic.TierModePace || selection.Tier.OnFailure != dynamic.FailureNextTier {
		t.Fatalf("tier = %#v", selection.Tier)
	}
}

func TestDecodeDynamicRoutePolicyAbsentSelectionIsLegacyDefaults(t *testing.T) {
	for name, raw := range map[string]string{
		"versioned":  `{"version":1,"transient":{"on_exhausted":"skip"},"hard":{"on_exhausted":"stop"}}`,
		"legacy map": `{"on_provider_error":"try_next"}`,
	} {
		t.Run(name, func(t *testing.T) {
			document, _, selection, err := decodeDynamicRoutePolicy(raw)
			if err != nil {
				t.Fatalf("decodeDynamicRoutePolicy: %v", err)
			}
			if selection.JoinPrevious || selection.Tier != nil {
				t.Fatalf("selection = %#v, want no tier metadata", selection)
			}
			if document.Transient.OnExhausted != "skip" {
				t.Fatalf("transient = %#v", document.Transient)
			}
		})
	}
}

func TestDecodeDynamicRoutePolicyDoesNotLeakSelectionIntoEvaluation(t *testing.T) {
	// Selection is additive metadata; the failure document the engine evaluates
	// must stay exactly the versioned document the server wrote.
	document, _, _, err := decodeDynamicRoutePolicy(dynamicSelectionRouteDocument)
	if err != nil {
		t.Fatalf("decodeDynamicRoutePolicy: %v", err)
	}
	if document.Transient.OnExhausted == "" || document.Hard.OnExhausted == "" {
		t.Fatalf("document = %#v, want both classes present", document)
	}
	if document.Transient.Retry.MaxRetries != 2 || document.Hard.Retry.Enabled {
		t.Fatalf("document = %#v, want only the transient class to carry retry settings", document)
	}
}

func TestDeriveTiersGroupsContiguousJoinedRows(t *testing.T) {
	candidates := []dynamic.Candidate{
		{ID: "a", Selection: dynamic.Selection{Tier: &dynamic.TierPolicy{
			Mode: dynamic.TierModePace, OnFailure: dynamic.FailureSameTierNext}}},
		{ID: "b", Selection: dynamic.Selection{JoinPrevious: true}},
		{ID: "c", Selection: dynamic.Selection{JoinPrevious: true}},
		{ID: "d", Selection: dynamic.Selection{Tier: &dynamic.TierPolicy{
			Mode: dynamic.TierModeCost, OnFailure: dynamic.FailureNextTier}}},
	}
	tiers := dynamic.DeriveTiers(candidates)
	if len(tiers) != 2 {
		t.Fatalf("tiers = %d, want 2", len(tiers))
	}
	if tiers[0].Index != 1 || tiers[0].HeadID != "a" || tiers[0].Policy.Mode != dynamic.TierModePace {
		t.Fatalf("tier 1 = %#v", tiers[0])
	}
	if len(tiers[0].Candidates) != 3 {
		t.Fatalf("tier 1 candidates = %d, want 3", len(tiers[0].Candidates))
	}
	if tiers[1].Index != 2 || tiers[1].HeadID != "d" || len(tiers[1].Candidates) != 1 {
		t.Fatalf("tier 2 = %#v", tiers[1])
	}
}

func TestDeriveTiersGivesLegacyRowsTheDocumentedDefaults(t *testing.T) {
	// A row with no stored selection is its own tier with ordered routing and
	// same-tier-next fallback, so joining the whole list remains possible.
	tiers := dynamic.DeriveTiers([]dynamic.Candidate{{ID: "a"}, {ID: "b"}})
	if len(tiers) != 2 {
		t.Fatalf("tiers = %d, want 2", len(tiers))
	}
	for _, tier := range tiers {
		if tier.Policy.Mode != dynamic.TierModeOrder || tier.Policy.OnFailure != dynamic.FailureSameTierNext {
			t.Fatalf("tier %d policy = %#v, want the legacy defaults", tier.Index, tier.Policy)
		}
	}
}

func TestDeriveTiersHonoursTheFirstRowJoinFlag(t *testing.T) {
	// The server rejects a first-row join, but a hand-built profile that carries
	// one must not silently swallow the following row.
	tiers := dynamic.DeriveTiers([]dynamic.Candidate{
		{ID: "a", Selection: dynamic.Selection{JoinPrevious: true}},
		{ID: "b"},
	})
	if len(tiers) != 2 {
		t.Fatalf("tiers = %d, want 2", len(tiers))
	}
}
