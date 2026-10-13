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

Three failure shapes break that contract today. A terminal write that loses to a
busy database writer can leave a run `pending` or `running`; a run that stays
non-terminal past the pass's own time budget has no live work behind it yet is
still treated as active; and a pass that overruns its time budget ends the whole
attempt even though the dynamic route may have another candidate that could
finish. The first two strand the run and block the task's later review requests
until a process restart. This requirement covers the recovery behavior; the
review outcome itself is owned by
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
- **AC-AGENTS-REVIEW-RUN-RECOVERY-001.5:** When a review pass ends because it
  exceeded its time budget, the system shall record that run `failed` with a
  reason that identifies the timeout, and shall start exactly one automatic
  retry on the next candidate of the dynamic reviewer profile, in the route's own
  order and excluding the candidate that timed out. It shall not retry when no
  other candidate remains, when the failure is not a timeout, or when the retry
  itself times out, and the retry shall be linked to the run it replaces using
  the existing run record.

## Out of scope

- The review outcome, prompt, batching, findings, and publication contracts
  owned by [Native Code Review](native-code-review.md).
- The restart-time sweep that closes runs left in flight by a stopped process.
- New background monitors, queues, metrics, configuration, or schema.
- A pass timeout proportional to the diff size. It stays a follow-up candidate:
  an observed oversized timeout was on a small diff (#266: three commits, 17
  files, about 140 changed lines), which points at reviewer exploration or model
  speed rather than diff size, so the timeout fallback on the next candidate is
  the primary remedy for that case.
