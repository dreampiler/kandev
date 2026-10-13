---
id: "03-windows-idle-reclaim"
title: "Windows ownership and replaced-session teardown"
status: done
wave: 3
depends_on:
  - "01-terminal-reclaim"
plan: "plan.md"
requirements:
  - REQ-EXECUTORS-RUNTIME-RECLAIM-005
acceptance_criteria:
  - AC-EXECUTORS-RUNTIME-RECLAIM-005.1
  - AC-EXECUTORS-RUNTIME-RECLAIM-005.2
  - AC-EXECUTORS-RUNTIME-RECLAIM-005.3
  - AC-EXECUTORS-RUNTIME-RECLAIM-005.4
  - AC-EXECUTORS-RUNTIME-RECLAIM-005.5
system_design:
  - ../../specs/executors/system-design/agent-runtime-reclamation.md
---

# WO-03: Windows ownership and replaced-session teardown

## Scope and authorization

The operator delegated implementation, Windows unit tests, fork PR merge, and
worktree cleanup on 2026-10-13. This work order records that execution request.
Keep the configured 60-minute timeout and ownership protection. Production DB
and logs are read-only. No runtime build, swap, API mutation, or upstream write.

## Confirmed causes

- Windows `OwnedSessionTerminated` always returned unsupported. Its strict
  behavior came from interrupted-session recovery (#4380), preserving the rule
  that process death alone cannot prove descendant death.
- Windows `Inspect` checked only creation time; an exited process with an open
  handle appeared alive.
- Primary promotion and queue transfer did not stop a settled demoted execution.
- The idle parking skip count includes stopped historical rows. It does not
  invoke the Windows termination-proof function. No timeout change is justified.

## Owned files and implementation

- `internal/common/processidentity/`: creation/exit time and exact named-job proof.
- `internal/common/winproc/lifecycle_windows.go`: name the existing suspended-start job.
- `internal/agent/runtime/agentctl/launcher/`: publish and retain the launcher's job identity.
- `internal/orchestrator/replaced_session_reclaim.go`: normal stop admission and maintenance retry.
- Orchestrator primary promotion, queue transfer, and existing maintenance tick wiring.

## Acceptance and verification

1. Windows mock process-table and real-job tests prove ownership, surviving
   descendants, termination, and protection of unknown/live/reused processes.
2. Reaper tests preserve 60 minutes, reclaim past it, and reject unknown identity.
3. Replacement tests prove direct primary and queue paths, active-turn retry,
   rotation protection, and coordinator ownership protection.

```text
go test -trimpath -race ./internal/common/processidentity ./internal/common/winproc ./internal/agent/runtime/agentctl/launcher -count=1
go test -trimpath -race ./internal/orchestrator -run "TestPrimaryReplacement|TestQueueReplacement|TestIdleReaper|TestIdleParking|TestSetPrimarySession|TestTransferQueuedSessionState|TestClassifyIdleReclaim|TestReclaimIdleSession" -count=1
```

## Results

- RED: live-owner and empty named-job Windows proof returned
  `ErrUnverifiableIdentity` before correction.
- RED: the valid primary replacement fixture failed against the original
  `origin/main` handler through a Go source overlay: the old execution was not stopped.
- Baseline `15c11a2a54`: full orchestrator fails the two passthrough attachment
  tests, `TestHandleAgentReady_PassthroughAttachmentOnlyOrdinaryMessageUsesAttachmentAwarePrompt`
  and `TestHandleAgentReady_PassthroughQueuedAttachmentUsesAttachmentAwarePrompt`.
- GREEN: all three Windows ownership packages pass with `-trimpath -race`.
- GREEN: the focused orchestrator command above passes with `-trimpath -race`,
  including the configured 60-minute boundary, unknown execution, active-turn
  retry, rotation, re-promotion, coordinator protection, and caller-held lock.
- An added failing caller-held lifecycle-lock test exposed reentrant locking;
  nonblocking cleanup and a separate primary-mutation mutex corrected it.
- Specification and catalog checks pass. Public documentation validator tests
  pass (63 cases); the actual validator reports the same pre-existing missing
  `report_to_handoff_source_kandev` coverage entry on unchanged `origin/main`.

## Risks and exclusions

Legacy Windows process identities lack a job name and remain fail-closed. The
operator's next runtime build is needed before production gets named identities.
Provisional active-turn replacement claims are in memory; restart uses existing
startup reconciliation and idle policy. No per-session memory cap or new policy.
