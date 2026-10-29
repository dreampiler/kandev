---
created: 2026-10-29
status: in_progress
requirements:
  - REQ-PLATFORM-SQLITE-READER-POOL-001
system_design:
  - ../../specs/platform/system-design/sqlite-reader-pool.md
legacy_specs: []
---

# SQLite reader pool startup override plan

## Root cause

The SQLite reader pool is fixed at four connections. A local install with many
concurrent interactive reads can saturate the pool even though WAL mode permits
more readers. Raising the default changes behavior for every install, while an
unvalidated environment-only override would bypass the startup configuration
contract.

## Scope and approach

Keep the default at four and add a startup-only, opt-in reader-pool size through
the existing typed configuration catalog. Validate the value strictly, apply it
to both maximum open and idle reader connections, and leave the writer pool,
required-store probes, and failure semantics unchanged.

## Work orders

- [ ] [01: Startup reader override](task-01-startup-reader-override.md)

## Verification

Run from `apps/backend` unless stated otherwise.

- `go test -trimpath ./internal/db ./internal/common/config ./internal/persistence -count=1`
- `go test -trimpath ./internal/persistence/requiredstores -count=1`
- `go build -trimpath ./internal/db ./internal/persistence ./internal/common/config`
- `gofmt -l` on changed Go files
- `bash scripts/lint-go-changed <changed-go-files>` from the repo root when available
- `git diff --check`
- Local `pr-docs.cjs` `validateCoverage` over the changed paths and linked work order

## Exclusions

Probe timeout and hysteresis changes, reader-contention classification, a
raised default, runtime overrides, PostgreSQL sizing changes, schema changes,
and operational rollout are out of scope.

## Risks

A validation or wiring mistake could break startup for every SQLite install.
Tests therefore pin the default, both override sources, the accepted boundaries,
invalid-value rejection, the actual open and idle counts, and the unchanged
writer size. The setting is an operator opt-in, not a substitute for fixing
overload causes.

## Results

Implemented `database.sqliteReaderConns` and `KANDEV_SQLITE_READER_CONNS` with a
default of four and an accepted range of one through 64. The value is applied to
both reader maximum open and idle connections through
`OpenSQLiteReaderWithConns`; the writer pool and required-store health behavior
are unchanged. See the work order for verification receipts.

## Unresolved contract

The P50b probe policy remains deliberately unresolved: probe timeout,
consecutive-failure hysteresis, and reader-contention classification are not
part of this change and require the existing maintainer direction.
