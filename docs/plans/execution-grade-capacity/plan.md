---
status: in_progress
created: 2026-10-23
requirements:
  - REQ-AGENTS-SESSION-CEILING-003
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# feat(capacity): add opt-in total and execution-grade limits

Opt into all-session and explicit execution-grade capacity with existing reservation, FIFO replay and queue presentation contracts.

## Delivery order and dependencies

The implementation is one existing PR slice. Preserve its native dependencies and existing feature defaults, then verify the narrow affected paths. No dependent PR is imported into this package.

- [Implementation and verification](task-01-implementation.md)

## Scope and risks

Changing disabled defaults, assigning grades from profile names, or changing dynamic candidate order. These remain outside this delivery slice. Preserve existing files, configuration and history; report unavailable integration coverage without inventing a successful result.

## Verification strategy

Reuse the completed focused implementation checks recorded below and the existing documentation-coverage evaluator. Only remaining review fixes require their affected checks to run again.

## ASCII UI preview

UI-01: Existing affected surface, after this change.

```text
Task Behavior > Session capacity
  Policy: automatic | all
  Total limit: [ ]
  Grade             Limit
  control           [ ]
  work              [ ]

The existing section stacks on phone; controls retain their labels and save action.
```

This sketch records the affected structure; spacing is illustrative. It does not add a new control or approval requirement.
