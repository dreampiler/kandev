package orchestrator

import "testing"

// TestCeilingLaunchOrigin pins the admission class of a server-classified
// automatic launch: it overrides a manual base so an agent/MCP-initiated launch
// cannot ride the manual override past the session ceiling, while a launch that
// is already automatic keeps its class.
func TestCeilingLaunchOrigin(t *testing.T) {
	cases := []struct {
		name      string
		base      launchOrigin
		automatic bool
		want      launchOrigin
	}{
		{"automatic overrides manual base", launchOriginManual, true, launchOriginAutomatic},
		{"automatic keeps automatic base", launchOriginAutomatic, true, launchOriginAutomatic},
		{"non-automatic keeps manual base", launchOriginManual, false, launchOriginManual},
		{"non-automatic keeps automatic base", launchOriginAutomatic, false, launchOriginAutomatic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ceilingLaunchOrigin(tc.base, tc.automatic); got != tc.want {
				t.Fatalf("ceilingLaunchOrigin(%q, %v) = %q, want %q", tc.base, tc.automatic, got, tc.want)
			}
		})
	}
}

// TestSeam1StartPayloadCarriesEffectiveOrigin pins that a deferred record's
// payload restores the same admission class on replay: an automatic spawn is
// recorded as automatic even though it is not an autoStart.
func TestSeam1StartPayloadCarriesEffectiveOrigin(t *testing.T) {
	automatic := seam1StartPayload("profile", "executor", "exec-profile", "", "prompt", "step", false, false, nil,
		startTaskOptions{Automatic: true})
	if got := stringField(automatic, "origin"); got != string(launchOriginAutomatic) {
		t.Fatalf("automatic spawn payload origin = %q, want %q", got, launchOriginAutomatic)
	}

	manual := seam1StartPayload("profile", "executor", "exec-profile", "", "prompt", "step", false, false, nil,
		startTaskOptions{})
	if got := stringField(manual, "origin"); got != string(launchOriginManual) {
		t.Fatalf("plain launch payload origin = %q, want %q", got, launchOriginManual)
	}

	explicit := seam1StartPayload("profile", "executor", "exec-profile", "", "prompt", "step", false, false, nil,
		startTaskOptions{Origin: launchOriginAutomatic})
	if got := stringField(explicit, "origin"); got != string(launchOriginAutomatic) {
		t.Fatalf("explicit automatic payload origin = %q, want %q", got, launchOriginAutomatic)
	}
}
