package controller

import (
	"context"

	"github.com/kandev/kandev/internal/agent/settings/dto"
)

// ProfileUsageProvider reads every concrete profile's provider usage. It is
// read-only and served from the shared usage cache.
type ProfileUsageProvider interface {
	ListProfileUsage(ctx context.Context) ([]dto.AgentProfileUsageDTO, error)
}

// SetProfileUsageProvider injects the usage reader.
func (c *Controller) SetProfileUsageProvider(provider ProfileUsageProvider) {
	c.profileUsage = provider
}

// ListProfileUsage returns every concrete profile's usage. Without a provider
// the list is empty rather than an error, so the settings page still renders.
func (c *Controller) ListProfileUsage(ctx context.Context) (*dto.ListAgentProfileUsageResponse, error) {
	if c.profileUsage == nil {
		return &dto.ListAgentProfileUsageResponse{Profiles: []dto.AgentProfileUsageDTO{}}, nil
	}
	profiles, err := c.profileUsage.ListProfileUsage(ctx)
	if err != nil {
		return nil, err
	}
	if profiles == nil {
		profiles = []dto.AgentProfileUsageDTO{}
	}
	return &dto.ListAgentProfileUsageResponse{Profiles: profiles}, nil
}
