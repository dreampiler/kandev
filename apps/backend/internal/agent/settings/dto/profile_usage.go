package dto

import "time"

// AgentProfileUsageDTO is one concrete profile's provider usage, as shown on the
// agents settings list. State is ok, unavailable, unsupported or no_usage_api;
// Reason is a bounded failure code and never a provider response body.
type AgentProfileUsageDTO struct {
	ProfileID  string                       `json:"profile_id"`
	State      string                       `json:"state"`
	Reason     string                       `json:"reason,omitempty"`
	Status     int                          `json:"status,omitempty"`
	Source     string                       `json:"source,omitempty"`
	Provider   string                       `json:"provider,omitempty"`
	Plan       string                       `json:"plan,omitempty"`
	ModelClass string                       `json:"model_class,omitempty"`
	FetchedAt  *time.Time                   `json:"fetched_at,omitempty"`
	Windows    []AgentProfileUsageWindowDTO `json:"windows"`
	// Recorded is the account's usage Kandev itself recorded in the current UTC
	// day. It is reported for accounts whose provider publishes no usage API,
	// and is a lower bound rather than the provider's own count.
	Recorded *AgentProfileRecordedUsageDTO `json:"recorded,omitempty"`
}

// AgentProfileUsageWindowDTO is one provider window that limits the profile's
// model. Scope names the class of models the window limits.
type AgentProfileUsageWindowDTO struct {
	Label           string     `json:"label"`
	UtilizationPct  float64    `json:"utilization_pct"`
	ResetAt         *time.Time `json:"reset_at,omitempty"`
	StartAt         *time.Time `json:"start_at,omitempty"`
	DurationSeconds int64      `json:"duration_seconds,omitempty"`
	Scope           string     `json:"scope,omitempty"`
	LimitReached    bool       `json:"limit_reached,omitempty"`
}

// AgentProfileRecordedUsageDTO is account-wide recorded usage over a window.
type AgentProfileRecordedUsageDTO struct {
	WindowStart  time.Time `json:"window_start"`
	WindowEnd    time.Time `json:"window_end"`
	Turns        int64     `json:"turns"`
	TokensTotal  int64     `json:"tokens_total"`
	ProfileCount int       `json:"profile_count"`
}

// ListAgentProfileUsageResponse lists every concrete profile's usage.
type ListAgentProfileUsageResponse struct {
	Profiles []AgentProfileUsageDTO `json:"profiles"`
}
