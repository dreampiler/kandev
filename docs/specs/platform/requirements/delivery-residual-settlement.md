---
status: draft
system: platform
created: 2026-10-13
owners:
  - kandev
---

# Requirements: delivery residual settlement (post completed-stream retirement)

Status: design checkpoint (non-committed). Parent approval required before implementation.
Follows: `delivery-completed-stream-retirement` (PR #282, merged as `c616a21638`).

## Context (measured, read-only, operator-provided copy 2026-10-13 17:53 KST)

- Last-hour inflow (07:53-08:53 UTC) 579,822 rows splits as: completed-stream
  342,379 (59%) / block_resolved-pinned 111,131 (19%) / active-submission
  126,292 (22%) / uncompacted 20.
- The merged completed-stream predicate (a) retires only the first bucket after
  the payload grace. Steady-state retention is therefore an estimated
  ~41% of hourly inflow (~110-240k rows/h), NOT near-zero. Estimate only;
  confirmation needs the replaced-binary readback.
- 320 `block_resolved` terminal settlements pin ~1.08M rows. All 320
  submissions sit in terminal states (265 completed, 47 cancelled, 8 failed)
  with `fully_settled_at` NULL and zero open recovery blocks. 318/320 have no
  `workflow.on_turn_complete` row at all; 2 mismatch on identity.
- 220 active submissions (167 dispatching, 53 interrupted_unknown), none with a
  terminal binding. Owner states: 147 CANCELLED, 34 FAILED, 1 COMPLETED,
  24 WAITING_FOR_INPUT, 14 RUNNING. Estimate: ~182 residue (terminal owner),
  ~21 live, ~17 unclear.

## Requirements

### REQ-PLATFORM-DELIVERY-RESIDUAL-001: Settle dead-owner submissions

**Intent:** Streams pinned only by an active-state submission whose owner is
already dead must become eligible for the completed-stream predicate.

#### Acceptance criteria

- **AC-PLATFORM-DELIVERY-RESIDUAL-001.1:** An `prepared`/`dispatching`/
  `accepted`/`interrupted_unknown` submission whose owning session is terminal
  (COMPLETED, FAILED, CANCELLED) or missing shall reach a terminal submission
  state through the existing settlement path.
- **AC-PLATFORM-DELIVERY-RESIDUAL-001.2:** Submissions with live owners
  (RUNNING, WAITING_FOR_INPUT with recent arrivals) shall not be touched; no
  new submission state and no invented terminal event shall be introduced.

### REQ-PLATFORM-DELIVERY-RESIDUAL-002: Complete block_resolved with missing workflow receipt

**Intent:** A `block_resolved` settlement that is complete in every dependency
except the workflow receipt must be able to promote once that receipt is
durably recorded through the owning writer.

#### Acceptance criteria

- **AC-PLATFORM-DELIVERY-RESIDUAL-002.1:** A `block_resolved` terminal
  settlement whose submission is terminal and whose recovery blocks are all
  resolved shall become `completed` (with `fully_settled_at` and immutable
  terminal binding) once its required workflow outcome is durably recorded.
- **AC-PLATFORM-DELIVERY-RESIDUAL-002.2:** Where no workflow receipt exists,
  the guarded decision shall be recorded through the retention spec's Task 04
  no-op writer; retention itself shall not create workflow receipts, and
  `fully_settled_at` shall never be invented by age or estimate.

### REQ-PLATFORM-DELIVERY-RESIDUAL-003: Preserve replay, ACK and terminal accuracy

**Intent:** Residual settlement must not change recovery semantics.

#### Acceptance criteria

- **AC-PLATFORM-DELIVERY-RESIDUAL-003.1:** Replay, ACK, projected cursor, and
  terminal evidence for unsettled rows shall be preserved exactly; the
  contiguous-prefix rule, retained-terminal and retained-submission proofs,
  and generic-receipt requirement are unchanged.
- **AC-PLATFORM-DELIVERY-RESIDUAL-003.2:** A global `ReplayAge`/
  `ReplayEvents` reduction shall not be used as a substitute.

## Out of scope

- Changing the completed-stream predicate (a) itself.
- New standing monitors, ledgers, gates, CI, or E2E.
- Touching the production database from this design; operator execution only.
