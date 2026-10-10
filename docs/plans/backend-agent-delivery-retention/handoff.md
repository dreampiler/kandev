# Backend delivery retention handoff

This is an implementation draft, not a merge-ready change. Tasks 01–07 are complete. Task 08 remains in progress.
The user requested a draft PR on 2026-10-10 so a colleague can continue. This authorizes the handoff commit and push, superseding the earlier no-publication constraint. No acceptance limit is waived.

## Shutdown handoff (2026-10-10)

The user requested a pushed checkpoint within ten minutes before shutting down the machine. This is an incomplete implementation handoff. Task 08 stays in progress; do not signal workflow completion or mark the PR ready.

Backend CI fixture fixes are published. The browser checkpoint correlates the actual new projected recovery submission before killing its runtime, preserves the unchanged frozen N binary with explicit mock-helper provenance, and keeps agent survival enabled only throughout the retained-runtime rollback case. Teardown returns to the current binary with survival disabled. Static lint and formatting pass. The rollback case still fails exact effect preservation: a terminal receipt captured as `block_resolved` with no completion timestamp later becomes `completed` on N reopen. The failure occurs at the `written` snapshot comparison after the pending deletion job was reopened under N. Actual new N writes, deletion journaling, ID fencing and the pending-job reopen assertion had passed; forward purge was not reached. Wait for terminal and workflow settlement before capturing that new-write snapshot, retaining exact preservation assertions. Do not treat an idle session state as proof of full settlement.

The shutdown desktop run ended with seven passes, one rollback failure and one interrupted retry-exhaustion test (exit 130, 7.2m). This is not a passing nine-test receipt. The disposable PostgreSQL container is stopped and preserved; use `docker start kandev-retention-resume-pg` before reusing the local PostgreSQL runners. The final mobile matrix was not started because of the shutdown deadline. SQLite latency, storage/concurrency/conformance, final real PostgreSQL rollback/backup evidence, and Kubernetes CI diagnosis remain open.

Selected immutable binaries, complete receipt-cache measurement samples, and CI/browser evidence are also preserved outside `/tmp` at `/root/.kandev/task-artifacts/backend-agent-delivery-retention-20261010/`, with `SHA256.json`. Copy this local directory to the colleague's machine; it is not a GitHub attachment. The full historical artifacts remain under `/tmp/kandev-retention-resume-20261009/`. Private runner scripts and credentials are not included in the durable copy.

## Start here

Read [the manifest](plan.md), [Task 08](task-08-integration-and-storage.md), and the paired [requirements](../../specs/platform/requirements/backend-agent-delivery-retention.md) and [design](../../specs/platform/system-design/backend-agent-delivery-retention.md).
Task 08 records successful checks, failed measurements, source boundaries, and the exact final commands.

The implementation includes authenticated replay, hash/checkpoint recovery, bounded event and prompt cleanup, retirement jobs and orphan discovery, worker readiness/shutdown, configuration, telemetry, and offline maintenance. SQLite and real PostgreSQL focused race tests cover the changed behavior.

## Latest change and evidence

Work resumed after draft publication. The branch is rebased onto main `3fb026365239bbc893fa4a5272d926640c7ef1f9`; the PostgreSQL helper conflict is resolved. Latest change: the same-name unprojected replay index excludes projected history, repaired atomically at startup. Populated upgrade/cancellation/reopen and exact frozen-N generic-plan race GREEN passed on both engines (10.051s), plus broader affected regressions (77.265s). The complete pending-index comparison passed PostgreSQL and failed SQLite. Fixed receipt SELECT plan reuse now passes focused lifecycle (18.307s) and broad affected (64.996s) race checks, with fresh evidence preserved. Its complete original-runtime comparison passed PostgreSQL idle/active (0.888905x / 0.933549x) and failed SQLite idle/active (1.155385x / 1.317054x), with all 4500 samples per state retained. SQLite latency remains open. Task 08 preserves every previous failed window. Earlier artifact checksums/build receipts below are historical source boundaries.


Sparse ownership indexes now exclude blank session IDs. Current orphan and final-existence probes explicitly state that predicate. Unknown owned incarnations and blank/mixed cursor-attributed effects retain their original deletion scope.

Final focused race GREEN passed in 17.769s: fresh databases, old two/three-column repair, interruption/cancellation rollback, reopen, frozen-reader additive index DDL, and actual generic orphan query plans. Earlier focused GREEN passed in 23.385s. SQL guard and benchmark compilation passed.
Actual frozen-reader typed recovery remains indexed. Its legacy OR admission query scans 10001 effects under both full and sparse controls; no bounded-reader claim is made for that query.

Historical direct-prefix comparison (superseded by the receipt-plan result above) retained all 4500 samples per state/engine:

| Engine | Idle/baseline p95 | Active/baseline p95 | Limit |
| --- | --- | --- | --- |
| SQLite | 1.1117127270119913 | 1.3892183912981657 | <= 1.10, both failed |
| PostgreSQL | 0.8768164414816446 | 0.8857589902898558 | <= 1.10, both passed for that candidate |

All 18 commands and data-preservation assertions passed in that window. Command success does not satisfy latency acceptance. Do not round away a failure, discard samples, or reuse a preceding candidate's pass for new source.

CI fixture reconciliation now passes focused race checks for owner/receipt shutdown publication, audited configuration aliases, managed retirement admission/replacement, maintenance, and exact workflow settlement. Browser fixture corrections remain local and uncommitted while rollback validation is open. Recent desktop matrices pass eight cases and fail the frozen-N case. The PostgreSQL witness preserves the agent runtime; browser restarts still kill it. The next fixture correction must preserve the runtime across the rollback and return to baseline during teardown. Kubernetes compatibility CI reports cancelled-session failures with no established cause; keep those failures open. No production/native code changed in these CI fixture corrections.

## Remaining work, in order

1. Resolve the SQLite receipt-plan latency failures. Preserve the same predeclared baseline/idle/active interleaving, production pools, matched 100000-event history, and all 4500 samples in further comparisons. Resolve any remaining regression before calling Task 08 complete.
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

This witness certifies retained-session recovery with the agent runtime preserved across backend rollback, plus explicit-ID manual prompt/event writes. Source inspection identified historical implicit auto-start/direct-retry preparation gaps. It does not certify every launch path; a shipping Release N needs those admission corrections. Do not replace the frozen witness with a current binary and claim an old-reader test.

The unchanged pre-retention benchmark artifact SHA-256 is `3b21db2b295ac620ab7d3f61b6e82553c72aa1008843ec5566ae0c64cf08965e`.
The sparse benchmark artifact SHA-256 is `e51980564038831fd0981a8112b888125fc5ab8d645dcaa906fe2b8ff61f89d9`.
Obtain the baseline artifact and matched fixture scripts from the existing workspace when continuing elsewhere.

Native rollback tests require `KANDEV_RETENTION_CURRENT_BINARY`, `KANDEV_RETENTION_COMPAT_BINARY`, `KANDEV_RETENTION_ROLLBACK_POSTGRES_DSN`, `KANDEV_RETENTION_POSTGRES_CONTAINER`, and `KANDEV_RETENTION_BACKUP_OUTPUT`, in addition to `KANDEV_TEST_POSTGRES_DSN`. Verify each harness prerequisite and record skips as blockers. Never print credentials.

Historical initial handoff native/helper/web/plugin build passed with `GOMAXPROCS=2 GOFLAGS='-p=2' make build-backend build-backend-linux-helpers build-web-e2e build-e2e-plugin-package`.
Native SHA-256: `818c856021b1280439043548f81e4da5a20968864d9550ea4dd77c8b46fe33f3`.
Whole-backend lint passed with zero issues using `golangci-lint run ./... --new-from-rev=d6cff57659ae5aea28e9457c587a8c9d70d0a130 --timeout=20m --allow-serial-runners`. Catalog (370 decisions/1498 specifications), full specification lint, package coverage (22/22 ACs, 68 links) and whitespace checks passed.

Current rebased pending-index/receipt-plan native/helper/web/plugin build passed. Native SHA-256: `1d6b09b2a65dc1bb82b055a8cac29916688ebcc29e4162705a9ca8718aa3fe92`. Use this fresh artifact for remaining runtime checks. Whole-backend lint against refreshed main passed with zero issues; catalog, full specification lint, package audit and formatting pass. Runtime/storage/browser gates and SQLite latency remain open.

## Sequential PR delivery reconciliation (2026-10-11)

Merged the current main after the journal shutdown and interrupted-session recovery predecessors. Conflict resolution retains delivery identity fields, retention serialization and the recovery session revision fence. The lifecycle fixtures now provide a persisted execution owner, a complete projected cursor and the authenticated descriptor/client required by the predecessor ACK worker. No production ownership or uncertainty check is relaxed.

Local conflict validation passed: race-enabled repository recovery-block/reconstruction/harness/retention-integration selection (5.041s); watcher and orchestrator delivery/recovery/terminal selections (1.085s / 15.517s); lifecycle durable/reconstruction and the two retained/remote replay regressions (6.612s). Backend lint against the merged base reported zero issues. Catalog, full specification lint and package validation passed. These checks do not complete storage, latency, native rollback or browser acceptance.

The original compatibility and pre-retention benchmark artifacts are not available in the accessible task directories on this runner. Their recorded `/root` location is inaccessible to this account, so its contents are unverified. The original artifacts must be transferred before the unchanged compatibility and latency acceptance can run. Keep this PR draft and Task 08 in progress; the prior failed storage and browser CI evidence is not waived.
