---
id: "03-record-retention-preparation-failure"
title: "Record why a tool-payload retention backup preparation failed"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-SYSTEM-PAGE-TOOL-PAYLOAD-RETENTION-002
acceptance_criteria:
  - AC-SYSTEM-PAGE-TOOL-PAYLOAD-RETENTION-002.9
system_design:
  - ../../specs/system-page/system-design/tool-payload-retention.md
---

# Task 03: Record retention preparation failure

## Summary

When backup preparation fails, log the error through a new `Options.Log`
callback and keep a bounded copy in the persisted retention record. The status
response keeps returning only `backup_failed`.

## In scope

- `internal/system/toolretention/service.go`: `Options.Log` and a nil-safe
  `logFailure` helper. Clear the stored message on disable and on a new pending
  preparation in `Save`.
- `internal/system/toolretention/types.go`: `record.PreparationDetail`
  (`preparation_detail`), outside the embedded `Status`.
- `internal/system/toolretention/preparation.go`: set and log the message in
  `finishPreparation`. Cut it to 1,024 bytes without splitting a UTF-8
  sequence. Clear it on success and in `failPreparation`.
- `internal/system/system.go`: wire `Options.Log` to the backend logger at
  error level.
- Tests in `internal/system/toolretention/preparation_test.go`:
  - a failing backup is logged, the stored record keeps the message, and the
    status shows only `backup_failed`;
  - the cut keeps at most 1,024 bytes and valid UTF-8;
  - a later disable clears the stored message.

## Out of scope

UI, API response fields, scheduler failure counting, and the backup
verification path.

## Acceptance

- The new tests fail on the base branch (no log call, no stored message) and
  pass after the change.
- `GET` status output is unchanged for a failed preparation.

## Verification

```bash
(cd apps/backend && go test -tags fts5 ./internal/system/toolretention -run '^Test(PreparationFailureRecordsCause|LimitPreparationDetail|DisableClearsPreparationDetail)' -count=1 -v)
(cd apps/backend && go vet ./internal/system/toolretention/... ./internal/system)
(cd apps/backend && go build ./...)
```

## Likely files

- `apps/backend/internal/system/toolretention/service.go`
- `apps/backend/internal/system/toolretention/types.go`
- `apps/backend/internal/system/toolretention/preparation.go`
- `apps/backend/internal/system/toolretention/preparation_test.go`
- `apps/backend/internal/system/system.go`

## Results

Not implemented yet.
