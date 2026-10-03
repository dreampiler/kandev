package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
)

var (
	nowForContinuity  = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	errExecutorLookup = errors.New("session executor unavailable")
)

// countingUsageProvider records the candidates the engine ranked for a decision,
// including whether each was marked as a remote execution. That is what makes
// "the first claim was handed remote-marked candidates" an observable fact rather
// than an inference from the resulting score.
type countingUsageProvider struct {
	calls  int
	sawAny bool
	// hostLocalSeen records a candidate the engine treated as running on this
	// host, which for a container session would mean host-account attribution.
	hostLocalSeen []string
}

func (p *countingUsageProvider) UsageSnapshot(
	_ context.Context, profile dynamicruntime.Profile,
) (map[string]dynamicruntime.PaceScore, error) {
	p.calls++
	scores := make(map[string]dynamicruntime.PaceScore, len(profile.Candidates))
	for _, candidate := range profile.Candidates {
		p.sawAny = true
		if !candidate.RemoteExecution {
			p.hostLocalSeen = append(p.hostLocalSeen, candidate.ID)
		}
		scores[candidate.ID] = dynamicruntime.PaceScore{ObservedAt: nowForContinuity}
	}
	return scores, nil
}

// TestResolverMarksRemoteSessionCandidates drives the real selection entry point
// that a new dynamic session uses.
//
// The previous coverage built a Profile by hand and asserted the field it had
// just set, which could not catch the actual defect: the executor never reached
// the load, so every candidate stayed host-local and a container session read the
// backend host's provider account. This test goes through Resolve with a session
// executor seam, which is the path task_operations.resolveExecutionForLaunchSession
// takes.
func TestResolverMarksRemoteSessionCandidates(t *testing.T) {
	candidates := continuityCandidates()
	for name, testCase := range map[string]struct {
		executorID string
		wantRemote bool
	}{
		"host local":       {executorID: "exec-local", wantRemote: false},
		"container":        {executorID: "exec-local-docker", wantRemote: true},
		"ssh":              {executorID: "exec-ssh", wantRemote: true},
		"unqualified host": {executorID: "", wantRemote: false},
	} {
		t.Run(name, func(t *testing.T) {
			resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
				t, "dynamic-remote", continuityCandidates(), true,
			)
			resolver.SetSessionExecutorResolver(
				func(context.Context, string) (string, error) { return testCase.executorID, nil },
			)

			profile, err := resolver.LoadDynamicProfileForSession(
				context.Background(), "dynamic-remote", "session-remote",
			)
			if err != nil {
				t.Fatalf("LoadDynamicProfileForSession: %v", err)
			}
			if len(profile.Candidates) != len(candidates) {
				t.Fatalf("candidates = %d, want %d", len(profile.Candidates), len(candidates))
			}
			for _, candidate := range profile.Candidates {
				if candidate.RemoteExecution != testCase.wantRemote {
					t.Fatalf("candidate %s RemoteExecution = %v, want %v for executor %q",
						candidate.ID, candidate.RemoteExecution, testCase.wantRemote, testCase.executorID)
				}
			}
		})
	}
}

// TestResolverFirstClaimCarriesTheSessionExecutor pins the defect this whole
// round exists for: the FIRST route claim of a session must already see the
// session's executor, because that is the claim whose usage decides the opening
// candidate. A container session must not select its first candidate from the
// backend host's account.
func TestResolverFirstClaimCarriesTheSessionExecutor(t *testing.T) {
	ctx := context.Background()
	provider := &countingUsageProvider{}
	resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
		t, "dynamic-remote", continuityCandidates(), true,
		dynamicruntime.WithUsageSnapshotProvider(provider),
	)
	resolver.SetSessionExecutorResolver(
		func(context.Context, string) (string, error) { return "exec-local-docker", nil },
	)

	if _, err := resolver.Resolve(ctx, "session-docker", "dynamic-remote", 0, ""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !provider.sawAny {
		t.Fatal("the engine never consulted the usage provider, so the claim path is not covered")
	}
	// This is the defect the round exists for. Before the session executor
	// reached the load, every candidate arrived host-marked and the container
	// session's opening candidate was picked from the backend host's account.
	if len(provider.hostLocalSeen) != 0 {
		t.Fatalf("candidates %v were treated as host-local for a container session",
			provider.hostLocalSeen)
	}
}

// TestResolverSessionExecutorFailureKeepsTheHostDefault pins the degradation. A
// lookup that cannot answer must not silently mark every candidate remote, which
// would disable automatic usage for all profiles; the host default is the
// established behaviour for an unqualified execution.
func TestResolverSessionExecutorFailureKeepsTheHostDefault(t *testing.T) {
	resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
		t, "dynamic-remote", continuityCandidates(), true,
	)
	resolver.SetSessionExecutorResolver(func(context.Context, string) (string, error) {
		return "", errExecutorLookup
	})

	profile, err := resolver.LoadDynamicProfileForSession(
		context.Background(), "dynamic-remote", "session-unknown",
	)
	if err != nil {
		t.Fatalf("LoadDynamicProfileForSession: %v", err)
	}
	for _, candidate := range profile.Candidates {
		if candidate.RemoteExecution {
			t.Fatalf("candidate %s marked remote from a failed lookup", candidate.ID)
		}
	}
}

// TestResolverExplicitExecutorBeatsTheSessionLookup pins precedence: a caller
// that already knows the executor is not second-guessed by the seam.
func TestResolverExplicitExecutorBeatsTheSessionLookup(t *testing.T) {
	resolver := newWorkflowDynamicProfileResolverWithCandidatesAndKeepModel(
		t, "dynamic-remote", continuityCandidates(), true,
	)
	resolver.SetSessionExecutorResolver(
		func(context.Context, string) (string, error) { return "exec-ssh", nil },
	)

	profile, err := resolver.LoadDynamicProfileForExecutor(
		context.Background(), "dynamic-remote", "exec-local",
	)
	if err != nil {
		t.Fatalf("LoadDynamicProfileForExecutor: %v", err)
	}
	for _, candidate := range profile.Candidates {
		if candidate.RemoteExecution {
			t.Fatalf("candidate %s marked remote despite an explicit host executor", candidate.ID)
		}
	}
}
