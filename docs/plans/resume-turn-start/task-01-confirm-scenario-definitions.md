---
status: done
plan: plan.md
requirements:
  - REQ-AGENTS-RESUME-TURN-START-001
system_design:
  - ../../specs/agents/system-design/resume-turn-start.md
---

# Task 01: Confirm scenario definitions

## Outcome

A frozen, evidence-backed mapping between the original manual driver scenarios
(A~E) and the v09 §2.2 scenarios, so that later work cannot silently reuse a
letter for the wrong meaning.

## In scope

- Read the original manual driver, its progress log, and the captured
  `setup-*`/`after-*` output, and record what each original letter actually
  asserted and observed.
- Produce the mapping table in [plan.md](plan.md#scenario-definitions-confirm-first)
  and confirm or correct it against the evidence.

## Exclusions

- No change to `docs/plans/resume-turn-start/plan.md` semantics beyond the
  mapping table.
- No production or test code change.

## Acceptance conditions

1. Every original A~E letter has a definition, an original observed result, and a
   named v09 target.
2. Original results (A, C, D OK; B, E FAIL) are recorded without equating an
   original failure to a v09 failure.

## Verification

- `git -C <worktree> status --porcelain` is empty after the mapping is recorded
  (no code change).
- Cross-check each row against the original driver's function bodies (`after`,
  `setup`).

## Likely files

- `docs/plans/resume-turn-start/plan.md` (mapping table only)

## Dependencies

- None.

## Results

Confirmed against the original driver (`setup`, `after`) and its progress log.
The plan's mapping table is authoritative. Original A, C, D were
OK and B, E FAIL; the v09 partition differs and is not equated to the original
letters.
