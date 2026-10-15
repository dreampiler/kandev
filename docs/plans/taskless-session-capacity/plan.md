---
created: 2026-10-15
status: in-progress
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
  - REQ-OFFICE-TASKLESS-001
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
  - ../../specs/office/system-design/taskless-run-sessions.md
legacy_specs: []
---

# Taskless Office session capacity

## Requirements

An enabled install-wide session ceiling must include taskless Office executions.
Task sessions, run sessions and in-flight reservations share one atomic admission
decision. A capacity refusal preserves the same queued run and resumes without
operator action when capacity returns. Provider/action-required blocks, owner
admission, routing order, retry history and existing task behavior remain intact.
Disabled capacity retains its existing unlimited behavior. No settings change,
synthetic task, new queue, poller, permanent test or runtime flag is introduced.
Execution grades, utility counting and manual override policy remain subsequent
slices of the broader execution-capacity goal.

## Design

Extend the orchestrator's existing controller with an optional Office population
source. Prefix run-session keys to distinguish them from task-session keys and
union persisted preparing/running rows with reservations under the same mutex.
Reserve before run-session persistence and runtime allocation. Confirm the
reservation immediately after the preparing row commits, transferring counting
to that row. A pre-persistence failure returns its reservation; later failures
retain their row until cleanup reaches a terminal state. The Office repository
signals the existing ceiling driver only after a successful terminal compare-and-set.

Capacity-only taskless refusals park the existing run with a distinct routing
reason, without creating an attempt or incrementing failure/backoff state. The
existing release-driven/20-second sweep clears only that reason, in queue order
and bounded by observed free slots. Existing Office scheduling claims and launches
the run through its normal owner, budget and routing checks. Task-bound ceiling
replay keeps its existing single owner. Population read errors do not release
capacity-blocked work while enabled. No run-terminal history is replayed.

## Plan

1. Add the optional run population, reservation and retry seams to the existing
   controller; preserve its task and unlimited paths.
2. Connect Office persistence, launch admission and terminal notification at the
   backend composition root.
3. Distinguish taskless capacity refusal in concrete and routed dispatch and
   restore only capacity-blocked queued runs through the current sweep.
4. Run the affected existing Go checks with `-trimpath` and temporary overlay
   probes for contention, exact ownership, refusal/recovery and disabled mode.

## Work order and verification

[Task 01: shared capacity](task-01-shared-capacity.md) owns this implementation.

The capacity worktree is the only implementation writer. Git publication belongs
to the integrating session. The existing #4479/#4503 diffs are read-only references;
their task-recovery/queue changes are not copied. Use existing fake runtime and
SQLite fixtures. Temporary probes live outside the repository's collection paths
and are removed after verification. Verify a task-owned slot blocks a taskless
launch, releasing it resumes the same run, run rows/reservations are counted once,
and late/duplicate predecessor completion cannot release a successor's capacity.
Do not run operational endpoints or change the running installation.
