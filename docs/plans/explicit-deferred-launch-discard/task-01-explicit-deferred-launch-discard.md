---
id: "01-explicit-deferred-launch-discard"
title: "Explicit deferred-launch discard"
status: in_progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-QUEUED-SESSION-OWNERSHIP-004
acceptance_criteria:
  - AC-TASKS-QUEUED-SESSION-OWNERSHIP-004.1
  - AC-TASKS-QUEUED-SESSION-OWNERSHIP-004.2
  - AC-TASKS-QUEUED-SESSION-OWNERSHIP-004.3
  - AC-TASKS-QUEUED-SESSION-OWNERSHIP-004.4
  - AC-TASKS-QUEUED-SESSION-OWNERSHIP-004.5
  - AC-TASKS-QUEUED-SESSION-OWNERSHIP-004.6
system_design:
  - ../../specs/tasks/system-design/queued-session-ownership.md
---

# Task 01: Explicit deferred-launch discard

## Summary

Add a user-decided discard for a pending ceiling-deferred launch that answers
`already_clear`, `discarded`, or `retained` and never hides an incomplete
settlement or clear. The automatic replay drop and the explicit path share one
settlement routine.

## Proposal disclaimer

The transport (`POST /api/v1/tasks/:id/deferred-launch/discard`,
`200 {outcome, reason?}`, no additional operator permission) is a proposal
pending the maintainer direction requested in `kdlbs/kandev#4508`. It is not an
agreed public contract.

## In scope

- `internal/task/models`: the closed outcome and retained-reason sets.
- `internal/orchestrator`: the shared settlement routine, the guarded
  identity-CAS clear that refuses a live foreign claim, and the exported discard
  entry point.
- `internal/task/service`: task-write plus executor-mutation authorization and a
  narrow injected settler; an unwired settler is an explicit error.
- `internal/task/handlers`: one thin route and response.
- Composition wiring in `internal/backendapp`.
- Focused tests in the existing ceiling replay test file and one route
  registration assertion.

## Out of scope

- Frontend affordance or copy.
- Session start, resume, stop, or steering; task-state writes.
- New schema, permissions, configuration, metrics, or queue.

## Validation

- `gofmt` on every touched Go file.
- `go build ./...` in `apps/backend`.
- `go test ./internal/orchestrator` (whole package) with the known
  Windows-only passthrough-attachment failures recorded separately.
- `go test ./internal/task/service -run DeferredLaunch`.
- `go test ./internal/task/handlers -run 'DeferredLaunchDiscard|RegisteredTaskCreate'`.
- `python3 scripts/list-docs.py validate` and
  `python3 scripts/lint-spec-files.py --all`.
- A local `pr-docs.cjs validateCoverage` run over the real changed-file set.

## Open contract questions for the maintainer

- Path and verb for the task-scoped discard.
- Response field names and whether `retained` should be a non-2xx status.
- Whether any operator-level permission is required in addition to task write.
- Whether an asynchronous shape is preferred; it would need a durable,
  queryable operation outcome rather than a bare accepted response.
