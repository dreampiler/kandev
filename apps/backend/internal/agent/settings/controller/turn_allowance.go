package controller

import (
	"context"
	"encoding/json"

	"github.com/kandev/kandev/internal/agent/settings/dto"
)

type ProfileTurnAllowanceProvider interface {
	GetProfileTurnAllowance(context.Context, string) (*dto.ProfileTurnAllowanceDTO, error)
	PatchProfileTurnAllowance(context.Context, string, json.RawMessage) (*dto.ProfileTurnAllowanceDTO, error)
}

func (c *Controller) ProviderTurnAllowancesEnabled() bool {
	_, ok := c.profileUsage.(ProfileTurnAllowanceProvider)
	return ok
}

func (c *Controller) GetProfileTurnAllowance(ctx context.Context, id string) (*dto.ProfileTurnAllowanceDTO, error) {
	provider, ok := c.profileUsage.(ProfileTurnAllowanceProvider)
	if !ok {
		return nil, ErrProviderUsageDisabled
	}
	return provider.GetProfileTurnAllowance(ctx, id)
}

func (c *Controller) PatchProfileTurnAllowance(ctx context.Context, id string, patch json.RawMessage) (*dto.ProfileTurnAllowanceDTO, error) {
	provider, ok := c.profileUsage.(ProfileTurnAllowanceProvider)
	if !ok {
		return nil, ErrProviderUsageDisabled
	}
	return provider.PatchProfileTurnAllowance(ctx, id, patch)
}
