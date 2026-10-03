---
created: 2026-10-01
status: draft
requirements:
  - REQ-TASKS-CHILD-STALL-001
  - REQ-TASKS-CHILD-STALL-002
  - REQ-TASKS-CHILD-STALL-003
system_design:
  - ../../specs/tasks/system-design/child-turn-stalled-signal.md
legacy_specs: []
---

# Child-turn Stalled Signal Design Package

## Scope and status

This package supplies the missing reviewable requirements, design, plan, and
work orders. It does not implement the feature or select an option for the
owner. The later reassignment permits these design files despite the earlier
task-plan sentence excluding repository files. Unrelated dirty work remains
outside this package. No commit, PR, runtime change, or permanent test is part
of this design turn.

Tasks owns the durable parent/child, turn, entry, and notification contract.
The [requirements](../../specs/tasks/requirements/child-turn-stalled-signal.md)
and [system design](../../specs/tasks/system-design/child-turn-stalled-signal.md)
are both draft. Existing stall recovery and peer-report batch designs are
adjacent, not dependencies or permission to implement their changes.

## Owner decision

**Recommendation: B, conditional on identifying the monitor integration. Owner
selection: pending.** The original phrase "Choose B" is a recommendation,
not evidence of owner authorization. Both options retain the full settlement
predicate, durable deduplication, and normal parent admission rules.

| Dimension | A: producer-qualified direct parent item | B: internal candidate feed and selective delivery |
| --- | --- | --- |
| Parent demand | One item per producer-qualified candidate | One item per monitor-qualified candidate |
| Ordinary turns | Must suppress them in producer | Monitor suppresses them without parent slots |
| Slot/token cost | Up to approximately 45 items/day under supplied candidate estimate | At most the same estimate if every candidate is actionable; actual savings unknown |
| Other processing | Eligibility/classification at producer; durable delivery still needed | Up to approximately 778 classifications/day if all supplied turns were eligible child settlements |
| Failure surface | Fewer components, but classifier tightly coupled to turn handling | Durable feed, consumer leases, lag, classifier integration, keyed delivery |
| Delay | Prompt queue admission after settlement | Consumer latency plus queue admission; healthy event path avoids waiting for polling |
| Serial parent | FIFO work can still be delayed by actionable alerts | Same per-alert impact, potentially fewer alerts; no interruption |

The supplied historical sample is 135 candidate segments / 2,335 turns over
three days: 5.78%, 45 candidate segments/day, 778.33 turns/day. These are
unverified task-context observations, not a production forecast. The child
subset and actual actionable fraction are unknown. Do not present 45 as a
hard cap or imply B necessarily reduces it.

For a mean parent alert turn of `t` minutes and `p` billable tokens, A's sizing
case is `45*t` slot-minutes and `45*p` tokens/day. B is `q*t` and `q*p`, where
`q` is measured qualified deliveries, plus consumer work for up to 778 events.
Illustratively, at two minutes each, 45 alerts occupy 90 slot-minutes/day;
this is not a measured runtime. Monetary cost cannot be calculated without
model pricing and input/output counts. No paid classifier is assumed or
authorized; one would require a revised cost comparison and explicit scope.

Correcting the earlier storage estimate: if B durably ingests every live child
settlement, its receipt creation rate is the eligible child subset of up to
778/day in this sample, not merely 45/day. Seven days at 778/day is about
5,450 resolved rows, plus unresolved failures. At an illustrative 1 KiB/row
that is about 5.3 MiB before indexes and database overhead; no size measurement
has been made. The seven-day retention and retry constants are draft defaults.

Owner action is to select A or B after reviewing these tradeoffs. B also needs
the monitor owner/entrypoint and structured evidence contract identified.
Selecting A requires moving the same classifier to the producer and revising
Task 02 before implementation. This design handoff requests no immediate
approval or model switch and does not claim a confirmed decision.

## Delivery order

1. [Task 01: Capture live settlement and durable identity](task-01-live-settlement.md).
2. [Task 02: Classify durable candidates](task-02-candidate-classification.md), after 01.
3. [Task 03: Deliver and expose parent alerts](task-03-parent-delivery.md), after 02.

All work orders are pending. No implementation delegation is authorized. A
later implementation instruction, owner selection, and monitor integration
resolution precede execution. The table/schema, classifier, and queue delivery
form sequential verification boundaries, not independently released features.

## ASCII UI preview

UI-01: Existing parent conversation and queued-message region. Shared content
composition for desktop and phone; phone uses its dedicated task conversation,
single vertical scroll, wrapping labels, and accessible child navigation.

```text
Parent conversation
  System: Child needs attention
  [Open child: Child title]
  Step: Review
  Cause: Input required
  Turn: <turn reference>

Queued messages
  Child needs attention - Queued
```

Structure and attribution are required by AC-TASKS-CHILD-STALL-003.1; text and
spacing are illustrative, localized at implementation. Delivered items live in
history. Pre-queue failure and monitor lag are operator diagnostics, not a
fabricated delivered chat item. UI-01 does not add an interactive retry control
to the parent conversation. Verify desktop and phone readability and opening
the same child from the existing conversation route.

## Verification strategy and risks

No production or test execution belongs to this design turn. Work orders specify
targeted existing tests and explicitly invoked temporary Go overlays for new
behavior. Put temporary cases under the OS temporary directory, outside normal
test discovery; remove overlays after retaining concise results. Do not commit
new permanent tests without separate authorization. Focused rendered desktop
and phone checks cover UI-01 after implementation; do not run the whole suite.

Required scenario coverage across 01-03: successful ordinary turn, every cause,
optional versus required completion signal, abandonment, rejected dispatch,
same-step re-entry, deferred transition race, duplicate/reordered event, busy or
offline parent, Auto-run OFF, pending question, queue full, failed reads, crash
at queue insertion, consumer restart, retry exhaustion, retention replay, and
zero workflow mutations. An unresolved external dispatch is not proof of an
exactly-once model turn. New source symbols and test names in work orders are
proposed, not existing coverage.

Key risks are live-end provenance, atomic entry capture, monitor availability,
duplicate question delivery, and the queue/receipt crash window. None is solved
by treating `turn.completed` as a workflow transition. The final integration
must preserve ordinary turn latency and existing task admission ownership.

## Design validation

On 2026-10-01, `python -X utf8 scripts/list-docs.py validate` passed (338
decisions, 1,285 specifications); `python -X utf8 scripts/lint-spec-files.py
--all` passed; `python -X utf8 scripts/lint-spec-files.test.py` passed all 36
tests. Catalog lookup discovers both new task specifications. Scoped
`git diff --check` passed, but does not examine untracked new files; those
files were read separately for links, encoding, and whitespace. Implementation
and rendered verification are not run and all work orders remain pending.
