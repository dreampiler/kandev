package controller

import (
	"context"
	"errors"
	"strings"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

var ErrInvalidResourceCircuitKey = errors.New("invalid resource circuit key")

// ResourceCircuitManager extends the existing owner-release runtime seam.
type ResourceCircuitManager interface {
	ReleaseResourceCircuit(context.Context, string) (bool, error)
	ListResourceCircuits(context.Context) ([]dynamic.CircuitSnapshot, error)
}

func (c *Controller) ReleaseResourceCircuit(ctx context.Context, key string) (bool, error) {
	scope, value, found := strings.Cut(key, ":")
	if !found || value == "" || len(key) > 2048 {
		return false, ErrInvalidResourceCircuitKey
	}
	switch dynamic.ResourceScope(scope) {
	case dynamic.ScopeProvider, dynamic.ScopeCredential, dynamic.ScopeModel, dynamic.ScopeProfile, dynamic.ScopeAccountBlock:
	default:
		return false, ErrInvalidResourceCircuitKey
	}
	manager, ok := c.accountBlockReleaser.(ResourceCircuitManager)
	if !ok {
		return false, ErrAccountBlockReleaseUnavailable
	}
	return manager.ReleaseResourceCircuit(ctx, key)
}

func (c *Controller) ListResourceCircuits(ctx context.Context) ([]dynamic.CircuitSnapshot, error) {
	manager, ok := c.accountBlockReleaser.(ResourceCircuitManager)
	if !ok {
		return nil, ErrAccountBlockReleaseUnavailable
	}
	return manager.ListResourceCircuits(ctx)
}
