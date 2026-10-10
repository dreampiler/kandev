---
id: "01-admit-focus-resume-as-automatic"
title: "Admit focus resume as automatic"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
acceptance_criteria:
  - AC-AGENTS-SESSION-CEILING-001.1
system_design:
  - ../../specs/agents/system-design/session-concurrency-ceiling.md
---

# Task 01: Admit focus resume as automatic

## Summary

Admit a focus-driven idle-suspension resume against the session ceiling as an
automatic launch, so passive focus never uses a manual override.

## In scope

- Add a regression test for focus with a saturated ceiling before the change.
- Change the focus resume origin from manual to automatic.
- Record the focus origin in the session ceiling design.

## Out of scope

Explicit Resume, message delivery, ceiling configuration, and UI changes.

## Acceptance

- With the ceiling full, focusing an idle-suspended session launches no agent.
- Existing focus and idle-suspension tests pass.

## Verification

From the repository root:

```bash
(cd apps/backend && go test ./internal/orchestrator -run 'TestFocusTaskSession|IdleSuspension|IdleSession' -count=1)
(cd apps/backend && go vet ./internal/orchestrator)
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

## Files likely touched

- apps/backend/internal/orchestrator/idle_session_focus.go
- apps/backend/internal/orchestrator/idle_session_focus_ceiling_test.go
- docs/specs/agents/system-design/session-concurrency-ceiling.md

## Dependencies

None.

## Risks

A refused focus resume waits for capacity through the existing deferral path.

## Parallelism

`sequential`

## Inputs

- Issue #4321.
- REQ-AGENTS-SESSION-CEILING-001 and its automatic and manual admission criteria.
- The linked session ceiling design.

## Results

- RED: the new test failed on the original code with one agent launched past
  the ceiling.
- GREEN: the new test and the existing focus and idle-suspension tests pass.
- `go vet` and `golangci-lint` on the changed package report no issues.
