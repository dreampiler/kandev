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

Run from `apps/backend` unless stated otherwise. New-behavior checks ran from a
temporary Go overlay outside the repository's permanent test tree, then the
temporary files were removed.

- One-off overlay: `go test -trimpath -overlay <temp overlay> ./internal/db ./internal/common/config ./internal/persistence -run 'TestP50' -count=1 -v`
- Existing suites: `go test -trimpath ./internal/db -count=1` and `go test -trimpath ./internal/persistence/requiredstores -count=1`
- Configuration catalog consistency: `go test -trimpath ./internal/common/config -run 'TestConfigurationCatalog' -count=1 -v`
- `go build -trimpath ./internal/db ./internal/persistence ./internal/common/config`
- `go vet -trimpath ./internal/db ./internal/common/config ./internal/persistence`
- `bash scripts/lint-go-changed <changed-go-files>` from the repo root when available
- `git diff --check`
- Local `pr-docs.cjs` `validateCoverage` over the changed paths and linked work order

## Exclusions

Probe timeout and hysteresis changes, reader-contention classification, a
raised default, runtime overrides, PostgreSQL sizing changes, schema changes,
and operational rollout are out of scope.

## Risks

A validation or wiring mistake could break startup for every SQLite install.
The one-off checks therefore pin the default, both override sources, the
accepted boundaries, invalid-value rejection, the actual open and idle counts,
the unchanged writer size, and PostgreSQL startup with an invalid SQLite-only
value. The setting is an operator opt-in, not a substitute for fixing overload
causes.

## Results

Implemented `database.sqliteReaderConns` and `KANDEV_SQLITE_READER_CONNS` with a
default of four and an accepted range of one through 64. The value is applied to
both reader maximum open and idle connections through
`OpenSQLiteReaderWithConns`; the writer pool and required-store health behavior
are unchanged. Strict parsing and range validation are driver-scoped, so an
invalid SQLite-only value does not reject a PostgreSQL startup.

Verification used one-off Go overlay tests outside the permanent test tree.
They covered the default, YAML and environment overrides, environment-over-YAML
precedence, boundaries, invalid values, actual open and idle counts,
persistence wiring, and PostgreSQL startup with invalid YAML and environment
values. The temporary files and overlay were removed after the run. No new
permanent behavior test was added.

The catalog's audited environment inventory line for
`KANDEV_SQLITE_READER_CONNS` remains in `catalog_test.go`. It is the minimum
consistency update for a catalog that already requires every stable environment
variable; it is not a new behavior test. A permanent companion test for this
feature would need a separate narrow proposal and explicit approval, and is
recorded as an unresolved follow-up rather than assumed.

## Unresolved contract

The P50b probe policy remains deliberately unresolved: probe timeout,
consecutive-failure hysteresis, and reader-contention classification are not
part of this change and require the existing maintainer direction. A permanent
upstream companion test for the SQLite reader override, if maintainers require
one, is also a separate proposal.
