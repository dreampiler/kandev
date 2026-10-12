package backendapp

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/auth/authn"
	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/office/crossworkspace"
	"github.com/kandev/kandev/internal/office/models"
)

// staticIdentities resolves a fixed set of active accounts, standing in for
// the auth service's IdentityForUser.
type staticIdentities map[string]bool

func (s staticIdentities) IdentityForUser(_ context.Context, userID string) (authn.Identity, bool) {
	if !s[userID] {
		return authn.Identity{}, false
	}
	return authn.Identity{UserID: userID, Role: authn.RoleMember}, true
}

// TestOfficeCrossWorkspaceReachUsesOwnerReadAccess runs the reach decision
// through the real task-service authorization and the real MCP owner
// resolver, in both authentication modes.
func TestOfficeCrossWorkspaceReachUsesOwnerReadAccess(t *testing.T) {
	taskSvc, taskRepo, officeRepo := newRunSubscriptionCheckHarness(t)
	office := seedOwnedWorkspace(t, taskSvc, officeScopeUserA)
	ownSecond := seedOwnedWorkspace(t, taskSvc, officeScopeUserA)
	foreign := seedOwnedWorkspace(t, taskSvc, officeScopeUserB)
	ctx := context.Background()
	if err := officeRepo.SetCrossWorkspaceAccess(ctx, models.CrossWorkspaceAccess{WorkspaceID: office, Enabled: true}); err != nil {
		t.Fatalf("enable: %v", err)
	}

	identities := staticIdentities{officeScopeUserA: true, officeScopeUserB: true}
	authOn := true
	resolver := mcpscope.NewResolver(taskRepo, identities, func() bool { return authOn }, testLogger(t))
	svc := crossworkspace.NewService(officeRepo, taskSvc, resolver, nil, testLogger(t))

	// A request that arrives carrying somebody else's identity (a browser
	// cookie on the same request) must not decide the Office owner's reach.
	asB := authn.WithIdentity(ctx, authn.Identity{UserID: officeScopeUserB, Role: authn.RoleMember})
	for _, reqCtx := range []context.Context{ctx, asB} {
		if err := svc.Reachable(reqCtx, office, ownSecond); err != nil {
			t.Fatalf("auth on: owner's second workspace: %v", err)
		}
		if err := svc.Reachable(reqCtx, office, foreign); !errors.Is(err, crossworkspace.ErrNotReachable) {
			t.Fatalf("auth on: another owner's workspace: err = %v, want ErrNotReachable", err)
		}
	}
	dir, err := svc.ListReachable(asB, office)
	if err != nil {
		t.Fatalf("ListReachable: %v", err)
	}
	for _, ws := range dir.Workspaces {
		if ws.ID == foreign {
			t.Fatalf("auth on: directory lists another owner's workspace: %+v", dir)
		}
	}

	// A disabled owner account fails closed.
	delete(identities, officeScopeUserA)
	if err := svc.Reachable(ctx, office, ownSecond); !errors.Is(err, crossworkspace.ErrNotReachable) {
		t.Fatalf("disabled owner: err = %v, want ErrNotReachable", err)
	}
	identities[officeScopeUserA] = true

	// With authentication off there is one user, so every workspace is in
	// reach once the setting is on.
	authOn = false
	if err := svc.Reachable(ctx, office, foreign); err != nil {
		t.Fatalf("auth off: %v", err)
	}
}
