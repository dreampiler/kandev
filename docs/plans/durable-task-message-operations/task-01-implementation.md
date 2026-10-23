---
id: "01-implementation"
title: "feat(mcp): add durable task message operation readback"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-TASK-MSG-OPID-001
  - REQ-PLATFORM-TASK-MSG-OPID-002
  - REQ-PLATFORM-TASK-MSG-OPID-003
  - REQ-PLATFORM-TASK-MSG-OPID-004
acceptance_criteria:
  - AC-PLATFORM-TASK-MSG-OPID-001.1
  - AC-PLATFORM-TASK-MSG-OPID-001.2
  - AC-PLATFORM-TASK-MSG-OPID-001.3
  - AC-PLATFORM-TASK-MSG-OPID-002.1
  - AC-PLATFORM-TASK-MSG-OPID-002.2
  - AC-PLATFORM-TASK-MSG-OPID-002.3
  - AC-PLATFORM-TASK-MSG-OPID-002.4
  - AC-PLATFORM-TASK-MSG-OPID-003.1
  - AC-PLATFORM-TASK-MSG-OPID-003.2
  - AC-PLATFORM-TASK-MSG-OPID-003.3
  - AC-PLATFORM-TASK-MSG-OPID-003.4
  - AC-PLATFORM-TASK-MSG-OPID-004.1
  - AC-PLATFORM-TASK-MSG-OPID-004.2
  - AC-PLATFORM-TASK-MSG-OPID-004.3
  - AC-PLATFORM-TASK-MSG-OPID-004.4
system_design:
  - ../../specs/platform/system-design/task-message-operation-identity.md
---

# feat(mcp): add durable task message operation readback

## Outcome and scope

Persist optional message operation identity, sender-scoped readback and durable once-only replay using existing delivery and queue settlement.

Out of scope: New delivery paths, metadata authorization, unknown-outcome replay, and changing sends without operation identity.

## Implementation acceptance

1. The affected native entrypoint follows the linked requirement and design while preserving unrelated behavior and history.
2. Rejected, stale and failed operations retain their existing refusal or rollback semantics.
3. The focused implementation checks and linked documentation coverage have honest recorded results; unexecuted follow-up checks remain pending.

## Files and dependencies

The source files are the existing PR #4720 diff. This work order adds its delivery record and links existing requirements and system design; it introduces no runtime gate or permanent test.

- [System design](../../specs/platform/system-design/task-message-operation-identity.md)
- [Plan](plan.md)

## Verification and results

Validation: existing message/parent-question/tool-catalog tests and backend wiring build passed. Disposable race-enabled checks exercised actual registered MCP → native scope resolver → dispatcher → real SQLite, lost response after queue acceptance, readback/replay without duplicate delivery, conflicts/forged scope, once-only settlement, reopen and retention. Existing SQLite store/previous-stable upgrade checks passed; live PostgreSQL and provider/process E2E were not run. SQL guard, docs and all normal commit hooks passed. Existing catalog/upgrade expectations were adapted; no standing tests or CI were added.

Documentation-only correction: git diff --check and the existing .github/scripts/pr-docs.cjs validateCoverage entrypoint with the candidate changed paths and the linked candidate documents supplied on stdin. No GitHub status is synthesized locally.
