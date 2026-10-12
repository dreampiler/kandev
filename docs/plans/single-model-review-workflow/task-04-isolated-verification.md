---
id: "04-isolated-verification"
title: "Isolated run_code_review runtime proof"
status: building
wave: 2
depends_on: ["01-loader-allowlist", "02-writer-owned-review-template", "03-reviewer-origin-exclusion"]
plan: "plan.md"
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-002
acceptance_criteria:
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.2
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.3
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.4
system_design:
  - ../../specs/agents/system-design/native-code-review-reviewer-exclusion.md
---

# Task 04: Isolated `run_code_review` runtime proof

## Summary

Run one actual `run_code_review` against an off-home isolated instance on this
branch and record the entrypoint, result, and limits.

## In scope

- Launch an isolated TEST instance with `scripts/dev-isolated.ps1`
  (PowerShell `-File`, isolated home/port).
- Trigger exactly one `run_code_review` and record the run row/status and the
  origin-exclusion outcome.
- Stop the instance with `scripts/kandev-kill.ps1` using the owned pidfile and
  `-Yes`.

## Out of scope

- Operating home/DB/install/shared builds/global kill.
- Any second or repeated run, or an unchanged-success rerun.

## Acceptance

- One real `run_code_review` entrypoint is exercised and its result recorded.
- The observed origin-exclusion behavior matches the contract, or the exact
  unproven limit is disclosed (`SKIP` is not `PASS`).

## Verification

```powershell
scripts/dev-isolated.ps1 -NoBuild
# trigger one run_code_review on the isolated instance, capture the run result
scripts/kandev-kill.ps1 -Pidfile <owned pidfile> -Yes
```

## Files likely touched

- None (runtime proof; results recorded in the PR body/plan).

## Dependencies

- Tasks 01–03.

## Risks

- Environment limits may prevent a full run; report actual result vs unproven.

## Results

Recorded in the task plan and PR body.
