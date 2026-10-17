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
	UtilizationPct float64   `json:"utilization_pct"` // 0–100
	ResetAt        time.Time `json:"reset_at"`
	// Observation is additive provenance for strict readers; legacy values and wire data are unchanged.
	Observation *WindowObservation `json:"-"`
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

// ProviderUsage is the full utilization response for one provider credential.
type ProviderUsage struct {
	// AccountRef is the existing account sent with this successful request, never a profile or cache key.
	AccountRef string `json:"-"`
	// OrganizationRef is the actual provider organization returned for this token.
	OrganizationRef string              `json:"-"`
	Provider        string              `json:"provider"`       // "anthropic", executionProviderOpenAI
	Plan            string              `json:"plan,omitempty"` // e.g. "max", "pro", "plus", "free"
	Windows         []UtilizationWindow `json:"windows"`
	FetchedAt       time.Time           `json:"fetched_at"`
}

// ProviderUsageClient fetches live utilization from a provider API.
type ProviderUsageClient interface {
	FetchUsage(ctx context.Context) (*ProviderUsage, error)
}
