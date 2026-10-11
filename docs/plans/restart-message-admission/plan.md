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
native resume. Operations later confirmed the running instance was the
deliberate rollback `c8bcf5e293-hotfix/start`, which predates the durable
agent session continuity work (#3598).

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

## Reported behavior vs. reproduced behavior

The reported evidence was observed on the pre-#3598 rollback. It is not
reproducible on this base:

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

No production or permanent-test change. The existing implementation satisfies
the observable result on fork main.

## Remaining inputs (not this task)

- Operations owns deployment and rollback decisions. The operational
  remediation is to run a lineage at or after #3598 rather than the
  `c8bcf5e293-hotfix/start` rollback. This task does not redeploy or mutate the
  operating instance, database, or processes.
- A pre-#3598 build genuinely lacks automatic restart recovery; that is the
  expected behavior of that lineage, not a defect on this base.

## Out of scope

- W095/#220 superseded-starting settlement (owned elsewhere).
- The AIWS watchdog recovery ladder (existing W-1010-35).
- Office and automation run projection; its park-and-surface rule is unchanged.
- New permanent integration, end-to-end, or CI coverage.
