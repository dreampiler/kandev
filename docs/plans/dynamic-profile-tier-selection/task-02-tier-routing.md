---
id: "02-tier-routing"
title: "Select tier candidates from usage with durable transition chains"
status: in_progress
wave: 2
depends_on:
  - "01-selection-contract"
plan: "plan.md"
requirements:
  - REQ-AGENTS-TIER-SELECTION-003
  - REQ-AGENTS-TIER-SELECTION-004
  - REQ-AGENTS-TIER-SELECTION-005
acceptance_criteria:
  - AC-AGENTS-TIER-SELECTION-003.1
  - AC-AGENTS-TIER-SELECTION-003.2
  - AC-AGENTS-TIER-SELECTION-003.3
  - AC-AGENTS-TIER-SELECTION-003.4
  - AC-AGENTS-TIER-SELECTION-003.5
  - AC-AGENTS-TIER-SELECTION-003.6
  - AC-AGENTS-TIER-SELECTION-004.1
  - AC-AGENTS-TIER-SELECTION-004.2
  - AC-AGENTS-TIER-SELECTION-004.3
  - AC-AGENTS-TIER-SELECTION-004.4
  - AC-AGENTS-TIER-SELECTION-004.5
  - AC-AGENTS-TIER-SELECTION-005.1
  - AC-AGENTS-TIER-SELECTION-005.2
system_design:
  - ../../specs/agents/system-design/dynamic-profile-tier-selection.md
---

# Task 02: Usage-Aware Tier Routing

## Summary

Wire usage observations, manual window totals and pure tier ranking into the
shared resolver/conductor. Persist chain exclusions and route reasons using
existing transactional route state; expose the same computation as a read-only
settings preview.

## In scope

Window durations/reset validity, candidate/account applicability, manual ledger
query, order/pace/cost comparators, failure direction, chain lifecycle, healthy
stickiness and preview authorization. Join the usage event's turn to
`task_session_turns.execution_profile_id` for concrete attribution; never aggregate
the logical dynamic profile as though it were the concrete candidate. A proven
gap in that existing contract must be reported before expanding the ledger schema.

## Out of scope

New provider clients, account discovery by guessing, scheduler polling, global
mode, billing-system replacement, UI, runtime configuration and upstream work.
Do not change the existing effect-safe or unclassified-fallback admission scope.

## Acceptance

1. Numeric pace, manual calendar totals, unknown ranking, explicit free/no-window
   and configured cost/reservation behavior match the reviewed design decisions;
   observed usage belongs to the candidate actually executing.
2. Policy wait/retry/stop precedes tier direction; exclusions survive skip,
   restart and profile edits; healthy work stays on its model; racing failures
   produce at most one successor with existing continuation safety.
3. Preview matches the decision for identical inputs without route/health/save
   mutation, and committed attempts record the selection reason.

## Verification

From `apps/backend/`, run sequentially:

```powershell
go test ./internal/agent/usage -count=1 -timeout=10m
go test ./internal/agent/runtime/dynamic ./internal/agent/runtime/routingpolicy -count=1 -timeout=10m
go test ./internal/agent/runtime -run 'Test.*Dynamic' -count=1 -timeout=10m
go test ./internal/task/repository/sqlite -run 'Test.*(DynamicRoute|DynamicTier|DynamicManual|Usage)' -count=1 -timeout=10m
go test ./internal/task/usage -count=1 -timeout=10m
go test ./internal/backendapp ./internal/orchestrator -run 'Test.*Dynamic' -count=1 -timeout=10m
go test ./internal/agent/settings/controller -run 'Test.*Dynamic' -count=1 -timeout=10m
```

Use the plan's focused backend lint command after changes settle. Run the
PostgreSQL query counterpart with an isolated `KANDEV_TEST_POSTGRES_DSN`:

```powershell
go test ./internal/task/repository/sqlite -run 'TestPostgres.*DynamicManualWindow' -count=1 -timeout=10m -v
```

Its environment-gated skip does not satisfy parity evidence.
Add the plan's named cases to existing suites: clock floor/worst window,
unknown/free ordering, valid/invalid reset, manual boundary/partial observations,
two logical sessions on one concrete model, A-B-C failure chain, same-tier versus
next-tier, restart, duplicate events, stale claim, policy stop and preview no-write.
Inspect actual persisted route state/reasons, not only emitted log text.

## Files likely touched

- `apps/backend/internal/agent/usage/types.go`, `client_claude.go`, `client_codex.go`
- `apps/backend/internal/backendapp/usage_adapter.go`, `dynamic_routing.go`
- `apps/backend/internal/agent/runtime/dynamic_resolver.go`
- `apps/backend/internal/agent/runtime/dynamic/engine.go`, `types.go`, `conductor.go`
- Proposed `apps/backend/internal/agent/runtime/dynamic/selection.go`
- `apps/backend/internal/task/repository/sqlite/dynamic_route.go`
- `apps/backend/internal/task/repository/sqlite/usage_totals.go`
- `apps/backend/internal/task/models/usage_event.go` and existing turn types as read contracts
- Settings controller/handlers for proposed dynamic preview endpoints

## Dependencies and risks

Requires Task 01. Before affected logic, settle the plan's cost/reservation/off
semantics with control and prove native account binding and ledger attribution.
Do not call a missing read a zero. Fetch outside the engine lock and recheck
generation after fetch. Do not clear the chain when clearing retry counters.

## Results

Partial. Commit `d7caec982` delivers the pure ranking core only. The remaining
work orders are not started, so this work order is not complete.

### Delivered so far

- `usage.UtilizationWindow` gained additive `DurationSeconds`, `StartAt` and
  `ModelID` plus a `UsableFor` predicate. A window needs a numeric duration, a
  known reset and a non-positive span to score; it is never parsed out of the
  display `Label`, and an unusable window is unknown rather than zero.
- `dynamic/ranking.go` holds the pure, side-effect-free ranking a preview and a
  live selection can share: `PaceFromWindows` with the clamped elapsed fraction
  and the 0.05 floor, the largest-pace-across-windows rule, invalid-window
  rejection, known-before-unknown ordering with saved row order on ties,
  `CostRank` class ordering with the legacy unknown class last, and
  `ReservationExclusion` as an admission gate that leaves raw pace untouched.

### Verified

| Check | Exit | Result |
| --- | --- | --- |
| `go test ./internal/agent/runtime/dynamic -count=1` | 0 | pass, includes the new pace, cost, order and reservation cases |
| `go test ./internal/agent/usage -count=1 -timeout=10m` | 0 | pass |
| `golangci-lint run ./internal/agent/runtime/dynamic/... ./internal/agent/usage/...` | 0 | 0 issues |
| `go build ./...` | 0 | pass |

Two table expectations in the new ranking tests were wrong and were corrected;
the implementation matched the reviewed semantics in both cases.

### Second increment: engine wiring and durable chains (commit `17080b099`)

- `dynamic/chain.go` holds the durable `SelectionChain` carried inside
  `PolicyStateJSON`: concrete candidate identities, never badge numbers, bounded
  by the candidate list. It continues across retry, skip and restart and is only
  closed by a new attempt.
- `ResolveSelection` handles a fresh selection and `ResolveFallbackSelection`
  handles a permitted failure transition. Keeping them apart is load-bearing: a
  new user turn starts a new chain, while only a failure transition applies the
  tier's configured direction. Deriving the direction in both paths broke
  `TestEngineQuotaFailureSkipsSiblingsOnSharedBinding`, which re-selects a
  healthy candidate on a new turn.
- `same_tier_next` keeps the failed tier and advances only once it is exhausted;
  `next_tier` skips its remaining peers. `TierSearchOrder` never wraps.
- `selectWithPlan` fetches the usage snapshot outside `Engine.mu`, takes one
  observation for the whole decision, and rechecks admission during the existing
  atomic claim. A lost probe race re-ranks instead of ending the decision, and a
  losing probe never marks a candidate tried.
- The unclassified fallback path still uses its own narrower
  `candidateSelectable`, so its admission scope did not widen.

### Three defects the existing suite caught

- A failed probe claim gave up instead of reconsidering, so an exhausted circuit
  ended the selection rather than advancing.
- The chain blocked an explicit same-candidate retry; a permitted retry is not a
  cross-candidate selection and must not be excluded.
- `selectWithPlan` exceeded the 80-line limit until the exhausted-selection
  branch was extracted.

### Verified for this increment

| Check | Exit | Result |
| --- | --- | --- |
| `go test ./internal/agent/runtime/dynamic ./internal/agent/runtime -count=1 -timeout=10m` | 0 | pass, existing engine and conductor suites included |
| `golangci-lint run ./internal/agent/runtime/dynamic/... ./internal/agent/runtime` | 0 | 0 issues |
| `go test ./internal/backendapp -run 'Test.*Dynamic\|Test.*Route' -count=1` | 0 | pass |
| `go test ./internal/task/dto -run 'Test.*Dynamic\|Test.*Route' -count=1` | 0 | pass |

New tier cases: same-tier-next A-B-C walk then forward advance, next-tier peer
skip, no wrap to an earlier tier, chain survival across a restart, pace ranking
picking the least-used candidate, ranking only the first tier that has a
candidate, stop vetoing any automatic change, unknown usage never winning pace,
and chain exclusions surviving policy-counter changes.

`orchestrator` `TestCeilingDispatchAdmissionSerializesRouteMutation` is a
pre-existing flaky timing test; it fails on roughly half of repeated runs at the
untouched base and asserts only a claim renewal timestamp.

### Third increment: manual ledger aggregate (commit `873bcb98f`)

- `sqlite.GetManualWindowUsage` aggregates one concrete execution profile's
  recorded usage over the half-open interval `[start, end)`.
- Concrete identity comes from `task_session_turns.execution_profile_id` joined
  through the event's `turn_id`, never from the session's mutable current route
  and never from the event's logical `agent_profile_id`. The only fallback is an
  event whose own profile is already proven concrete, which is what
  `TestDynamicManualWindowUsageLeavesDeletedTurnsUnattributed` pins: a deleted
  turn stays unattributed, and the logical dynamic profile is never treated as
  a concrete candidate.
- The total is recorded-only and stays visible as a lower bound: unpriced and
  incomplete events are counted and returned alongside the sum, so
  `Complete()` reports the gap instead of the query collapsing to a wrong
  number. An empty query is a zero *recorded* total, not proof a subscription was
  never used.

### Verified for this increment

| Check | Exit | Result |
| --- | --- | --- |
| `go test ./internal/task/repository/sqlite -run 'TestDynamicManualWindowUsage' -count=1` | 0 | pass |
| `golangci-lint run ./internal/task/repository/sqlite/...` | 0 | 0 issues |
| `go build ./...` | 0 | pass |
| `go vet ./internal/task/repository/sqlite` | 0 | pass |

Six cases: two logical sessions on one concrete model, sibling candidate
exclusion, reset-instant boundary exclusion, partial and unpriced accounting,
deleted-turn attribution, empty query, and degenerate input.

The PostgreSQL counterpart named in the plan (`TestPostgres.*DynamicManualWindow`)
is **not** written yet, so this increment has no dialect-parity evidence. The
query itself is dialect-neutral (no `rowid`, no SQLite-only JSON or date syntax)
but that is unproven until the env-gated test exists.

The wider `sqlite` package run shows six failures. Each was reproduced at the
untouched base by stashing and repeating: `TestUpdateDocumentWritesEveryMutableFieldAndReportsMissing`,
`TestUpdateExecutorProfileWritesMutableFieldsAndReportsMissing`,
`TestUpdateExecutorProfileIfUnmodifiedRejectsStaleProfile`,
`TestResetExecutorReachabilityClearsTheRecordButAdvancesUpdatedAt`,
`TestUpdateTurnRejectsSnapshotStaleBehindMetadataPatch` and
`TestQuerySidebarTaskPageBreaksMalformedParentCycleAtSmallestID` are pre-existing
timestamp-ordering flakes on this host, not regressions.

### Fourth increment: reset windows and route reason codes (`99a56d187`, `c96019654`)

- `agent/usage.ResolveResetWindow` resolves the window containing an instant for
  all four manual periods. A five-hour window steps whole blocks from the
  anchor's fixed phase, so its length stays exactly five hours across a DST
  transition. Day, week and month are calendar boundaries resolved with
  `time.Date` in the target location, so a monthly plan resets on the calendar
  month rather than on a fixed 30-day duration.
- `ResetAnchor` carries the local reset time, weekday, day of month and phase in
  one documented space-separated form. A day of month beyond the month length
  clamps to that month's last valid day (31 January / 28 or 29 February).
- Route reason codes are a bounded set: `tier_order`, `tier_pace`, `tier_cost`,
  with `_same_tier` / `_next_tier` suffixes on a fallback. The rule part names how
  the successor was chosen and the suffix names the direction that produced the
  transition, taken from the failed candidate's tier rather than the successor's
  own policy. A row with no stored tier metadata keeps its established reason
  code, so existing consumers are unaffected.

### Verified for this increment

| Check | Exit | Result |
| --- | --- | --- |
| `go test ./internal/agent/usage -count=1` | 0 | pass |
| `golangci-lint run ./internal/agent/usage/...` | 0 | 0 issues |
| `go test ./internal/agent/runtime/dynamic ./internal/agent/runtime -count=1 -timeout=10m` | 0 | pass |
| `golangci-lint run ./internal/agent/runtime/dynamic/... ./internal/agent/runtime ./internal/agent/usage/...` | 0 | 0 issues |
| `go test ./internal/backendapp ./internal/orchestrator -run 'Test.*Dynamic' -count=1` | 0 | pass |

Cases cover anchor parsing and its rejection set, five-hour length across a US
spring-forward, phase shifting the block grid, calendar day/week boundaries,
calendar-month resolution with clamping, leap-day clamping, a day-31 anchor in a
31-day month, DST fall-back, degenerate input, the full reason-code table, the
persisted code plus direction, and legacy code preservation.

One implementation fix came from `staticcheck`: `monthReset` assigned an
intermediate value it then overwrote.

### Not delivered

Still outstanding for this work order: a real usage snapshot provider wired at
the composition boundary for Claude/Codex, numeric window duration plumbing from
those clients, and the two read-only preview endpoints. The PostgreSQL parity
test named in the plan is still not written, so the manual-window query has no
dialect-parity evidence. WO-03 must not ship before these exist, because the
editor would promise behaviour that is not yet implemented.
