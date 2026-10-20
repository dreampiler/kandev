---
id: "01-replaced-session-reclaim"
title: "Stop settled replaced session runtimes"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-TASKS-PRIMARY-SESSION-OWNERSHIP-001
acceptance_criteria:
  - AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.6
system_design:
  - ../../specs/tasks/system-design/primary-session-ownership.md
---

# Task 01: Stop settled replaced session runtimes

## Summary

With `features.replacedSessionReclaim` on, stop the execution a session held
when it lost the primary role or handed its queue to a replacement, once that
session settles.

## In scope

- `registerReplacedSessionReclaim`, `reclaimReplacedSession`,
  `reclaimReplacedSessionsOnce`, and `stopReplacedSessionIfSettled` in
  `orchestrator/replaced_session_reclaim.go`.
- `SetPrimarySession` records the previous primary under
  `primarySessionReclaimMu`; `transferQueuedSessionState` registers the donor;
  the idle-reaper tick retries registrations.
- Runtime flag `features.replacedSessionReclaim` /
  `KANDEV_FEATURES_REPLACED_SESSION_RECLAIM`, off in prod, dev, and e2e.

## Out of scope

Automatic promotion, idle suspension policy, Office and coordinator lifecycle,
and queue routing.

## Verification

- `go test ./internal/orchestrator/ -run 'TestPrimaryReplacement|TestQueueReplacementStopsSettledDemotedExecution' -count=1`
- `go test ./internal/runtimeflags/ ./internal/profiles/ -count=1`
