package orchestrator

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/task/models"
)

type providerAdmissionClaimRepo struct {
	repoStore
	claim   models.PromptableTaskSessionClaim
	claimed bool
	reads   int
}

type changedRouteClaimRepo struct{ repoStore }

func (r changedRouteClaimRepo) ClaimPromptableTaskSessionIfActive(ctx context.Context, id string) (models.PromptableTaskSessionClaim, error) {
	claim, err := r.repoStore.ClaimPromptableTaskSessionIfActive(ctx, id)
	if err != nil || claim.Status != models.PromptableTaskSessionClaimed {
		return claim, err
	}
	session, err := r.GetTaskSession(ctx, id)
	if err != nil {
		return claim, err
	}
	session.RouteGeneration++
	return claim, r.UpdateTaskSession(ctx, session)
}

type availableLifecycleAdmission struct{}

func (availableLifecycleAdmission) UsageSnapshot(context.Context, dynamicruntime.Profile) (map[string]dynamicruntime.PaceScore, error) {
	return nil, nil
}
func (availableLifecycleAdmission) CheckAdmission(context.Context, dynamicruntime.Candidate, string, time.Time) (dynamicruntime.PaceScore, error) {
	return dynamicruntime.PaceScore{}, nil
}

func TestProviderTierLifecycleClaimRejectsChangedRoute(t *testing.T) {
	svc, repo := newTurnLifecycleTestService(t)
	ctx := context.Background()
	session, err := repo.GetTaskSession(ctx, "session1")
	if err != nil {
		t.Fatal(err)
	}
	session.State = models.TaskSessionStateWaitingForInput
	session.AgentProfileID, session.ExecutionProfileID, session.RouteGeneration = "dynamic-profile", "shared", 1
	if err := repo.UpdateTaskSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	svc.repo = changedRouteClaimRepo{repoStore: repo}
	svc.profileExecutionResolver = newWorkflowDynamicProfileResolverWithCandidates(t, "dynamic-profile", []workflowDynamicCandidate{{executionProfileID: "shared", enabled: true}}, dynamicruntime.WithUsageSnapshotProvider(availableLifecycleAdmission{}), dynamicruntime.WithStateLoader(lifecyclePairRouteLoader{}))
	_, err = svc.claimPromptableSessionForIdentity(ctx, "task1", "session1", nil)
	if !errors.Is(err, dynamicruntime.ErrStaleGeneration) {
		t.Fatalf("changed route was admitted: %v", err)
	}
	stored, err := repo.GetTaskSession(ctx, "session1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != models.TaskSessionStateWaitingForInput || stored.RouteGeneration != 2 {
		t.Fatalf("stale claim restore overwrote replacement route: %+v", stored)
	}
}

type lifecyclePairAdmissionRepo struct {
	repoStore
	claims      atomic.Int32
	secondClaim chan struct{}
	secondRead  chan struct{}
}

func (r *lifecyclePairAdmissionRepo) ClaimPromptableTaskSessionIfActive(ctx context.Context, id string) (models.PromptableTaskSessionClaim, error) {
	claim, err := r.repoStore.ClaimPromptableTaskSessionIfActive(ctx, id)
	if err == nil && claim.Status == models.PromptableTaskSessionClaimed && r.claims.Add(1) == 2 {
		close(r.secondClaim)
	}
	return claim, err
}

func (r *lifecyclePairAdmissionRepo) UpdateTaskSession(ctx context.Context, session *models.TaskSession) error {
	if session.ID == "session1" && session.State == models.TaskSessionStateRunning && session.RouteState == dynamicRouteStatusWaiting {
		<-r.secondRead
	}
	return r.repoStore.UpdateTaskSession(ctx, session)
}

type lifecyclePairAdmissionUsage struct {
	repo     *lifecyclePairAdmissionRepo
	entered  chan struct{}
	release  chan struct{}
	readOnce sync.Once
}

func (*lifecyclePairAdmissionUsage) UsageSnapshot(context.Context, dynamicruntime.Profile) (map[string]dynamicruntime.PaceScore, error) {
	return nil, nil
}
func (u *lifecyclePairAdmissionUsage) CheckAdmission(ctx context.Context, _ dynamicruntime.Candidate, exclude string, now time.Time) (dynamicruntime.PaceScore, error) {
	if exclude == "session1" {
		close(u.entered)
		<-u.release
	}
	active := 0
	for _, id := range []string{"session1", "session2"} {
		session, err := u.repo.GetTaskSession(ctx, id)
		if err != nil {
			return dynamicruntime.PaceScore{}, err
		}
		if id != exclude && session.State == models.TaskSessionStateRunning {
			active++
		}
	}
	if exclude == "session2" {
		u.readOnce.Do(func() { close(u.repo.secondRead) })
	}
	if active >= 1 {
		return dynamicruntime.PaceScore{AdmissionReason: "limit_concurrency", AdmissionRetryAt: now.Add(time.Minute)}, nil
	}
	return dynamicruntime.PaceScore{}, nil
}

type lifecyclePairRouteLoader struct{}

func (lifecyclePairRouteLoader) LoadRouteState(_ context.Context, id string) (*dynamicruntime.RouteState, error) {
	return &dynamicruntime.RouteState{SessionID: id, LogicalProfileID: "dynamic-profile", ExecutionProfileID: "shared", Generation: 1, ProfileVersion: 1, Status: "active"}, nil
}

func TestProviderTierConcurrentLifecycleClaimsMakeProgress(t *testing.T) {
	svc, repo := newTurnLifecycleTestService(t)
	ctx := context.Background()
	original, err := repo.GetTaskSession(ctx, "session1")
	if err != nil {
		t.Fatal(err)
	}
	secondSession := *original
	secondSession.ID, secondSession.QueueIncarnationID = "session2", ""
	secondSession.IsPrimary = false
	if err := repo.CreateTaskSession(ctx, &secondSession); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"session1", "session2"} {
		session, err := repo.GetTaskSession(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		session.State = models.TaskSessionStateWaitingForInput
		session.AgentProfileID, session.ExecutionProfileID, session.RouteGeneration = "dynamic-profile", "shared", 1
		if err := repo.UpdateTaskSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	wrapped := &lifecyclePairAdmissionRepo{repoStore: repo, secondClaim: make(chan struct{}), secondRead: make(chan struct{})}
	u := &lifecyclePairAdmissionUsage{repo: wrapped, entered: make(chan struct{}), release: make(chan struct{})}
	svc.repo = wrapped
	svc.profileExecutionResolver = newWorkflowDynamicProfileResolverWithCandidates(t, "dynamic-profile", []workflowDynamicCandidate{{executionProfileID: "shared", enabled: true}}, dynamicruntime.WithUsageSnapshotProvider(u), dynamicruntime.WithStateLoader(lifecyclePairRouteLoader{}))
	results := make(chan error, 2)
	go func() { _, err := svc.claimPromptableSessionForIdentity(ctx, "task1", "session1", nil); results <- err }()
	<-u.entered
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := svc.claimPromptableSessionForIdentity(ctx, "task1", "session2", nil)
		results <- err
	}()
	<-started
	select {
	case <-wrapped.secondClaim:
	case <-time.After(time.Second):
	}
	close(u.release)
	first, second := <-results, <-results
	admitted := 0
	for _, err := range []error{first, second} {
		if err == nil {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatalf("concurrent lifecycle claims admitted %d, want one: %v, %v", admitted, first, second)
	}
}

func (r *providerAdmissionClaimRepo) ClaimPromptableTaskSessionIfActive(context.Context, string) (models.PromptableTaskSessionClaim, error) {
	r.claimed = true
	return r.claim, nil
}

func (r *providerAdmissionClaimRepo) GetTaskSession(context.Context, string) (*models.TaskSession, error) {
	r.reads++
	if !r.claimed {
		return nil, errors.New("session snapshot read before atomic claim")
	}
	return &models.TaskSession{ID: "session", TaskID: "task"}, nil
}

func TestProviderTierLifecycleAdmissionClaimsBeforeReadingSession(t *testing.T) {
	for _, status := range []models.PromptableTaskSessionClaimStatus{models.PromptableTaskSessionClaimed, models.PromptableTaskSessionBusy, models.PromptableTaskSessionInactive} {
		t.Run(string(status), func(t *testing.T) {
			repo := &providerAdmissionClaimRepo{claim: models.PromptableTaskSessionClaim{Status: status}}
			svc := &Service{repo: repo}
			claim, err := svc.claimPromptableSessionForIdentity(context.Background(), "task", "session", nil)
			if err != nil || claim.Status != status || !repo.claimed {
				t.Fatalf("atomic lifecycle claim = %+v, %v", claim, err)
			}
			wantReads := 0
			if repo.reads != wantReads {
				t.Fatalf("session reads = %d, want %d after the claim", repo.reads, wantReads)
			}
		})
	}
}
