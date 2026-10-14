---
status: done
requirements:
  - REQ-TASKS-BOUNDED-QUEUE-ADMISSION-001
system_design:
  - ../../specs/tasks/system-design/bounded-session-queue-admission.md
---

# Bounded Session Queue Admission Delivery

## Outcome and authority

Port the existing bounded-acquisition queue repair from fork change
`0797bea018f8802210e913dc30008cefb34f521e` to upstream base
`15ed289c050f8bd2bd675c4542ffb796525bf991`. Preserve the original callback
lifetime, FIFO/cancellation, taken-message requeue, retryable transport result,
and durable attachment-cleanup recovery. This is one repair package, not a new
task or a changed recovery policy.

- [Requirements](../../specs/tasks/requirements/bounded-session-queue-admission.md)
- [System design](../../specs/tasks/system-design/bounded-session-queue-admission.md)
- [Work order](task-01-bound-acquisition-and-cleanup.md)

## Dependencies and exclusions

The accepted-work and primary-ownership proposal #4479, observed at
`cb7532ad868c93a9beffc1b368269083e4fb2458`, owns its existing
`ensureSessionRunningTimeout` predicate and recovery behavior. This package adds
only its admission-timeout predicate to the implementation base and does not
copy that proposal. Reconcile that small shared error branch at integration.

## Risk and verification

The acquisition budget must never be propagated to admitted callbacks, and a
callback deadline must not be relabeled as an acquisition timeout. Preserve
session references when cancelled waiters leave and preserve durable cleanup
after budget exhaustion. Use the work order's focused existing cases plus an
explicit temporary overlay outside automatic collectors. No standing case,
E2E, audit framework, model call, or operating-data change is authorized.
