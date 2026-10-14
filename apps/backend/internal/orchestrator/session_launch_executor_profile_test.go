package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/task/repository/repoerrors"
	v1 "github.com/kandev/kandev/pkg/api/v1"
	"github.com/stretchr/testify/require"
)

type launchProfileStore struct {
	sessionExecutorStore
	profile *models.ExecutorProfile
	err     error
	lookups int
}

func (r *launchProfileStore) GetExecutorProfile(context.Context, string) (*models.ExecutorProfile, error) {
	r.lookups++
	return r.profile, r.err
}

func TestLaunchSessionExecutorProfileReference(t *testing.T) {
	for _, id := range []string{"missing-profile", "valid-profile", ""} {
		t.Run(id, func(t *testing.T) {
			repo := &launchProfileStore{}
			if id == "missing-profile" {
				repo.err = fmt.Errorf("%w: %s", repoerrors.ErrExecutorProfileNotFound, id)
			} else {
				repo.profile = &models.ExecutorProfile{ID: id}
			}
			claimErr := errors.New("attachment boundary reached")
			claimer := &rejectingLaunchAttachmentClaimer{wantErr: claimErr}
			svc := &Service{repo: repo}
			svc.SetLaunchAttachmentClaimer(claimer)
			_, err := svc.LaunchSession(context.Background(), &LaunchSessionRequest{
				TaskID: "task", Intent: IntentStart, ExecutorProfileID: id,
				Attachments: []v1.MessageAttachment{{AttachmentID: "attachment"}},
			})
			if id == "missing-profile" {
				require.ErrorIs(t, err, repoerrors.ErrExecutorProfileNotFound)
				require.Empty(t, claimer.taskID)
			} else {
				require.ErrorIs(t, err, claimErr)
				require.Equal(t, "task", claimer.taskID)
			}
			if id == "" {
				require.Zero(t, repo.lookups)
			} else {
				require.Equal(t, 1, repo.lookups)
			}
		})
	}
}
