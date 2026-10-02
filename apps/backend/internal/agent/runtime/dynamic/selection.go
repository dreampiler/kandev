package dynamic

import "math/big"

// Tier selection is a closed set. The values are stored in the route document
// and are never inferred from a profile name.
type TierMode string

const (
	TierModeOrder TierMode = "order"
	TierModePace  TierMode = "pace"
	TierModeCost  TierMode = "cost"
)

type FailureDirection string

const (
	FailureSameTierNext FailureDirection = "same_tier_next"
	FailureNextTier     FailureDirection = "next_tier"
)

// CostClass is configured marginal-cost metadata used to order a tier. An
// empty class is the legacy unknown classification and sorts after the three
// known ones; it never claims a numerical price.
type CostClass string

const (
	CostFree         CostClass = "free"
	CostSubscription CostClass = "subscription"
	CostMetered      CostClass = "metered"
)

type UsageSource string

const (
	UsageAutomatic UsageSource = "automatic"
	UsageManual    UsageSource = "manual"
	UsageNone      UsageSource = "none"
)

// UsageWindow is one user-entered reset window. Limit is a positive decimal
// string so a money allowance keeps subcent precision; ResetAnchor carries the
// local reset time, weekday/day-of-month and fixed-window phase, and Timezone
// is a validated IANA name. A month window uses calendar boundaries, so its
// limit is divided by the elapsed fraction of the current calendar month.
type UsageWindow struct {
	Period      string
	Unit        string
	Limit       string
	ResetAnchor string
	Timezone    string
}

// LimitValue parses the allowance exactly. Callers must not treat an unparsable
// value as zero usage or unlimited capacity.
func (w UsageWindow) LimitValue() (*big.Rat, bool) {
	value, ok := new(big.Rat).SetString(w.Limit)
	if !ok || value.Sign() <= 0 {
		return nil, false
	}
	return value, true
}

type TierPolicy struct {
	Mode      TierMode
	OnFailure FailureDirection
}

type ModelOptions struct {
	Cost                 CostClass
	UsageSource          UsageSource
	ReservedUserSharePct int
	Windows              []UsageWindow
}

// Selection is one candidate row's normalized tier and model options. Tier is
// present only on a tier's first row.
type Selection struct {
	JoinPrevious bool
	Tier         *TierPolicy
	Model        ModelOptions
}

// Tier is one maximal contiguous run of candidates connected by joins. Tiers
// are numbered from one in list order and are always traversed in that order.
type Tier struct {
	Index      int
	HeadID     string
	Policy     TierPolicy
	Candidates []Candidate
}

// DeriveTiers groups an ordered candidate list into contiguous tiers. A row
// whose join flag is set belongs to the tier above it. Identity stays the
// concrete profile ID: the index is derived adjacency and is not persisted.
func DeriveTiers(candidates []Candidate) []Tier {
	tiers := make([]Tier, 0, len(candidates))
	for _, candidate := range candidates {
		// A row can only join a tier that already exists above it.
		if candidate.Selection.JoinPrevious && len(tiers) > 0 {
			current := &tiers[len(tiers)-1]
			current.Candidates = append(current.Candidates, candidate)
			continue
		}
		tiers = append(tiers, Tier{
			Index:      len(tiers) + 1,
			HeadID:     candidate.ID,
			Policy:     tierPolicyFor(candidate),
			Candidates: []Candidate{candidate},
		})
	}
	return tiers
}

// tierPolicyFor resolves a head's policy, falling back to ordered routing with
// same-tier-next fallback. A head without stored tier metadata is a legacy row
// or a fragment that inherited an empty policy, and both mean the defaults.
func tierPolicyFor(candidate Candidate) TierPolicy {
	tier := candidate.Selection.Tier
	if tier == nil {
		return TierPolicy{Mode: TierModeOrder, OnFailure: FailureSameTierNext}
	}
	policy := *tier
	if policy.Mode == "" {
		policy.Mode = TierModeOrder
	}
	if policy.OnFailure == "" {
		policy.OnFailure = FailureSameTierNext
	}
	return policy
}
