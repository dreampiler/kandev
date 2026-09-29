package toolretention

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

// @covers AC-SYSTEM-PAGE-TOOL-PAYLOAD-RETENTION-002.1, 002.2, 002.6
func TestPreparationGatesInitialCleanup(t *testing.T) {
	for _, choice := range []string{"skip", "backup", "failure"} {
		t.Run(choice, func(t *testing.T) {
			s := testService(t)
			seedAnalysis(t, s)
			ctx := context.Background()
			called := false
			s.opts.CreateBackup = func(context.Context) (string, error) {
				called = true
				if choice == "failure" {
					return "", errors.New("disk full secret path")
				}
				return "verified-receipt", nil
			}
			requested := choice
			if choice == "failure" {
				requested = "backup"
			}
			_, err := s.Save(ctx, Update{Enabled: true, Age: Age{3, "months"}, BackupChoice: requested})
			require.NoError(t, err)
			err = s.stepPreparation(ctx)
			status, getErr := s.Get(ctx)
			require.NoError(t, getErr)
			if choice == "failure" {
				require.Error(t, err)
				require.False(t, status.Policy.Enabled)
				require.Equal(t, "failed", status.Preparation.State)
				require.NotContains(t, status.Preparation.Error, "secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, choice == "backup", called)
			require.True(t, status.Policy.Enabled)
			require.Equal(t, "ready", status.Preparation.State)
			require.Equal(t, "cleanup", status.Operation.Kind)
			require.Equal(t, "running", status.Operation.State)
		})
	}
}

func TestLateBackupCompletionCannotOverrideDisable(t *testing.T) {
	s := testService(t)
	seedAnalysis(t, s)
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	s.opts.CreateBackup = func(context.Context) (string, error) { close(started); <-release; return "receipt", nil }
	status, err := s.Save(ctx, Update{Enabled: true, Age: Age{3, "months"}, BackupChoice: "backup"})
	require.NoError(t, err)
	go func() { done <- s.stepPreparation(ctx) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("preparation returned without backup: %v", err)
	}
	_, err = s.Save(ctx, Update{Age: Age{3, "months"}, Revision: status.Policy.Revision})
	require.NoError(t, err)
	close(release)
	require.NoError(t, <-done)
	status, err = s.Get(ctx)
	require.NoError(t, err)
	require.False(t, status.Policy.Enabled)
	require.Equal(t, "none", status.Preparation.State)
}

// TestPreparationFailureRecordsCauseForDiagnosis keeps the raw backup error out
// of the status payload while logging it and persisting it in the settings row,
// so a future "backup_failed" can be diagnosed from the database alone.
func TestPreparationFailureRecordsCauseForDiagnosis(t *testing.T) {
	s := testService(t)
	seedAnalysis(t, s)
	ctx := context.Background()
	backupErr := errors.New("vacuum into /data/backups/snapshot.db: disk full secret path")
	var loggedMessage string
	var loggedErr error
	s.opts.Log = func(_ context.Context, message string, err error) {
		loggedMessage, loggedErr = message, err
	}
	s.opts.CreateBackup = func(context.Context) (string, error) { return "", backupErr }
	_, err := s.Save(ctx, Update{Enabled: true, Age: Age{3, "months"}, BackupChoice: "backup"})
	require.NoError(t, err)
	require.Error(t, s.stepPreparation(ctx))
	status, err := s.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "failed", status.Preparation.State)
	require.Equal(t, "backup_failed", status.Preparation.Error)
	require.NotContains(t, status.Preparation.Error, "secret")
	require.ErrorIs(t, loggedErr, backupErr)
	require.NotEmpty(t, loggedMessage)
	stored, err := readRecord(ctx, s.pool.Reader())
	require.NoError(t, err)
	require.Contains(t, stored.PreparationDetail, "disk full secret path")
	require.LessOrEqual(t, len(stored.PreparationDetail), preparationDetailLimit)
}

func TestLimitPreparationDetailTruncates(t *testing.T) {
	long := strings.Repeat("x", preparationDetailLimit+50)
	got := limitPreparationDetail(long)
	require.Len(t, got, preparationDetailLimit)
	require.Equal(t, long[:preparationDetailLimit], got)
	require.Equal(t, "short", limitPreparationDetail("short"))
}
