# Backend delivery retention handoff

This is an implementation draft, not a merge-ready change. Tasks 01–07 are complete. Task 08 remains in progress.
The user requested a draft PR on 2026-10-10 so a colleague can continue. This authorizes the handoff commit and push, superseding the earlier no-publication constraint. No acceptance limit is waived.

## Start here

Read [the manifest](plan.md), [Task 08](task-08-integration-and-storage.md), and the paired [requirements](../../specs/platform/requirements/backend-agent-delivery-retention.md) and [design](../../specs/platform/system-design/backend-agent-delivery-retention.md).
Task 08 records successful checks, failed measurements, source boundaries, and the exact final commands.

The implementation includes authenticated replay, hash/checkpoint recovery, bounded event and prompt cleanup, retirement jobs and orphan discovery, worker readiness/shutdown, configuration, telemetry, and offline maintenance. SQLite and real PostgreSQL focused race tests cover the changed behavior.

## Latest change and evidence

Sparse ownership indexes now exclude blank session IDs. Current orphan and final-existence probes explicitly state that predicate. Unknown owned incarnations and blank/mixed cursor-attributed effects retain their original deletion scope.

Final focused race GREEN passed in 17.769s: fresh databases, old two/three-column repair, interruption/cancellation rollback, reopen, frozen-reader additive index DDL, and actual generic orphan query plans. Earlier focused GREEN passed in 23.385s. SQL guard and benchmark compilation passed.
Actual frozen-reader typed recovery remains indexed. Its legacy OR admission query scans 10001 effects under both full and sparse controls; no bounded-reader claim is made for that query.

The latest sparse candidate has NOT had a full controlled latency comparison. The preceding direct-prefix candidate retained all 4500 samples per state/engine:

| Engine | Idle/baseline p95 | Active/baseline p95 | Limit |
| --- | --- | --- | --- |
| SQLite | 1.1117127270119913 | 1.3892183912981657 | <= 1.10, both failed |
| PostgreSQL | 0.8768164414816446 | 0.8857589902898558 | <= 1.10, both passed for that candidate |

All 18 commands and data-preservation assertions passed in that window. Command success does not satisfy latency acceptance. Do not round away a failure, discard samples, or reuse a preceding candidate's pass for new source.

## Remaining work, in order

1. Measure the sparse candidate with the same predeclared baseline/idle/active interleaving, production pools, matched 100000-event history, and all 4500 samples. Resolve any remaining regression before calling Task 08 complete.
2. Measure concurrent replacement/deletion. Run actual million-event 1KiB/4KiB and protected-gap fixtures on both engines, with logical/physical storage, canonical equality, reopen and maintenance evidence.
3. Run the exact three-package retention race selector with all PostgreSQL/native/backup prerequisites. Allow a 90-minute overall Go test timeout for database-scale fixtures; retain the unchanged 250ms batch, five-second pass and 100ms yield limits.
4. Repeat store conformance and the Task 08 orchestrator/managed-deletion selectors on final source.
5. Run the nine desktop tests, then seven mobile tests, through the guarded projects on a fresh native/helper/web/plugin build. Earlier smoke/static passes do not replace them.
6. Repeat real PostgreSQL backup, frozen-N retained-session rollback/new writes/deletion and forward upgrade against the final native artifact. Preserve the witness scope below.
7. Reconcile final results/statuses, inspect the diff, and signal workflow completion only after every required gate passes. Do not merge this draft while these items remain open.

If adding foreground-priority scheduling, first strengthen benchmark progress to require committed cleanup during timed samples; the existing final nonzero checkpoint can include setup. Add real authenticated Receive+Project at 20 events/s concurrently with worker passes. Current capacity tests bulk-seed arrivals and cannot prove fairness under that new scheduling policy. No priority scheduling is implemented here.

## Artifacts outside Git

The existing runner stores raw logs, samples, profiles, source backups and private scripts under `/tmp/kandev-retention-resume-20261009/`. These are local files, not PR attachments. Copy needed artifacts before removing the workspace. Private runner scripts contain the disposable DSN; do not publish them or print their contents. Use a new isolated PostgreSQL 18 instance on another machine.

Frozen Release N must remain unchanged:

- Path: `/tmp/kandev-delivery-retention-release-n-20261009/kandev`.
- SHA-256: `3df1e310220f0a7588453c062f2fa90aa0a611dd7277cc77c76dd4f8bc3a0517`.
- Version: `v0.97.0-143-gd7a44e96ed-dirty`.

This witness certifies retained-session recovery and explicit-ID manual prompt/event writes. Source inspection identified historical implicit auto-start/direct-retry preparation gaps. It does not certify every launch path; a shipping Release N needs those admission corrections. Do not replace the frozen witness with a current binary and claim an old-reader test.

The unchanged pre-retention benchmark artifact SHA-256 is `3b21db2b295ac620ab7d3f61b6e82553c72aa1008843ec5566ae0c64cf08965e`.
The sparse benchmark artifact SHA-256 is `e51980564038831fd0981a8112b888125fc5ab8d645dcaa906fe2b8ff61f89d9`.
Obtain the baseline artifact and matched fixture scripts from the existing workspace when continuing elsewhere.

Native rollback tests require `KANDEV_RETENTION_CURRENT_BINARY`, `KANDEV_RETENTION_COMPAT_BINARY`, `KANDEV_RETENTION_ROLLBACK_POSTGRES_DSN`, `KANDEV_RETENTION_POSTGRES_CONTAINER`, and `KANDEV_RETENTION_BACKUP_OUTPUT`, in addition to `KANDEV_TEST_POSTGRES_DSN`. Verify each harness prerequisite and record skips as blockers. Never print credentials.

Handoff native/helper/web/plugin build passed with `GOMAXPROCS=2 GOFLAGS='-p=2' make build-backend build-backend-linux-helpers build-web-e2e build-e2e-plugin-package`.
Native SHA-256: `818c856021b1280439043548f81e4da5a20968864d9550ea4dd77c8b46fe33f3`.
Whole-backend lint passed with zero issues using `golangci-lint run ./... --new-from-rev=d6cff57659ae5aea28e9457c587a8c9d70d0a130 --timeout=20m --allow-serial-runners`. Catalog (370 decisions/1498 specifications), full specification lint, package coverage (22/22 ACs, 68 links) and whitespace checks passed.
