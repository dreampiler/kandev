package backendapp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/settings/controller"
	settingsstore "github.com/kandev/kandev/internal/agent/settings/store"
)

// providerLimitStore persists operator-entered provider limit settings.
type providerLimitStore interface {
	ListProviderLimits(ctx context.Context) ([]dynamicruntime.ProviderLimit, error)
	SaveProviderLimit(ctx context.Context, limit dynamicruntime.ProviderLimit) error
}

// providerLimitService implements controller.ProviderLimitService. Providers
// are the prefixes of the models agent profiles launch, plus any provider that
// already has saved settings.
type providerLimitService struct {
	store    providerLimitStore
	profiles settingsstore.Repository
	calendar *dynamicLimitCalendar
	now      func() time.Time
}

var providerKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func newProviderLimitService(
	store providerLimitStore,
	profiles settingsstore.Repository,
	calendar *dynamicLimitCalendar,
) *providerLimitService {
	return &providerLimitService{store: store, profiles: profiles, calendar: calendar, now: time.Now}
}

// providerProfile is one local profile that launches a model of a provider.
type providerProfile struct {
	profileID string
	modelID   string
}

// ListProviderLimits implements controller.ProviderLimitService.
func (s *providerLimitService) ListProviderLimits(ctx context.Context) ([]controller.ProviderLimitDTO, error) {
	saved, err := s.store.ListProviderLimits(ctx)
	if err != nil {
		return nil, err
	}
	byProvider := make(map[string]dynamicruntime.ProviderLimit, len(saved))
	for _, limit := range saved {
		byProvider[dynamicruntime.NormalizeProvider(limit.Provider)] = limit
	}
	profiles, err := s.providerProfiles(ctx)
	if err != nil {
		return nil, err
	}
	providers := make(map[string]struct{}, len(profiles)+len(byProvider))
	for provider := range profiles {
		providers[provider] = struct{}{}
	}
	for provider := range byProvider {
		providers[provider] = struct{}{}
	}
	names := make([]string, 0, len(providers))
	for provider := range providers {
		names = append(names, provider)
	}
	sort.Strings(names)
	now := s.now()
	result := make([]controller.ProviderLimitDTO, 0, len(names))
	for _, provider := range names {
		limit, hasLimit := byProvider[provider]
		if !hasLimit {
			limit = dynamicruntime.ProviderLimit{Provider: provider}
		}
		result = append(result, s.describe(ctx, limit, hasLimit, profiles[provider], now))
	}
	return result, nil
}

// UpdateProviderLimit implements controller.ProviderLimitService.
func (s *providerLimitService) UpdateProviderLimit(
	ctx context.Context,
	provider string,
	request controller.UpdateProviderLimitRequest,
) (*controller.ProviderLimitDTO, error) {
	provider = dynamicruntime.NormalizeProvider(provider)
	if !providerKeyPattern.MatchString(provider) {
		return nil, fmt.Errorf("%w: provider %q", controller.ErrInvalidProviderLimit, provider)
	}
	timezone := strings.TrimSpace(request.MonthlyResetTimezone)
	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			return nil, fmt.Errorf("%w: unknown timezone %q", controller.ErrInvalidProviderLimit, timezone)
		}
	}
	limit := dynamicruntime.ProviderLimit{
		Provider:             provider,
		MonthlyResetAt:       nonZeroTime(request.MonthlyResetAt),
		MonthlyResetTimezone: timezone,
		BlockUntil:           nonZeroTime(request.BlockUntil),
	}
	if limit.MonthlyResetAt == nil {
		limit.MonthlyResetTimezone = ""
	}
	if err := s.store.SaveProviderLimit(ctx, limit); err != nil {
		return nil, err
	}
	profiles, err := s.providerProfiles(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	limit.UpdatedAt = now
	described := s.describe(ctx, limit, true, profiles[provider], now)
	return &described, nil
}

func (s *providerLimitService) describe(
	ctx context.Context,
	limit dynamicruntime.ProviderLimit,
	saved bool,
	profiles []providerProfile,
	now time.Time,
) controller.ProviderLimitDTO {
	provider := dynamicruntime.NormalizeProvider(limit.Provider)
	result := controller.ProviderLimitDTO{
		Provider:             provider,
		ProfileCount:         len(profiles),
		MonthlyResetAt:       utcTime(limit.MonthlyResetAt),
		MonthlyResetTimezone: limit.MonthlyResetTimezone,
		BlockUntil:           utcTime(limit.BlockUntil),
	}
	if saved && !limit.UpdatedAt.IsZero() {
		result.UpdatedAt = utcTime(&limit.UpdatedAt)
	}
	for _, profile := range profiles {
		if dynamicruntime.ModelScoped(profile.modelID) && !dynamicruntime.IsFreeModel(profile.modelID) {
			result.ModelScoped = true
			break
		}
	}
	if sample, ok := paidProfile(profiles); ok && s.calendar != nil {
		candidate := dynamicruntime.Candidate{ID: sample.profileID, Enabled: true, ModelID: sample.modelID}
		windows := s.calendar.observedWindows(ctx, candidate)
		if reset, ok := observedMonthlyReset(windows, now); ok {
			result.NextMonthlyReset, result.NextMonthlyResetSource = utcTime(&reset), "usage"
		}
		if until, ok := s.calendar.ExhaustedUntil(ctx, candidate, now); ok {
			result.ObservedExhaustedUntil = utcTime(&until)
		}
	}
	if result.NextMonthlyReset == nil {
		if reset, ok := limit.NextMonthlyReset(now); ok {
			result.NextMonthlyReset, result.NextMonthlyResetSource = utcTime(&reset), "manual"
		}
	}
	return result
}

// providerProfiles groups the enabled local profiles by the provider prefix of
// their model.
func (s *providerLimitService) providerProfiles(ctx context.Context) (map[string][]providerProfile, error) {
	grouped := make(map[string][]providerProfile)
	if s.profiles == nil {
		return grouped, nil
	}
	agents, err := s.profiles.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if agent == nil {
			continue
		}
		profiles, err := s.profiles.ListAgentProfiles(ctx, agent.ID)
		if err != nil {
			return nil, err
		}
		for _, profile := range profiles {
			if profile == nil || profile.DeletedAt != nil || !profile.Enabled {
				continue
			}
			model := strings.TrimSpace(profile.Model)
			provider := dynamicruntime.ProviderOf(model)
			if provider == "" {
				continue
			}
			grouped[provider] = append(grouped[provider], providerProfile{profileID: profile.ID, modelID: model})
		}
	}
	return grouped, nil
}

// paidProfile picks a paid-model profile whose usage reading describes the
// provider account.
func paidProfile(profiles []providerProfile) (providerProfile, bool) {
	for _, profile := range profiles {
		if !dynamicruntime.IsFreeModel(profile.modelID) {
			return profile, true
		}
	}
	return providerProfile{}, false
}

func nonZeroTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func utcTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
