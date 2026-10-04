package sessioncapacity

import (
	"context"
	"errors"
	"sync"

	"github.com/kandev/kandev/internal/common/logger"
	"go.uber.org/zap"
)

// Target is the live admission controller. The control lane's ceiling and its
// profile set travel with the worker ceiling so one call applies both, and a
// partially applied capacity is not a state the controller may rest in. The
// parameters stay primitives so this package keeps no dependency on the
// controller's own types.
type Target interface {
	SetSessionCapacity(workerCeiling, controlCeiling int, controlProfileIDs []string)
}

type Service struct {
	mu                 sync.Mutex
	store              *Store
	target             Target
	environment        Environment
	controlEnvironment Environment
	logger             *logger.Logger
}

func NewService(
	store *Store,
	target Target,
	environment Environment,
	log *logger.Logger,
) *Service {
	return &Service{
		store:       store,
		target:      target,
		environment: environment,
		logger:      log,
	}
}

// NewServiceWithControl adds the control lane's environment override. Without
// it the control lane resolves from the saved setting alone, which is the
// pre-existing two-argument behavior.
func NewServiceWithControl(
	store *Store,
	target Target,
	environment Environment,
	controlEnvironment Environment,
	log *logger.Logger,
) *Service {
	service := NewService(store, target, environment, log)
	service.controlEnvironment = controlEnvironment
	return service
}

func (s *Service) Get(ctx context.Context) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	configured, err := s.loadConfigured(ctx)
	if err != nil {
		return Response{}, err
	}
	resolution, err := ResolveWithControl(configured, s.environment, s.controlEnvironment)
	if err != nil {
		return Response{}, err
	}
	s.warnInvalidEnvironment(resolution)
	return resolution.Response, nil
}

func (s *Service) Update(ctx context.Context, patch SettingsPatch) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.target == nil {
		return Response{}, ErrTargetUnavailable
	}
	if s.environmentIsLocked() {
		return Response{}, ErrEnvironmentLocked
	}
	if s.store == nil {
		return Response{}, errors.New("session capacity settings store unavailable")
	}
	settings, err := s.store.Update(ctx, func(current *Settings) (Settings, error) {
		resolution, resolveErr := ResolveWithControl(current, s.environment, s.controlEnvironment)
		if resolveErr != nil {
			return Settings{}, resolveErr
		}
		s.warnInvalidEnvironment(resolution)
		updated := patch.Apply(resolution.Settings)
		if validateErr := Validate(updated); validateErr != nil {
			return Settings{}, validateErr
		}
		return updated, nil
	})
	if err != nil {
		return Response{}, err
	}
	resolution, err := ResolveWithControl(&settings, s.environment, s.controlEnvironment)
	if err != nil {
		return Response{}, err
	}
	s.target.SetSessionCapacity(
		resolution.Effective.MaxSessions,
		resolution.Effective.ControlMaxSessions,
		resolution.Effective.ControlProfileIDs,
	)
	return resolution.Response, nil
}

func (s *Service) loadConfigured(ctx context.Context) (*Settings, error) {
	if s.store == nil {
		return nil, errors.New("session capacity settings store unavailable")
	}
	return s.store.Load(ctx)
}

// environmentIsLocked reports whether either lane is owned by the environment.
// The settings record is one document, so a lock on either lane locks the whole
// save rather than silently dropping half a patch.
func (s *Service) environmentIsLocked() bool {
	resolution, err := ResolveWithControl(nil, s.environment, s.controlEnvironment)
	if err != nil {
		return false
	}
	return resolution.Effective.Locked || resolution.Effective.ControlLocked
}

func (s *Service) warnInvalidEnvironment(resolution Resolution) {
	if s.logger == nil {
		return
	}
	if resolution.InvalidEnvironment {
		s.logger.Warn(
			"Ignoring invalid session capacity environment value",
			zap.String("environment_variable", EnvironmentVariable),
		)
	}
	if resolution.InvalidControlEnvironment {
		s.logger.Warn(
			"Ignoring invalid control session capacity environment value",
			zap.String("environment_variable", ControlEnvironmentVariable),
		)
	}
}
