package dynamic

import (
	"math/big"
	"strings"
)

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

// HostExecutorID is the host-local standalone executor. Anything else is a
// container, SSH host, Kubernetes pod or cloud executor, where the agent
// authenticates against a different provider account than the backend host.
const HostExecutorID = "exec-local"

// IsRemoteExecutor reports whether an executor ID names an environment other than
// the backend host. An empty ID keeps the host default, which is what an
// unqualified profile launches on; remoteness is only ever asserted on positive
// evidence, so a caller that does not know the executor is not treated as remote.
func IsRemoteExecutor(executorID string) bool {
	switch strings.TrimSpace(executorID) {
	case "", HostExecutorID:
		return false
	default:
		return true
	}
}

// Tier is one maximal contiguous run of candidates connected by joins. Tiers
// are numbered from one in list order and are always traversed in that order.
type Tier struct {
	Index      int
	HeadID     string
	Policy     TierPolicy
	Candidates []Candidate
	// Configured reports that the head actually stored a tier policy. A legacy
	// or default head carries the derived defaults but is not configured, so a
	// persisted route reason keeps its established code for it.
	Configured bool
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
			Configured: candidate.Selection.Tier != nil,
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
