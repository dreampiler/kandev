---
id: "02-entry-scoped-signals"
title: "Scope accepted signals to workflow entries"
status: done
wave: 2
depends_on:
  - "01-completion-recovery"
plan: "plan.md"
requirements:
  - REQ-TASKS-WORKFLOW-EXPLICIT-COMPLETION-SIGNAL-004
acceptance_criteria:
  - AC-TASKS-WORKFLOW-EXPLICIT-COMPLETION-SIGNAL-004.1
  - AC-TASKS-WORKFLOW-EXPLICIT-COMPLETION-SIGNAL-004.2
  - AC-TASKS-WORKFLOW-EXPLICIT-COMPLETION-SIGNAL-004.3
  - AC-TASKS-WORKFLOW-EXPLICIT-COMPLETION-SIGNAL-004.4
  - AC-TASKS-WORKFLOW-EXPLICIT-COMPLETION-SIGNAL-004.5
  - AC-TASKS-WORKFLOW-EXPLICIT-COMPLETION-SIGNAL-004.6
system_design:
  - ../../specs/tasks/system-design/workflow-explicit-completion-signal.md
---

# Task 02: scope accepted signals to workflow entries

## Scope

Reuse `task_step_transitions.id` in both server-owned turn stamps and pending
signal metadata. Atomically compare task/session/step/entry at claim time.
Capture the source-entry bag for the gate, SQL admission, carry, and audit, then
conditionally clear only that bag after a successful move. Preserve same-entry
first-payload-wins, retry publish, ownership/profile checks, question/cancel
barriers, and unambiguous legacy timestamp fallback.

Timestamp-only identity is insufficient for same-step leave-and-return races and
conditional consumption. Additional tables, watchers, caller identifiers, and
changes to review-run creation are outside this work order.

## Implementation and verification

Source-adjacent units cover six families: server stamps on both turn writers;
fresh and stale-turn claims across same-step re-entry; duplicate/concurrent
first-payload and publish behavior; legacy/engine/subscriber gating; captured
carry/audit plus failed-move and replacement-bag retention; legacy compatibility
and ambiguous-entry diagnostics. Existing environment-gated PostgreSQL coverage
checks concurrent claims and conditional consumption across two connections.

Run filtered tests in `internal/task/models`, `internal/task/repository/sqlite`,
`internal/mcp/handlers`, and `internal/orchestrator`. Use `-race` for concurrent
handler claims. Finish with build, vet, changed-package lint, and normal commit
hooks. No integration folder, E2E, CI, or full-suite addition is required.

The delayed subscriber cleanup and legacy conflict settlement were reproduced
before their corrections. Existing valid legacy carry/audit fixtures now include
a signal timestamp; dedicated undated-legacy cases retain the fail-closed
expectation.

## Related contracts

The pre-ledger compatibility correction backfills a missing current entry through
the existing task-row-guarded ensurer before rereading it. Source-adjacent tests
cover concurrent/idempotent backfill, acceptance from a turn predating the
backfill, all three consuming paths, and old-signal rejection after re-entry.
The creation timestamp is a compatibility floor for the first untracked entry,
not reconstructed transition history. Existing rows and later moves remain
authoritative. Entry-scoped admission is retained instead of a step-only bypass.

- [Plan](plan.md)
- [Requirement 004](../../specs/tasks/requirements/workflow-explicit-completion-signal.md)
- [System design](../../specs/tasks/system-design/workflow-explicit-completion-signal.md)
- [ADR 0015](../../decisions/0015-explicit-completion-signal-for-auto-advance.md)

## Verification results

Filtered unit suites passed in models, SQLite, MCP handlers, and orchestrator.
The handler concurrent-claim, first-payload, and claim-boundary cases passed with
go test -race -vet=off -trimpath -p 2; vet runs separately with the matching
-trimpath cache setting. Backend go build -trimpath -p 2 ./... and changed-package
go vet -trimpath -p 2 passed. PostgreSQL coverage was skipped by its existing
KANDEV_TEST_POSTGRES_DSN environment gate because no DSN was supplied.
Documentation catalog, spec lint, public-doc validation, and PR documentation
coverage checks passed. Fork filtered unit suites, concurrent handler and
entry-matcher/consumer race cases, changed-package build, and standalone vet
passed. Fork adapters record entry refusals through the existing diagnostic path
and use native identity before clock cutoffs in the reminder matcher. Lint and
normal hooks run at commit; independent workflow review remains pending.
