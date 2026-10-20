---
id: "01-windows-owned-job"
title: "Prove and contain Windows owned process trees"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-AGENT-RUNTIME-AVAILABILITY-002
acceptance_criteria:
  - AC-PLATFORM-AGENT-RUNTIME-AVAILABILITY-002.7
system_design:
  - ../../specs/platform/system-design/agent-runtime-availability.md
---

# Task 01: Prove and contain Windows owned process trees

## Summary

Give the agentctl launcher's kill-on-close job a kernel name, record it in the
process identity, and let Windows `OwnedSessionTerminated` and
`TerminateOwnedSession` use it once the original owner is confirmed exited.

## In scope

- `winproc.InstallNamedKillOnCloseJobForSuspendedProcess` and a named
  `createNamedKillOnCloseJob`; the unnamed entry points delegate to them.
- `processidentity.Identity.WindowsJobName`, `WindowsJobNamePrefix`, and
  `WindowsJobNameEnv`.
- Windows `Inspect` reports `StateExited` when the process has an exit time.
- Windows `Capture(os.Getpid())` records the inherited job name only after
  `IsProcessInJob` confirms membership.
- Windows `OwnedSessionTerminated`: live owner is not terminated; exited owner
  with an empty or closed owned job is terminated; reused PID, unknown state,
  missing or foreign job name, or unreadable job fail closed.
- Windows `TerminateOwnedSession` terminates and waits on the owned job after
  confirmed owner exit.
- The launcher names the job, passes it to the child, and keeps the identity
  even when capture fails (cleanup then still fails closed).

## Out of scope

Linux and macOS containment and every caller of the identity API.

## Verification

- `go test ./internal/common/processidentity/ -run 'TestWindows(ExitedOwner|OwnedSessionWithoutOwnedJob|OwnedSessionMockProcessTable|OwnedSessionAliveIsNotTerminated|ContainmentRejectsUnknownAndLiveProcesses)' -count=1`
  (no child process is started)
- Process tests that start child processes:
  `TestWindowsOwnedSessionNamedJobProvesTermination` and
  `TestWindowsNamedJobContainsOrphanAfterLeaderExit` (with its helper
  `TestWindowsOwnedJobHelper`).
- `GOOS=linux` and `GOOS=darwin` builds of the changed packages.
