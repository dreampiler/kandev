---
status: active
system: agents
created: 2026-10-13
owners:
  - kandev
---

# Native Review Run Recovery Requirements

## Overview

A native code-review pass returns to its caller immediately and finishes in the
background. Its value depends on the run reaching a recorded terminal state: an
operator reads the run history to learn whether a review passed or why it
failed, and the same task's next review request is suppressed while a prior pass
is still active.

Two failure shapes break that contract today. A terminal write that loses to a
busy database writer can leave a run `pending` or `running`, and a run that stays
non-terminal past the pass's own time budget has no live work behind it yet is
still treated as active. Either one strands the run and blocks the task's later
review requests until a process restart. This requirement covers the recovery
behavior; the review outcome itself is owned by
[Native Code Review](native-code-review.md).

## Terminology

- **Terminal write:** the single write that moves a run to `completed`, `failed`,
  or `cancelled`.
- **Abandoned run:** a run that is still `pending` or `running` after the
  progress-less time limit, with no live pass behind it.

## Requirements

### REQ-AGENTS-REVIEW-RUN-RECOVERY-001: Native Review Run Recovery

**Intent:** A native review pass always converges on a recorded terminal state,
and one stranded pass never makes a task's review surface permanently
unavailable.

#### Acceptance criteria

- **AC-AGENTS-REVIEW-RUN-RECOVERY-001.1:** When the write that records a review
  run's terminal outcome fails on the first attempt while the run is still
  non-terminal, the runner shall retry that write on a context detached from the
  review pass, with a bounded number of attempts and delays, so a transient
  storage failure does not leave the run `pending` or `running`.
- **AC-AGENTS-REVIEW-RUN-RECOVERY-001.2:** When a run's completion cannot be
  recorded, the system shall record the run `failed` with a reason instead of
  leaving it `running`.
- **AC-AGENTS-REVIEW-RUN-RECOVERY-001.3:** When a task's `pending` or `running`
  review run has been non-terminal past the progress-less time limit, the system
  shall record the run `failed` with a reason that identifies the abandonment,
  and shall not treat that run as live when a new review is requested, so a new
  pass for the same task starts instead of rejoining it.
- **AC-AGENTS-REVIEW-RUN-RECOVERY-001.4:** The progress-less time limit shall be
  greater than the review pass timeout, so a pass the runner still owns is never
  classified as abandoned.

## Out of scope

- The review outcome, prompt, batching, findings, and publication contracts
  owned by [Native Code Review](native-code-review.md).
- The restart-time sweep that closes runs left in flight by a stopped process.
- New background monitors, queues, metrics, configuration, or schema.
