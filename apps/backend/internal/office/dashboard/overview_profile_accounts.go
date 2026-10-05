package dashboard

import (
	"context"

	"go.uber.org/zap"
)

// ProfileAccountLister names the provider account of every concrete agent
// profile. It is implemented by the usage adapter, which already groups profiles
// by the credential each one authenticates with.
type ProfileAccountLister interface {
	ListProfileAccountIDs(ctx context.Context) (map[string]string, error)
}

// SetProfileAccountLister wires the account source. Without it the models
// section carries no account, and the client keeps grouping models by kind.
func (s *DashboardService) SetProfileAccountLister(l ProfileAccountLister) { s.accountLister = l }

// accountIDsByProfile reads the profile-to-account index once per snapshot. An
// unwired or failing source leaves the index nil, which leaves every model
// without an account: the section degrades to its own grouping rather than
// failing the aggregate or guessing an account for a profile.
func (s *DashboardService) accountIDsByProfile(ctx context.Context, snap *overviewSnapshot) {
	if s.accountLister == nil {
		return
	}
	ids, err := s.accountLister.ListProfileAccountIDs(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Debug("overview profile accounts unavailable", zap.Error(err))
		}
		return
	}
	snap.accountIDs = ids
}
