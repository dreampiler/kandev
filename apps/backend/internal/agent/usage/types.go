// Package usage provides subscription utilization tracking for agent providers.
// It fetches utilization data from provider APIs (Anthropic, OpenAI) for agents
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
	// Model is the explicit provider scope, empty for shared account windows.
	Model          string    `json:"-"`
	Label          string    `json:"label"`           // e.g. "5-hour", "7-day"
	UtilizationPct float64   `json:"utilization_pct"` // 0?100
	ResetAt        time.Time `json:"reset_at"`
	// Observation is additive provenance for strict readers; legacy values and wire data are unchanged.
	Observation *WindowObservation `json:"-"`

	// DurationSeconds and StartAt are the numeric shape of the window. Routing
	// needs them to compute elapsed time.
	DurationSeconds int64     `json:"duration_seconds,omitempty"`
	StartAt         time.Time `json:"start_at,omitempty"`
	// ModelID is the concrete model the window applies to when the provider
	// scopes its limits per model. Empty applies to every model on the account.
	ModelID string `json:"model_id,omitempty"`
	// Scope names which class of the account's models the window limits.
	Scope WindowScope `json:"scope,omitempty"`
	// LimitReached records that the provider itself reports the window as
	// exhausted.
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
	// WindowScopePremiumModels limits only premium models.
	WindowScopePremiumModels WindowScope = "premium_models"
)

// Exhausted reports whether the window is known to have no remaining capacity.
func (w UtilizationWindow) Exhausted() bool {
	return w.LimitReached || w.UtilizationPct >= 100
}

// WindowObservation distinguishes provider measurements from legacy defaults.
type WindowObservation struct {
	UtilizationObserved bool
	ResetAtObserved     bool
}

func legacyUtilization(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func utilizationObserved(value *float64) bool {
	return value != nil && *value >= 0 && *value <= 100
}

// ModelClass is the provider's cost class for a model.
type ModelClass string

const (
	ModelClassUnknown ModelClass = "unknown"
	ModelClassFree    ModelClass = "free"
	ModelClassPremium ModelClass = "premium"
)

// ProviderUsage is the full utilization response for one provider credential.
type ProviderUsage struct {
	FreeModelDailyRequests *FreeModelCounter `json:"free_model_daily_requests,omitempty"`
	// AccountRef is the existing account sent with this successful request, never a profile or cache key.
	AccountRef string `json:"-"`
	// OrganizationRef is the actual provider organization returned for this token.
	OrganizationRef string              `json:"-"`
	Provider        string              `json:"provider"`       // "anthropic", executionProviderOpenAI
	Plan            string              `json:"plan,omitempty"` // e.g. "max", "pro", "plus", "free"
	Windows         []UtilizationWindow `json:"windows"`
	FetchedAt       time.Time           `json:"fetched_at"`
}

type FreeModelCounter struct {
	Used      float64 `json:"used"`
	Limit     float64 `json:"limit"`
	Remaining float64 `json:"remaining"`
}

// ProviderUsageClient fetches live utilization from a provider API.
type ProviderUsageClient interface {
	FetchUsage(ctx context.Context) (*ProviderUsage, error)
}
