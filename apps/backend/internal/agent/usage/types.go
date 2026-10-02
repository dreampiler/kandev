// Package usage provides subscription utilization tracking for agent providers.
// It fetches utilization data from provider APIs and local usage commands for agents
// authenticated via OAuth/subscription credentials rather than API keys.
package usage

import (
	"context"
	"time"
)

// BillingType identifies how an agent is billed.
type BillingType string

const (
	// BillingTypeAPIKey means the agent uses an API key with per-token billing.
	BillingTypeAPIKey BillingType = "api_key"
	// BillingTypeSubscription means the agent uses OAuth/subscription credentials.
	BillingTypeSubscription BillingType = "subscription"
)

// UtilizationWindow represents one rate-limit window's utilization.
type UtilizationWindow struct {
	Label          string    `json:"label"`           // e.g. "5-hour", "7-day"
	UtilizationPct float64   `json:"utilization_pct"` // 0–100
	ResetAt        time.Time `json:"reset_at"`

	// DurationSeconds and StartAt are the numeric shape of the window, added
	// alongside the display label. Routing needs them to compute elapsed time,
	// and they must be carried by the client rather than parsed out of Label:
	// a label is display copy and a provider may localize or reword it.
	// StartAt is ResetAt minus DurationSeconds and may be zero when the
	// provider did not supply a usable reset, which makes the window
	// unavailable for a pace score rather than a zero-usage window.
	DurationSeconds int64     `json:"duration_seconds,omitempty"`
	StartAt         time.Time `json:"start_at,omitempty"`

	// ModelID is the concrete model the window applies to, when the provider
	// scopes its limits per model. Empty means the window applies to every
	// model on the account.
	ModelID string `json:"model_id,omitempty"`

	// AmbiguousModelScope marks a window the provider scoped to a model it did
	// not identify. Claude reports only a display name for a scoped limit, and
	// a display label is not an identity, so such a window cannot be matched to
	// a candidate and is unavailable for a pace score.
	AmbiguousModelScope bool `json:"ambiguous_model_scope,omitempty"`
}

// UsableFor reports whether this window can score a pace for the given model.
// A window needs a numeric duration and a known reset instant, and must not
// carry an unidentified model scope. An unusable window is unknown usage, not
// zero usage and not unlimited capacity.
func (w UtilizationWindow) UsableFor(modelID string) bool {
	if w.DurationSeconds <= 0 || w.ResetAt.IsZero() || w.AmbiguousModelScope {
		return false
	}
	if w.ModelID != "" && modelID != "" && w.ModelID != modelID {
		return false
	}
	return w.StartAt.Before(w.ResetAt)
}

// ProviderUsage is the full utilization response for one provider credential.
type ProviderUsage struct {
	Provider  string              `json:"provider"`       // "anthropic", "openai", "google"
	Plan      string              `json:"plan,omitempty"` // e.g. "max", "pro", "plus", "free"
	Windows   []UtilizationWindow `json:"windows"`
	FetchedAt time.Time           `json:"fetched_at"`
}

// ProviderUsageClient fetches live utilization from a provider API.
type ProviderUsageClient interface {
	FetchUsage(ctx context.Context) (*ProviderUsage, error)
}
