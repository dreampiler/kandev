# Task 02: block_resolved workflow completion (design checkpoint)

Record a guarded workflow outcome (`not_required` / `no_automatic_transition`)
through the retention spec's Task 04 writer for a `block_resolved` terminal
whose settlement is otherwise complete, so the existing
`completeAgentDeliveryTerminalSettlementTx` promotion can run. Retention
creates no workflow receipts itself. `fully_settled_at` is set only by that
promotion, never by age.

The (c) follow-up resolves workflow-bound terminals from durable facts only,
without evaluating the workflow engine, moving a step, or publishing UI:

- A turn whose step-entry stamp (`workflow_step_id_at_start`) is not the
  task's current step was replaced by a step move: its completion can no
  longer drive an entry, so it records `not_required`.
- A current-step turn whose step declares no automatic `on_turn_complete`
  move records `no_automatic_transition` (the engine would deterministically
  take no action).
- A current-step turn whose step declares an automatic move stays pinned and
  is only counted; a real transition must not be fabricated retroactively.
- A terminal missing its turn row stays pinned (no entry or turn evidence).

Acceptance: the eligible measured `block_resolved` rows become completable;
workflow-bound terminals on a transitioning step and terminals without turn
evidence stay pinned; all proofs/prefix rules intact.
