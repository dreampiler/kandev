---
created: 2026-10-11
status: implemented
requirements:
  - REQ-PLATFORM-DURABLE-AGENT-DELIVERY-006
  - REQ-PLATFORM-DURABLE-AGENT-DELIVERY-008
  - REQ-AGENTS-HARNESS-SESSION-CONTINUITY-001
  - REQ-AGENTS-HARNESS-SESSION-CONTINUITY-003
system_design:
  - ../../specs/platform/system-design/durable-agent-delivery.md
  - ../../specs/agents/system-design/harness-session-continuity.md
legacy_specs: []
---

# Implementation plan: restart message admission for execution-less sessions

## Overview

An operator reported that after a backend restart, a message to an
execution-less eligible session did not start a turn: the message was stored
but no turn ran for more than two minutes, the session showed a `starting`
route state, and recovery reported `unresolved_durable_work` / an unavailable
native resume. The incident-time build is unconfirmed: the incident was
reported at 16:53:17 KST, before the 17:16 rollback and the 17:18:41 restart.
The later running instance (present after 17:18:41 KST) is a deliberate
pre-#3598 rollback (`c8bcf5e293-hotfix/start`), but a later instance's build
does not establish the incident's build or cause.

On fork main (base `6a6d3d76df`, which contains #3598 plus #4380, #4384, and
#4403) the required behavior is already implemented and regression-tested, so
this package reuses the existing fix instead of adding another. It records the
comparison and a verification against the existing acceptance criteria rather
than a new requirement.

## Required behavior (already specified)

The behavior is owned by existing criteria and needs no new requirement:

- [REQ-PLATFORM-DURABLE-AGENT-DELIVERY-006](../../specs/platform/requirements/durable-agent-delivery.md),
  AC-006.20 through AC-006.24: after backend restart, eligible interrupted
  sessions reconcile automatically without browser interaction; a terminated
  process reloads the same saved native conversation without sending a prompt;
  repeated recovery reuses durable progress without duplicate dispatch.
- AC-008.6: after restoration, restoration follows AC-006.20 through
  AC-006.24, and new instructions use ordinary admission after restoration.
- [REQ-AGENTS-HARNESS-SESSION-CONTINUITY-001](../../specs/agents/requirements/harness-session-continuity.md)
  and -003: prefer native resume; on a typed native-unavailable result return
  the explicit continuation action; retain the previous native identifier and
  block prompt dispatch until recovery succeeds.

## Reported behavior vs. behavior on this base

The incident-time build is unconfirmed and the incident was not reproduced on
any lineage (see "Unproved by this package" below). The table records only what
this base does; it is not evidence of the incident's cause:

| Claim (report-only) | Finding on this base |
| --- | --- |
| message stored with no turn for more than 2 minutes | automatic restart recovery restores the interrupted native conversation, then ordinary admission dispatches one turn |
| `route_state` `starting` with no live execution | a resumable paused row is repaired in place (never pruned), so cold resume finds a durable row instead of the "no executor record" branch |
| resume `unresolved_durable_work` | an open durable block still blocks admission and surfaces the explicit `continue_from_history` action; it is not discarded |
| `continue_from_history` loses native identity | native-first is the default; context continuation stays an explicit action under the typed native-unavailable condition |

The reported claim that every `starting` session is orphaned is not supported
by this base: the resume-safety invariant repairs rather than prunes a
resumable/dead row (`pruneOrRepairExecutorRow`, `RowMustBePreserved`), so a
message reaches the durable row.

## Unproved by this package

These items from the original request were not performed or not established,
and are reported rather than dropped:

- Incident-time build: unconfirmed. The 16:53:17 KST incident preceded the
  17:16 rollback and the 17:18:41 restart, so the later confirmed pre-#3598
  running instance cannot prove the incident's build or cause.
- Actual end-to-end real-entry outcome: attempted, not completed. This session
  built the base binaries (`make -C apps/backend build`: `kandev.exe`,
  `agentctl.exe`, `mock-agent.exe`) and launched an isolated real backend on
  `127.0.0.1:48931` with an isolated home and DB under the system temp root
  (dev mode plus mock agent), intending to drive the real WebSocket
  `task.message.add` entry and assert exactly one turn. The attempt reached
  backend startup; first-boot agent discovery is slow, and the run was aborted
  before the message entry completed, so no accepted-message-to-turn result
  exists. `scripts/dev-isolated.ps1` could not be used because its safety guard
  treats this worktree path (a Kandev task worktree) as an
  operational Kandev home.
  Impact: the real-entry outcome remains unproved; the base-side service-level
  evidence stands.
  Resume condition and next actor: a follow-on run that starts the isolated
  backend with an equivalent env (own home, DB, and port), waits for `/ready`,
  creates a task and session through a real entry, and sends `task.message.add`
  over WS to assert one turn. No operating DB, process, or credential is used.
- Two-lineage WO-01 baseline comparison: not performed. No execution on the
  pre-#3598 lineage was run, so no incident reproduction exists on lineage (a).

Source and next actor: this author session for the service-level evidence and
the recorded attempt; a follow-on task with an isolated real backend on both
lineages for the real-entry and two-lineage outcomes. The original work order
remains open for those items.

## Verification

Run from `apps/backend` on base `6a6d3d76df` (see the work order for exact
commands and results). The decisive regression is
`TestRestartRecoveryRestoresNativeConversationWithoutPrompt`
(`internal/orchestrator/interrupted_session_restart_test.go`), which starts a
real orchestrator over an on-disk SQLite repository, restores an interrupted
session on startup, asserts no prompt and no turn is written during recovery,
and then asserts that an ordinary `PromptTask` follow-up dispatches to the
restored native conversation. Admission/queue/resume behavior is covered by
`TestForwardMessageAsPrompt_*` (`internal/task/handlers`) and
`TestPromptTask_*` (`internal/orchestrator`).

All listed tests pass on this base. No production change is required, so this
package contains documentation only.

## Work orders

- [x] [Task 01: Verify restart message admission on base](task-01-verify-restart-admission.md)

## Outcome

No production or permanent-test change. On this base the existing
implementation satisfies the restored-conversation-then-ordinary-admission
path covered by the focused tests. The incident-time build and the actual
real-entry and two-lineage outcomes remain unproved (see "Unproved by this
package").

## Remaining inputs (not this task)

- The operator deliberately runs the old `c8` lineage during its own mock-agent
  upstream/fork causal bisect. This package does not request a redeploy,
  replacement, or any override of that choice; whether to run a lineage at or
  after #3598 is the operator's decision. This task does not redeploy or mutate
  the operating instance, database, or processes.
- A pre-#3598 build genuinely lacks automatic restart recovery; that is the
  expected behavior of that lineage, not a defect on this base.

## Out of scope

- Superseded-starting settlement (owned elsewhere).
- The watchdog recovery ladder.
- Office and automation run projection; its park-and-surface rule is unchanged.
- New permanent integration, end-to-end, or CI coverage.
