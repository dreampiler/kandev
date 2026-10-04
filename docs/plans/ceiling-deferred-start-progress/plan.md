---
created: 2026-10-05
status: completed
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
  - REQ-AGENTS-SESSION-CEILING-003
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
legacy_specs: []
---

# Deferred launch progress

New automatic starts could take free capacity before an already deferred
`start_created` destination. Replay also returned silently when entry validation
was unavailable, and periodic pacing plus tick alignment could exceed five
minutes. The exact pre-recovery disposition of the historical operating incident
cannot be reconstructed from the retained record.

Keep arbitration in the existing admission controller, preserve claim and
workflow-entry ownership, and record pre-admission pause reasons. Reuse the
existing automation-run binding implementation. No schema, settings, frontend,
or operational data changes are needed.

| Work order | Scope | Status |
| --- | --- | --- |
| [Task 01](task-01-restore-deferred-progress.md) | Reproduce priority failure, preserve ordered admission, expose retry pauses, verify bounded recovery | done |

The implementation uses existing tests and temporary Go overlays, without adding
permanent test cases. Its isolated SQLite service reproduction exercises the
actual `StartCreatedSession` and sweep entrypoints; only the agent process is a
fake. This replaces a redundant full HTTP-server reproduction for this internal
admission change. Production replacement belongs to the operator.

After deployment, read the replay pause/admission logs and confirm that a natural
capacity release admits the waiting destination before later automatic work.
The separate natural automation-replay session/turn binding and settlement check
remains unverified and is not proved by the isolated reproduction.
