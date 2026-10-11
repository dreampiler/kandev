---
id: "02-batch-comparison-refs"
title: "Resolve comparison refs in one Git process"
status: planned
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-GIT-CAPTURE-RECOVERY-001
acceptance_criteria:
  - AC-PLATFORM-GIT-CAPTURE-RECOVERY-001.7
  - AC-PLATFORM-GIT-CAPTURE-RECOVERY-001.2
system_design:
  - ../../specs/platform/system-design/workspace-git-capture-recovery.md
---

# Task 02: Resolve comparison refs in one Git process

## Summary

Each comparison evidence read resolves its distinct safe references with one
`git rev-parse` process and falls back to the existing per-reference path
whenever the batch cannot be mapped exactly.

## In scope

1. Add tests in `agentctl/server/process` before the change, using a real
   temporary repository and the existing `SetGitEnvironment` + `GIT_TRACE`
   pattern to count `rev-parse` invocations:
   - With base, ahead/behind, remote-head, and comparison references that all
     resolve, one evidence read runs one `rev-parse` (baseline: four).
   - A missing reference yields the same values as separate resolution.
   - Empty and unsafe references stay empty and never reach Git.
   - An object-ID-shaped reference that does not exist stays empty.
   - Duplicate references resolve once and map to every field.
2. Extract the existing allowlist into `safeGitRefForOID`, keep `gitRefOID`
   behavior, and add a batch resolver used by
   `captureGitStatusComparisonEvidence`. Accept a batch only on zero exit with
   exactly one object-ID line per argument; otherwise use per-reference
   resolution. A cancelled context returns empty values without the fallback.
3. Keep the evidence struct, comparison, and error contract unchanged.

## Out of scope

Other `rev-parse` callers, branch identity reads, enrichment, admission class,
command deadlines, and the corrective capture policy.

## Acceptance

- The process-count test fails on the baseline and passes after the change;
  value-equivalence tests pass on both.
- Existing capture recovery tests still pass.

## Verification

```bash
(cd apps/backend && go test -trimpath ./internal/agentctl/server/process -run '^TestGitStatusComparisonEvidence' -count=1)
(cd apps/backend && go test -trimpath ./internal/agentctl/server/process -run '^TestWorkspaceTrackerGitStatusCaptureRecovery' -count=1)
(cd apps/backend && go build ./... && go vet ./internal/agentctl/server/process/...)
```

## Files likely touched

- `apps/backend/internal/agentctl/server/process/workspace_git_status_capture.go`
- `apps/backend/internal/agentctl/server/process/workspace_git_status_comparison_evidence_test.go`

## Dependencies

None.

## Risks

`rev-parse` without `--verify` echoes unresolvable arguments and stops at the
first failure; the exact-mapping rule and fallback keep values identical.

## Results

Pending implementation.
