---
id: "01-drop-invalid-launch-replays"
title: "Drop invalid launch replays"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
  - REQ-OFFICE-SCHEDULER-001
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-001.5
  - AC-AGENTS-SESSION-CEILING-001.11
  - AC-OFFICE-SCHEDULER-001.15
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
  - ../../specs/office/system-design/scheduler-02.md
---

# Task 01: Drop invalid launch replays

## Summary

Stop the ceiling sweep and the Office run retry from relaunching a command that
failed validation, and make the sweep's failure warning name the cause.

## In scope

- Add `ceilingReplayResult` (outcome, error, detail) and return it from
  `replayCeilingDeferral`, `loadCeilingReplayTask`, and each per-kind replay.
- Log the detail and error when `settleCeilingReplay` preserves a non-ceiling
  failure.
- Classify `validate agent command:` and `validate continue command:` as a
  permanent failure. `settleCeilingReplay` drops it with
  `ceiling_dropped_launch_invalid`.
- Escalate an Office run at once in `HandleRunFailure` for the same error
  family.

## Out of scope

- Other non-ceiling failures, which keep the existing retry.
- Backoff for preserved records and queue ordering.

## Acceptance

- A sweep that replays a deferred start whose launch fails command validation
  clears the record and does not launch again on the next sweep.
- A transient dispatch failure is still preserved, and its warning carries the
  error and detail.
- An Office run whose launch fails command validation escalates without a
  scheduled retry.

## Verification

```bash
cd apps/backend && go test ./internal/orchestrator/ -run 'CeilingReplay|SettleCeilingReplay|AutomationCeiling|CeilingBootDeadlock'
cd apps/backend && go test ./internal/office/service/ -run 'HandleRunFailure|RetryValidation'
```

## Likely files

- `apps/backend/internal/orchestrator/ceiling_replay.go`
- `apps/backend/internal/orchestrator/ceiling_replay_result.go` (new)
- `apps/backend/internal/orchestrator/session_ceiling.go`
- `apps/backend/internal/office/service/retry.go`
- Tests beside those files, plus the existing replay tests that read the
  outcome type.

## Results

Not implemented yet.
