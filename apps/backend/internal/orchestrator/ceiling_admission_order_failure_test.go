package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDeferredRetrySchedule_FailureRecordedOutlivesTheRetryBackoff pins the
// precedence yield: a record whose replay failed for a non-capacity reason
// keeps failing head precedence even after the short retry backoff elapses,
// until it is settled (succeeded or dropped) or replaced.
func TestDeferredRetrySchedule_FailureRecordedOutlivesTheRetryBackoff(t *testing.T) {
	schedule := newDeferredRetrySchedule()
	now := time.Now()
	schedule.now = func() time.Time { return now }

	require.False(t, schedule.failureRecorded("task-a", "start|t0"),
		"an untracked record has no recorded failure")

	schedule.recordFailure("task-a", "start|t0")
	require.True(t, schedule.failureRecorded("task-a", "start|t0"))
	require.False(t, schedule.failureRecorded("task-a", "start|t1"),
		"a replaced record is a different launch and does not inherit the failure")

	now = now.Add(ceilingRetryBaseInterval)
	require.False(t, schedule.failureWaiting("task-a", "start|t0"),
		"the retry wait clears once the base interval elapsed")
	require.True(t, schedule.failureRecorded("task-a", "start|t0"),
		"the precedence yield persists past the retry wait")

	schedule.settle("task-a")
	require.False(t, schedule.failureRecorded("task-a", "start|t0"),
		"settling the record clears the yield")
}

// TestCeilingAdmissionCandidateEligibleYieldsAFailedHead pins the queue
// head-blocking fix at the eligibility seam: a valid deferred record ranks as
// an earlier launch, but a record whose replay failed for a non-capacity reason
// no longer holds that rank, so a later automatic launch can take the free slot.
func TestCeilingAdmissionCandidateEligibleYieldsAFailedHead(t *testing.T) {
	f := newCeilingDispatchFixture(t)
	ctx := context.Background()
	task, err := f.repo.GetTask(ctx, f.task.ID)
	require.NoError(t, err)

	require.True(t, f.svc.ceilingAdmissionCandidateEligible(ctx, task, f.deferral),
		"a valid deferred record with no failure ranks as an earlier launch")

	f.svc.deferredRetrySchedule.recordFailure(f.task.ID, ceilingDeferralIdentityKey(f.deferral))
	require.False(t, f.svc.ceilingAdmissionCandidateEligible(ctx, task, f.deferral),
		"a record whose replay failed must not hold head precedence")
}
