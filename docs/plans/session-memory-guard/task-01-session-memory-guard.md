---
id: "01-session-memory-guard"
title: "Hold launches under host memory pressure"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-003
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-003.1
  - AC-AGENTS-SESSION-CEILING-003.2
  - AC-AGENTS-SESSION-CEILING-003.3
  - AC-AGENTS-SESSION-CEILING-003.4
  - AC-AGENTS-SESSION-CEILING-003.5
system_design:
  - ../../specs/agents/system-design/session-memory-guard.md
---

# Task 01: Hold launches under host memory pressure

## Summary

With `features.sessionMemoryGuard` on, defer a launch that would add an agent
process while host available memory is below the floor or commit charge is
above the ceiling.

## In scope

- `internal/system/hostmemory`: `Reading`, `Gate.Pressured`, `ReadConfig`, and
  Windows, Linux, and unsupported readers.
- `sessionCeilingController.configureMemoryGate`, `memoryPressuredLocked`, and
  the `memory_pressure` refusal in `admit`, `handOffOrAdmit`, and
  `decideLocked`; the coordinator exemption; the card note text.
- Runtime flag `features.sessionMemoryGuard` /
  `KANDEV_FEATURES_SESSION_MEMORY_GUARD`, off in prod, dev, and e2e.

## Out of scope

Lanes, CPU pressure, stopping running sessions, and the count ceiling itself.

## Verification

- `go test ./internal/system/hostmemory/ -count=1`
- `go test ./internal/orchestrator/ -run 'TestSessionCeilingMemory|TestCoordinatorTaskMemoryExemption' -count=1`
- `go test ./internal/runtimeflags/ ./internal/profiles/ -count=1`
- `GOOS=linux` and `GOOS=darwin` vet of `internal/system/hostmemory`
