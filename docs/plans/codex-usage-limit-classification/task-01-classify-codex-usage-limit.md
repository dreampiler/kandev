---
id: "01-classify-codex-usage-limit"
title: "Classify codex usage-limit notices"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-CODEX-USAGE-LIMIT-001
acceptance_criteria:
  - AC-AGENTS-CODEX-USAGE-LIMIT-001.1
  - AC-AGENTS-CODEX-USAGE-LIMIT-001.2
  - AC-AGENTS-CODEX-USAGE-LIMIT-001.3
system_design:
  - ../../specs/agents/system-design/codex-usage-limit-classification.md
---

# Task 01: Classify codex usage-limit notices

## Summary

Recognize the codex usage-limit notice in both apostrophe forms, derive the
retry time from the notice when no structured hint is present, and recognize
the notice during manual recovery.

## In scope

- Extend the codex quota rule with the plain usage-limit notice pattern.
- Add a reset-hint text parser and apply it in `Classify` for quota and rate
  classifications without a structured hint.
- Normalize the typographic apostrophe in the orchestrator manual-recovery
  check.
- Add focused classification, dynamic-routing, and manual-recovery tests.

## Out of scope

- Changing rules for providers other than codex-acp.
- Changing candidate ordering, circuit behavior, or provider selection.
- Web changes, database migrations, or new provider contracts.

## Acceptance

- A codex-acp notice with a straight or typographic apostrophe classifies as
  `quota_limited` with high confidence and allows fallback.
- A quota or rate classification without a structured reset hint carries the
  retry time parsed from the notice, and a structured hint takes precedence.
- Manual recovery recognizes the typographic-apostrophe notice and retains the
  queued prompt.

## Verification

```bash
make -C apps/backend fmt
go test ./internal/agent/runtime/routingerr ./internal/agent/runtime/dynamic ./internal/orchestrator -count=1
make -C apps/backend lint
python3 scripts/lint-spec-files.py --all
python3 scripts/list-docs.py validate
git diff --check
```

Run the Go tests from `apps/backend`.

## Files likely touched

- `apps/backend/internal/agent/runtime/routingerr/rules.go`
- `apps/backend/internal/agent/runtime/routingerr/resethint.go`
- `apps/backend/internal/agent/runtime/routingerr/routingerr.go`
- `apps/backend/internal/agent/runtime/routingerr/classify_test.go`
- `apps/backend/internal/agent/runtime/dynamic/engine_test.go`
- `apps/backend/internal/orchestrator/task_operations.go`
- `apps/backend/internal/orchestrator/task_operations_manual_recovery_test.go`

## Dependencies

None.

## Risks

- The reset timestamp is a local wall-clock value; parsing must fail closed.
- A structured reset hint must never be overwritten by text parsing.

## Parallelism

`sequential`

## Inputs

- `REQ-AGENTS-CODEX-USAGE-LIMIT-001` and all acceptance criteria.
- The codex usage-limit classification system design.

## Results

Pending implementation.
