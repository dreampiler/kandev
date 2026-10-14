package orchestrator

import (
	"context"
	"errors"
	"testing"

	agentruntime "github.com/kandev/kandev/internal/agent/runtime"
	"github.com/kandev/kandev/internal/task/models"
)

type providerAdmissionClaimRepo struct {
	repoStore
	claim   models.PromptableTaskSessionClaim
	claimed bool
	reads   int
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
			svc := &Service{repo: repo, profileExecutionResolver: &agentruntime.ProfileExecutionResolver{}}
			claim, err := svc.claimPromptableSessionForIdentity(context.Background(), "task", "session", nil)
			if err != nil || claim.Status != status || !repo.claimed {
				t.Fatalf("atomic lifecycle claim = %+v, %v", claim, err)
			}
			wantReads := 0
			if status == models.PromptableTaskSessionClaimed {
				wantReads = 1
			}
			if repo.reads != wantReads {
				t.Fatalf("session reads = %d, want %d after the claim", repo.reads, wantReads)
			}
		})
	}
}
