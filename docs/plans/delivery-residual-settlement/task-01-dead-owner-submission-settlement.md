# Task 01: dead-owner submission settlement (design checkpoint)

Settle `prepared`/`dispatching`/`accepted`/`interrupted_unknown` submissions
whose owning session is terminal (COMPLETED/FAILED/CANCELLED) or missing,
through the existing `settleAgentDeliveryTerminalTx` path, under existing
locks. Live owners (RUNNING, WAITING_FOR_INPUT with recent arrivals) are out
of scope. No new states, no invented terminal events, no schema change.

Acceptance: measured ~182 residue streams become completed-stream eligible;
live streams unchanged; all proofs/prefix rules intact.
