---
id: "14-abandoned-native-admission"
title: "Recover admission after an abandoned native review"
status: building
wave: 11
depends_on: ["04-run-orchestration"]
plan: "plan.md"
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-001
acceptance_criteria:
  - AC-AGENTS-NATIVE-CODE-REVIEW-001.14
system_design:
  - ../../specs/agents/system-design/native-code-review-recovery.md
---

# Task 14: Abandoned native review admission

Restore explicit review admission when a native worker has exited but its
terminal-state write did not settle the retained run.

## Scope and acceptance

- Apply AC-AGENTS-NATIVE-CODE-REVIEW-001.14 through
  [the recovery design](../../specs/agents/system-design/native-code-review-recovery.md).
- Confirm terminal settlement before fresh admission. Keep original rows,
  findings, duration, agent ownership, live-worker deduplication and entry IDs.
- Preserve read and settlement errors without dispatching a duplicate worker.
  Do not change inference timeouts, candidate order, provider fallback, schema,
  startup cancellation, polling or UI behavior.

## Files and verification

`apps/backend/internal/review/runner.go` and `recovery.go` own the change.
Run `go test ./internal/review -count=1` from `apps/backend` and the changed-file
Go lint from the repository root. A collector-excluded temporary Go overlay
uses the existing runner harness to compare native manual/workflow abandonment
with agent ownership; retain its observed results here without adding a test
framework or standing collector.

## Results

The temporary exact-runner proof failed both native cases on the original source
and passed after classified settlement; the agent-owned case remained unchanged.
The selected existing active-run, entry-redelivery, concurrency and cancellation
checks passed. A temporary store reproducing the service's split read/write
boundary confirmed that cancellation acknowledgement can be overwritten without
serialization; removing only the new settlement mutex failed that assertion.
With serialization the same proof passed. The existing review package, scoped
Go lint, documentation catalog and specification lint passed. An initial
synctest fixture was interrupted because mutex waits are not durably blocked;
that fixture result supplies no production failure or passing evidence.
No live provider, database or installed-runtime acceptance is claimed.
