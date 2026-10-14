---
id: task-01
title: Remove verified upstream duplicates
status: done
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-RELEASE-FORK-OVERLAY-PATCH-SET-003
acceptance_criteria:
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-003.1
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-003.2
  - AC-RELEASE-FORK-OVERLAY-PATCH-SET-003.3
system_design: ../../specs/release/system-design/fork-overlay-patch-set.md
---

# Task 01: Remove verified upstream duplicates

## Outcome

Fork changes that upstream already provides are removed before the patch
cut. Candidates that are not proven equivalent stay, with the reason recorded.

## In scope

- Re-check each inventory candidate against the current tree and the upstream
  base `51af93dcd86b`.
- Remove only fork code whose behavior or checks upstream already covers.

## Exclusions

- Fork production behavior without a proven upstream equivalent.
- Candidates waiting for an upstream decision: #4406 P46
  (`durableIdleActivityAnchor` against `latestIdleActivity`), #239 and #288
  (after #4448), and D01 with #208 (after #4045).

## Acceptance conditions

1. Each removal names the upstream source that covers it.
2. Each retained candidate records why it is retained.
3. The changed package's tests pass once.

## Verification

```sh
cd apps/backend && go test ./internal/system/backups/
```

## Likely files

- `apps/backend/internal/system/backups/retention_test.go`

## Results

Inventory inputs (proposal sections 3-3 and 3-4) were re-checked on
2026-10-15:

| Candidate | Current tree | Decision |
| --- | --- | --- |
| 23 upstream-identical commits | No remaining tree difference | Nothing to remove |
| #4414 (upstream `4a1041ee77`) | Included in fork `main`; no remaining fork hunk | Nothing to remove; no replacement PR is claimed |
| A12 `331aba6b3e`: `TestVerifySnapshotAcceptsValidDatabase` (24 lines) | Upstream #4126 `TestVerifySnapshotOpensNativeAbsolutePath` in `verify_snapshot_test.go` opens native absolute Windows paths and checks the full digest, covering this test | **Removed** |
| A12: Windows skip in `TestManualBackupIsPrivate` | Upstream still asserts POSIX `0600` on every OS | Retained: fork-only Windows fix |
| A10 `16646dcf64`: launcher and shell `CREATE_NO_WINDOW` tests (21 lines) | The fork production flag in `launcher/process_group_windows.go` and `shell/process_group_windows.go` is absent upstream | Retained: guards retained fork behavior |
| A7 `936817d8ca`: `TestReclaimIdleSessionSkipsTokenlessSession` | Upstream `TestReclaimIdleSessionRowOutcomeByResumeToken` covers the kept row; the fork test also asserts a dead liveness probe and PID retention | Retained: equivalence not established |
| 10 duplicate or cleanup commits | No separate tree content | Nothing to remove |

Verification: `go test ./internal/system/backups/` passed once
(`ok github.com/kandev/kandev/internal/system/backups 1.707s`).
