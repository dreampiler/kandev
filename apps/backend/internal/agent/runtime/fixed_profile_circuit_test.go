package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	agentsettingsmodels "github.com/kandev/kandev/internal/agent/settings/models"
)

// A fixed profile shares the circuit key a dynamic selection would compute for
// it: the credential binding when a resolver is configured, otherwise the
// profile's own key.
func TestFixedProfileCircuitKeyScope(t *testing.T) {
	ctx := context.Background()
	profiles := &dynamicResolverTestProfiles{
		concrete: &agentsettingsmodels.AgentProfile{ID: "concrete-profile", AgentID: "concrete", Enabled: true},
	}

	plain := NewProfileExecutionResolver(profiles, dynamic.NewEngine(), true)
	candidate, ok := plain.resourceCircuitCandidate(ctx, "concrete-profile")
	if !ok || candidate.BindingKey != dynamic.ResourceKey(dynamic.ScopeProfile, "concrete-profile") {
		t.Fatalf("profile-scope key = %#v, %v", candidate, ok)
	}

	bound := NewProfileExecutionResolver(profiles, dynamic.NewEngine(), true)
	bound.SetCredentialBindingResolver(dynamic.NewCredentialBindingResolver([]byte("test-installation-key")))
	boundCandidate, ok := bound.resourceCircuitCandidate(ctx, "concrete-profile")
	if !ok || boundCandidate.BindingKey == candidate.BindingKey ||
		!strings.HasPrefix(boundCandidate.BindingKey, string(dynamic.ScopeCredential)+":") {
		t.Fatalf("credential-scope key = %#v, %v", boundCandidate, ok)
	}

	if _, ok := plain.resourceCircuitCandidate(ctx, "missing-profile"); ok {
		t.Fatal("an unreadable profile reported a candidate")
	}
}

func TestRecordAndReadFixedProfileSuspension(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	engine := dynamic.NewEngine(dynamic.WithClock(func() time.Time { return now }))
	profiles := &dynamicResolverTestProfiles{
		concrete: &agentsettingsmodels.AgentProfile{ID: "concrete-profile", AgentID: "concrete", Enabled: true},
	}
	resolver := NewProfileExecutionResolver(profiles, engine, true)

	healthy, known := resolver.FixedProfileSuspension(ctx, "concrete-profile", now)
	if !known || healthy.Blocked() {
		t.Fatalf("a healthy profile = %#v, %v; want known and unblocked", healthy, known)
	}

	resolver.RecordFixedProfileFailure(ctx, "concrete-profile", &routingerr.Error{
		Code: routingerr.CodeQuotaLimited, Class: routingerr.ClassForCode(routingerr.CodeQuotaLimited),
		Confidence: routingerr.ConfHigh,
	})
	suspension, known := resolver.FixedProfileSuspension(ctx, "concrete-profile", now)
	if !known || !suspension.Blocked() || suspension.Code != routingerr.CodeQuotaLimited || suspension.Until.IsZero() {
		t.Fatalf("recorded suspension = %#v, %v", suspension, known)
	}
	expired, known := resolver.FixedProfileSuspension(ctx, "concrete-profile", now.Add(2*time.Hour))
	if !known || expired.Blocked() {
		t.Fatalf("an expired suspension = %#v, %v; want known and unblocked", expired, known)
	}

	resolver.RecordFixedProfileFailure(ctx, "concrete-profile", &routingerr.Error{
		Code: routingerr.CodeAccountBlocked, Class: routingerr.ClassForCode(routingerr.CodeAccountBlocked),
		Confidence: routingerr.ConfHigh,
	})
	held, known := resolver.FixedProfileSuspension(ctx, "concrete-profile", now.Add(365*24*time.Hour))
	if !known || !held.Blocked() || held.Scope != dynamic.ScopeAccountBlock || !held.Until.IsZero() {
		t.Fatalf("held suspension = %#v, %v; want an owner-release hold", held, known)
	}
	if released, err := resolver.ReleaseAccountBlock(ctx, "concrete-profile"); err != nil || !released {
		t.Fatalf("release = %v, %v", released, err)
	}

	if _, known := resolver.FixedProfileSuspension(ctx, "missing-profile", now); known {
		t.Fatal("an unreadable profile reported a known verdict")
	}
}
