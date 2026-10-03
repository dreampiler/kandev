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

	// Scope names which class of the account's models the window limits. An
	// account-wide plan window applies to every model; a free-request quota
	// only limits free models and a premium cap only premium ones.
	Scope WindowScope `json:"scope,omitempty"`

	// LimitReached records that the provider itself reports the window as
	// exhausted. It is kept even when the window has no usable reset, because an
	// exhausted account is a known fact rather than unknown usage.
	LimitReached bool `json:"limit_reached,omitempty"`
}

// WindowScope names the class of models a provider window limits.
type WindowScope string

const (
	// WindowScopeAllModels is an account-wide window.
	WindowScopeAllModels WindowScope = ""
	// WindowScopePaidModels limits only models billed against the plan.
	WindowScopePaidModels WindowScope = "paid_models"
	// WindowScopeFreeModels limits only the provider's free models.
	WindowScopeFreeModels WindowScope = "free_models"
	// WindowScopePremiumModels limits only the provider's premium models.
	WindowScopePremiumModels WindowScope = "premium_models"
)

// ModelClass is a provider's own classification of one model. The unknown
// class matches only account-wide windows, so an unclassified model is never
// charged with a class-specific quota it may not belong to.
type ModelClass string

const (
	ModelClassUnknown  ModelClass = ""
	ModelClassFree     ModelClass = "free"
	ModelClassStandard ModelClass = "standard"
	ModelClassPremium  ModelClass = "premium"
)

// AppliesTo reports whether a window with this scope limits a model of the
// given class.
func (s WindowScope) AppliesTo(class ModelClass) bool {
	switch s {
	case WindowScopeAllModels:
		return true
	case WindowScopeFreeModels:
		return class == ModelClassFree
	case WindowScopePaidModels:
		return class == ModelClassStandard || class == ModelClassPremium
	case WindowScopePremiumModels:
		return class == ModelClassPremium
	default:
		return false
	}
}

// Exhausted reports whether the window is known to have no remaining capacity.
func (w UtilizationWindow) Exhausted() bool {
	return w.LimitReached || w.UtilizationPct >= 100
}

// UsableFor reports whether this window can score a pace for the given model.
// A window needs a numeric duration and a known reset instant, and must not
// carry an unidentified model scope. An unusable window is unknown usage, not
// zero usage and not unlimited capacity.
//
// A window that names a specific model is only usable for that model. When the
// candidate's own model is unknown the window cannot be matched to it, so it
// stays unusable: attributing a sibling model's consumption to a candidate is
// exactly the substitution the unknown state exists to prevent.
func (w UtilizationWindow) UsableFor(modelID string) bool {
	if w.DurationSeconds <= 0 || w.ResetAt.IsZero() || w.AmbiguousModelScope {
		return false
	}
	if w.ModelID != "" && w.ModelID != modelID {
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
