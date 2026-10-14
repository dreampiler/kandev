---
status: draft
system: platform
requirements:
  - REQ-PLATFORM-DELIVERY-RESIDUAL-001
  - REQ-PLATFORM-DELIVERY-RESIDUAL-002
  - REQ-PLATFORM-DELIVERY-RESIDUAL-003
---

# System design: delivery residual settlement

Status: implemented (second and third PR). Companion to
`requirements/delivery-residual-settlement.md`.

## Where the pin lives

1. **Active-submission pin.** `agentDeliveryCompletedStreamSQL`
   (`agent_delivery_retention_proof.go:117-131`) requires NO submission of the
   incarnation in `prepared`/`dispatching`/`accepted`/`interrupted_unknown`.
   220 submissions sit in those states; ~182 have terminal owners
   (147 CANCELLED, 34 FAILED, 1 COMPLETED) and no terminal binding. They are
   never reaped because no path settles a submission whose owner died first.
   The settlement writer only runs on terminal-effect admission
   (`agent_delivery_settlement.go:111-154`).

2. **block_resolved pin.** `settleAgentDeliveryTerminalTx` leaves the effect in
   `block_resolved` (`agent_delivery_settlement.go:144-148`); promotion to
   `completed` needs `hasExactAgentDeliveryWorkflowOutcomeTx`
   (`agent_delivery_settlement.go:448-490`). 318/320 measured rows have no
   workflow row at all, so `completeAgentDeliveryTerminalSettlementTx` returns
   false forever and the suffix stays pinned. This matches the retention spec:
   `pending` and `block_resolved` are protected
   (`backend-agent-delivery-retention.md:66`); the owning fix is the Task 04
   no-op writer (`backend-agent-delivery-retention.md:79-87`), and retention
   never creates workflow receipts (`:87`).

## Change shape (for the later implementation order, NOT this checkpoint)

- A bounded settlement pass over dead-owner submissions only (owner terminal
  or missing, verified under the existing stream/session locks), reusing
  `settleAgentDeliveryTerminalTx` and the existing outcome evidence. Live
  owners untouched. Every terminal owner, including CANCELLED, must have no
  pending recovery or queue evidence. The pass requires exactly one projected
  terminal row for a submission; multiple rows remain unsettled.
- PR #298 writes only `not_required` workflow receipts for tasks with no
  workflow step through the Task 04 writer, then promotes via the existing
  `completeAgentDeliveryTerminalSettlementTx`. The (c) follow-up resolves
  workflow-bound terminals from the turn's step-entry stamp and the current
  step's `on_turn_complete` declaration, without evaluating the engine: a
  replaced turn records `not_required`, a current-step turn on a step with no
  automatic move records `no_automatic_transition`, and a current-step turn on
  a step with an automatic move stays pinned. No age-invented
  `fully_settled_at` (`backend-agent-delivery-retention.md:252`).
- Both reuse existing locks, bounded batches, and the contiguous-prefix
  retirement; no schema/index/wire/journal change; no new collector.

## Expected effect (estimate, to be confirmed by replaced-binary readback)

- Dead-owner settlement unpins ~182 streams; block_resolved completion unpins
  ~1.08M backlog rows plus 19% of hourly inflow. Combined with (a), retained
  inflow drops from ~41% toward live + window-only residue.
- Confirmation is the operator's one-hour natural inflow readback on the
  replaced binary, same procedure as Task 02.

## Test sketch (companion unit tests, same package)

- Dead-owner submission settles to terminal state; live-owner submission
  untouched.
- block_resolved + recorded no-op workflow promotes to completed with
  `fully_settled_at`; missing workflow stays pinned.
- Replaced turn records `not_required`; a current-step turn on a step with no
  automatic move records `no_automatic_transition`; a current-step turn on a
  step with an automatic move stays pinned and is only counted; a terminal
  without its turn row stays pinned.
- Checkpoint/submission/terminal/workflow rows survive; prefix stops at
  protected rows.
