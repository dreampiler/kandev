---
created: 2026-10-13
status: draft
requirements:
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-001
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-002
  - REQ-PLATFORM-DELIVERY-COMPLETED-RETIREMENT-003
system_design:
  - ../../specs/platform/system-design/delivery-completed-stream-retirement.md
legacy_specs: []
---

# Implementation plan: completed-stream delivery retirement

## Overview

The operator observed the delivery inbox grow from 2.80 GB to 5.08 GB in about ten hours after a cleanup, with `agent_delivery_inbox` at ~2.29M rows and `agent_delivery_effects` at ~2.29M rows (about 1.88M reasoning fragments).
Payload compaction is already removing fragment bytes, but the rows are retained for the whole per-stream replay window.
This plan adds the missing bound for completed streams so their fragment rows retire after the payload grace, readback-verified on a replaced binary.

This is the design/work-order checkpoint only: production code and standing-test changes wait for the parent's later explicit implementation request.
All artifacts are uncommitted; no upstream write is performed.

## Baseline

- Fork checkout `feature/inbox-effects-al6` at `94e2cfbfa9`; `origin` = `dreampiler/kandev` (fork). PR work is fork-only; no upstream fetch/push was performed.
- Installed build includes the fork delivery-row retirement change (#251, `addc2c1d6b`), which added the explicit known-type classifier and stopped stale session-state rows pinning checkpoints.
- Metrics referenced: `agent_delivery_inbox`, `agent_delivery_effects`, `task_session_messages` (operator measurement, DB copy, 2026-10-13 15:3x KST). Treated as reference evidence, not as this worker's measurement.
- `agent_delivery_*` inbox/projection lag `-1` means unavailable and is never read as zero.

## Root cause (source-confirmed)

Retirement is gated per stream by `max(24h projected age, 10000 events behind the projected head)` and is applied uniformly to terminal and non-terminal streams.

- Head gate: `agent_delivery_retention_discovery.go:117-123`.
- Locked candidate gate: `agent_delivery_retention_events.go:105-119` (`ageExpired || countExpired`).
- Defaults: `ReplayAge = 24h`, `ReplayEvents = 10000` (`config/source.go:400-408`, `config/catalog.go:119-120`).
- Compaction empties the payload but not the row (`agent_delivery_retention_events.go:204-241`), so rows, indexes and generic receipts grow until the window expires.
- Retirement is a contiguous projected prefix (`agent_delivery_retention_discovery.go:94-105`).

A completed stream with fewer than `ReplayEvents` fragments is bounded only by `ReplayAge`, so the inbox retains about a full window of inflow.
Because an expired replay already reconciles from the authenticated projected cursor (`agent_delivery.go:636-641`; `agent/runtime/lifecycle/streams.go:674-683`) and retirement metadata survives independently of the fragment row (`agent_delivery_retention_proof.go:9-57`), retaining a completed stream's fragment rows serves no consumer.
The change is therefore a predicate widening, not a new deletion reach.

Open secondary question (confirm before implementing, not a blocker): whether SQLite cleanup capacity is also below the observed inflow. The existing worker snapshot distinguishes this (`oldest_eligible_age_seconds`, `event_rows_retired`, `backlog_remaining`), and the earlier retention package recorded failing SQLite latency gates (`docs/plans/backend-agent-delivery-retention/plan.md` "Current integration checkpoint").

## Scope

In scope (one fork PR): the completed-stream eligibility predicate in the retention selector, its companion backend unit tests, and the operational one-time reclamation procedure documentation.

Out of scope: coalescing reasoning/message-chunk events at receive, journal or wire time; changing the default replay window for active/uncertain streams; schema, index, wire or journal changes; new gates/monitors; the operator's production database run; the upstream PR.

## Method comparison

| Option | Change | Cost / risk | Replay meaning | Verdict |
| --- | --- | --- | --- | --- |
| (a) Retire completed-stream fragments after the grace | Extend the locked eligibility predicate; keep checkpoint, submission, terminal and workflow receipts | Low; reuses existing bounded batch, locks and reconciliation; strict superset of current deletion reach | Unchanged: expired replay reconciles from projected cursor; rows a live consumer needs remain | **Recommended** |
| (b) Coalesce reasoning/message-chunk at receive | Batch fragments into one inbox/journal row | High; changes event cardinality, sequences, cursor identity, projection and agentctl journal semantics | Alters the durable sequence contract | Rejected for this task |

(a) directly targets the observed row growth and preserves the durable identity/sequence contracts. (b) reduces cardinality but still retains rows for the replay window and risks the delivery contract. Consistent with the owner's instruction not to substitute simple payload compaction or log deletion for the goal.

## Work orders

- [x] [Task 01: Retire completed-stream fragments after the grace](task-01-completed-stream-prefix-retirement.md)
- [ ] [Task 02: Document and read back the one-time backlog reclamation](task-02-operational-reclamation-and-readback.md)

Both are one task's checklist, sequential. Task 01 is the production change; Task 02 is documentation plus the operational readback procedure. Neither creates a separate table- or fragment-level task.

## One-time operational reclamation (documentation only)

Already accumulated rows are reclaimed by the operator through the existing offline entry point; this worker must not run it and must not issue ad hoc `DELETE`/`VACUUM`/`PRAGMA` against the production database.
The reduced-window environment override (`KANDEV_TASK_DELIVERY_RETENTION_REPLAY_AGE=1h`) is **not** the fix and must not be used as a substitute for the completed-stream predicate; the replaced binary retires completed-stream fragments on its own after the payload grace.
Exact row conditions, the rows to keep, the exclusive-access requirement and the execute/re-run/restore order are recorded in [Task 02](task-02-operational-reclamation-and-readback.md).

1. Stop the Kandev service so the database has exclusive access (`--execute` requires it).
2. Run `kandev maintenance database --execute --compact`. It takes and verifies a backup under `<database-dir>/backups/`, deletes in bounded transactions (rerun to continue if partial), and stages a VACUUM INTO copy with a `.pre-compact-<timestamp>.bak` rollback file.
3. Restart the replaced binary and read back one hour of natural inflow, projection and terminal settlement, plus the same row counts and projected/terminal state.

No hand-rolled SQL, no script, and no production execution by this worker.

## Tests and verification

- Companion backend unit tests beside the retention source (Task 01): completed stream retires after the grace without waiting for `ReplayAge`/`ReplayEvents`; active/uncertain stream does not; protected or unknown row still stops the prefix; checkpoint and terminal/workflow receipts remain.
- Narrow targeted command (Task 01), from `apps/backend`:
  `go test -race -run 'CompletedStream|Retention' ./internal/task/repository/sqlite ./internal/task/deliveryretention`
- One-off, not standing: reproduce on a disposable database copy or narrow temporary database and read before/after row and byte counts through the real retention batch entry point. No new standing test suite, integration/E2E folder, monitor, ledger, gate or CI job.
- Package validators for the docs: `python3 scripts/list-docs.py validate` and `python3 scripts/lint-spec-files.py --all`.

## Approval needed (user/owner only)

- Operator run of the one-time production database reclamation (exclusive access and backup). This worker performs none.
- Upstream proposal of the fork change after implementation; upstream writes belong to 93/W091, not to this task.
- Merge gate: a replaced-binary observed readback plus an independent review of the COMPLETED result; technical failure or empty findings is not a pass.

## Risks

- Premature "completed" classification could retire evidence a reconnect still needs; the predicate requires received == projected, no unprojected gap, and no unresolved terminal/submission/block/effect under the existing stream lock.
- A protected/unknown row still pins a prefix; this change does not widen past protected gaps.
- SQLite throughput may also bind; the existing snapshot metrics distinguish it.
- Physical shrinking remains an offline operation.

## Design-index alignment

The project design card `설계카드.md` invariants 1-5 are consistent with this change: no caller-generated identity (invariant 1), no new recovery-line coupling (invariant 2), retained user-visible control (invariant 3), profile isolation unchanged (4), and per-task worktree isolation respected here (5).
The design index `설계색인.md` has no delivery-retention pointer (`모름`/index gap); the owning contract is the repository's platform retention pair, cited above. No invariant conflict found.

## Handoff

The four-artifact package is complete and uncommitted in this worktree.
Implementation of Task 01 and Task 02 starts only after the parent's explicit implementation request.
Remaining for the parent/operator: the one-hour replaced-binary readback and the production reclamation run.
No production code, standing test, or upstream write is included in this checkpoint.
