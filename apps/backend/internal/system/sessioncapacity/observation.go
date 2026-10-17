package sessioncapacity

import (
	"context"
	"time"
)

// Observation is a read of the actual admission owner, not configured capacity
// combined with a visible-workspace subtotal. Unknown populations have no count.
type Observation struct {
	Policy             string             `json:"policy"`
	Enabled            bool               `json:"enabled"`
	Known              bool               `json:"known"`
	InUse              *int               `json:"in_use"`
	Limit              int                `json:"limit"`
	GradeInUse         map[string]int     `json:"grade_in_use,omitempty"`
	GradeLimits        map[string]int     `json:"grade_limits,omitempty"`
	NeedsConfiguration bool               `json:"needs_configuration,omitempty"`
	ObservedAt         time.Time          `json:"observed_at"`
	Waiting            WaitingObservation `json:"waiting"`
}

type WaitingObservation struct {
	Known  bool           `json:"known"`
	Counts map[string]int `json:"counts,omitempty"`
}

// ReadObservation is optional for older targets and does not change settings.
func (s *Service) ReadObservation(ctx context.Context) *Observation {
	source, ok := s.target.(interface {
		SessionCapacityObservation(context.Context) Observation
	})
	if !ok {
		return nil
	}
	observation := source.SessionCapacityObservation(ctx)
	return &observation
}
