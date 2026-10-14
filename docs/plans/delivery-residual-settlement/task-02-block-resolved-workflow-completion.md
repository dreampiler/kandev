# Task 02: block_resolved workflow completion (design checkpoint)

Record guarded no-op workflow outcomes (`applied` / `no_automatic_transition` /
`not_required`) through the retention spec's Task 04 writer for turns whose
`block_resolved` settlement is otherwise complete (terminal submission state,
no open recovery blocks), so the existing
`completeAgentDeliveryTerminalSettlementTx` promotion can run. Retention
creates no workflow receipts itself. `fully_settled_at` is set only by that
promotion, never by age.

Acceptance: 320 measured `block_resolved` rows become completable; missing
workflow stays pinned until recorded; all proofs/prefix rules intact.
