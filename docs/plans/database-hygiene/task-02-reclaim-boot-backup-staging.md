---
id: "02-reclaim-boot-backup-staging"
title: "Reclaim abandoned boot backup staging directories"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-SQLITE-FILE-HYGIENE-002
acceptance_criteria:
  - AC-PLATFORM-SQLITE-FILE-HYGIENE-002.1
  - AC-PLATFORM-SQLITE-FILE-HYGIENE-002.2
  - AC-PLATFORM-SQLITE-FILE-HYGIENE-002.3
  - AC-PLATFORM-SQLITE-FILE-HYGIENE-002.4
system_design:
  - ../../specs/platform/system-design/sqlite-file-hygiene.md
---

# Task 02: Reclaim boot backup staging

## Summary

Add one exported sweep in `internal/persistence` for `.kandev-backup-*`
directories that have shown no modification for six hours. Call it on every
SQLite boot and from the system backups sweep.

## In scope

- `internal/persistence/snapshot.go`: the prefix constant, the six-hour
  constant, `SweepStaleBootStaging(backupDir, log)`, and the newest
  modification time over a directory and its direct entries.
- `internal/persistence/provider.go`: use the prefix constant in
  `createPreMigrationBackup`. Call the sweep once per SQLite boot when the
  backups directory exists, before the backup decision.
- `internal/system/backups/store.go`: call the sweep at the start of
  `sweepStaleTmpFiles`.
- Tests next to both packages:
  - an old staging directory is removed and a published backup beside it stays;
  - a staging directory whose child file is recent stays even when the
    directory itself is old;
  - a recent staging directory and an old `.snapshot-` directory are not
    removed by the boot sweep;
  - a boot that takes no new backup still removes an old staging directory;
  - creating a system backup removes an old staging directory.

## Out of scope

Backup retention counts, `.snapshot-` and `.tmp` rules, restore, and the
startup progress steps.

## Acceptance

- The new tests fail on the base branch (the staging directory survives) and
  pass after the change.
- A sweep failure logs a warning and never fails boot or backup creation.

## Verification

```bash
(cd apps/backend && go test -tags fts5 ./internal/persistence -run '^Test(SweepStaleBootStaging|ProvideSQLiteReclaimsBootStaging)' -count=1 -v)
(cd apps/backend && go test -tags fts5 ./internal/system/backups -run '^TestCreate_SweepsStaleBootStaging' -count=1 -v)
(cd apps/backend && go vet ./internal/persistence/... ./internal/system/backups/...)
```

## Likely files

- `apps/backend/internal/persistence/snapshot.go`
- `apps/backend/internal/persistence/provider.go`
- `apps/backend/internal/persistence/snapshot_test.go`
- `apps/backend/internal/system/backups/store.go`
- `apps/backend/internal/system/backups/store_test.go`

## Results

Not implemented yet.
