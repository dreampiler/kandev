package utility

import (
	"context"
	"errors"
	"testing"
)

func TestACPFailureLaunchClassification(t *testing.T) {
	for _, phase := range []string{"initialize", "session/new", "prompt"} {
		err := describeACPFailure(t.Context(), phase, errors.New("synthetic disconnect"))
		if errors.Is(err, ErrLaunchUnavailable) != (phase != "prompt") {
			t.Fatalf("phase=%s, error=%v", phase, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := describeACPFailure(ctx, "initialize", errors.New("synthetic disconnect"))
	if errors.Is(err, ErrLaunchUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation must not trigger another reviewer")
	}
}
