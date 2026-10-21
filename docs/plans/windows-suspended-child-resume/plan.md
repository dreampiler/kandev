---
status: done
created: 2026-10-20
requirements:
  - REQ-PLATFORM-WINDOWS-SUSPENDED-RESUME-001
system_design:
  - ../../specs/platform/system-design/windows-suspended-child-resume.md
---

# Windows suspended child resume

## Overview

Resume Windows suspended starts through the child's process handle instead of a
system-wide thread snapshot.

## Evidence and root cause

A CPU profile of an isolated agentctl spent 31.7% of its CPU time in
`winproc.ResumeSuspendedProcess`. The function called
`CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD, 0)`, which copies every thread on
the system, and walked the snapshot with `Thread32Next` to find the child's
threads. Every managed Git command and agentctl-managed process paid that cost.

## Technical approach

Open the child with `PROCESS_SUSPEND_RESUME` and call `NtResumeProcess`. Keep the
function's signature, so every caller and the Job Object ordering stay
unchanged.

## Delivery order

1. [Task 01: Resume suspended children through the process handle](task-01-resume-through-process-handle.md)

## Risks

- **Undocumented export.** `NtResumeProcess` is not documented Win32 API. A
  missing export fails the start, and the owner terminates the child.
- **Exited child.** The snapshot walk failed when the child had no threads.
  `NtResumeProcess` succeeds on a process that has already exited, and the owner
  observes the exit through its normal wait.
