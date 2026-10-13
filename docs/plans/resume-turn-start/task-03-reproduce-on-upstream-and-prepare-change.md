---
status: done
plan: plan.md
requirements:
  - REQ-AGENTS-RESUME-TURN-START-001
system_design:
  - ../../specs/agents/system-design/resume-turn-start.md
---

# Task 03: Reproduce on upstream and prepare the change

## Outcome

A reproduction of the remaining restart-resume defect on the fork's current
upstream baseline, and a qualified, reviewable change with a PR body, ready for
the upstream write owner. No upstream publication happens here.

## In scope

- Reproduce the defect on the current baseline in an isolated instance using the
  fork's own worktree and the frozen scenario mapping.
- Identify the root cause in the resume, restart reconciliation, route, or
  delivery path, and change only what the cause requires.
- Add companion tests beside the changed Go sources for the changed behavior.
- Write the PR title and body (purpose, change, verification, and the
  `unverified` real-provider limits) to a file for the write owner.

## Exclusions

- No upstream push, PR, issue, or comment.
- No operating swap, home, installation, or `127.0.0.1:38430` change.
- No new recurring test, CI change, gate, or canary.
- No reimplementation of an installed change from task 02.
- No fix for a defect that no longer reproduces; report it instead.

## Acceptance conditions

1. The defect is reproduced on the current baseline, or a documented failure to
   reproduce narrows the change.
2. The change starts a real turn (new execution plus first event) or reports an
   explicit failure for the affected scenario, proven by the companion test and
   the isolated run.
3. The PR body file exists and lists the covered and uncovered behavior.

## Verification

- `go test ./apps/backend/internal/orchestrator/... -run 'Resume|Restart'` from
  `apps/backend`, plus `go build ./...` and `go vet ./...` for changed packages.
- Isolated run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-release.ps1 -Commit <rev> -ReleaseRoot <root> -InstanceKind isolated`.

## Likely files

- `apps/backend/internal/orchestrator/` (resume, restart reconciliation, route)
- `apps/backend/internal/agent/runtime/agentctl/` (journal, event processor)
- `<pr-body>.md` in an untracked location

## Dependencies

- Task 02 (reconciliation), task 01 (mapping).

## Results

Not reproduced on the current baseline. `scripts/verify-release.ps1` reports A,
B, C, and E passed on `origin/main` `1445dc6983`; the historical failure (a
resume that reported success without output, and a dynamic route stuck pending)
is covered by the installed fixes, so no source change is made. The RED evidence
remains the recorded manual reproduction run (B, E FAIL) taken before
those fixes. This is a no-change result: the K1 fix is already on the fork
baseline, and the remaining deliverable is the release-confidence mechanism
(task 04) and this package.

Caveat: the baseline check came from the guard-bypassing run recorded in task 04
and is preliminary until it is rerun from the operationally designated location.
