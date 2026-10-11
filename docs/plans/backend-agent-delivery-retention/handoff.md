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
2. Measure concurrent replacement/deletion against final source. Actual million-event 1KiB/4KiB and protected-gap race fixtures now pass on both engines, with canonical equality and the full-drain reopen/maintenance receipts recorded below.
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

### CI remediation on the reconciled source

The fresh SQLite CI shard again exhausted three bounded deadline retries on the million-event 1 KiB fixture, then hit the default ten-minute package timeout during the 4 KiB fixture. A local 4 KiB RED reproduced the deadline failure (420.588s). Database-scale fixtures now request the shipped background worker's 50-row quantum; maximum-candidate coverage remains in the smaller selection/reload/window tests. Row counts, byte limits, 250ms batch deadline, retry bound, preservation and reopen assertions stay intact. CI and the documented integration command use Task 08's existing ninety-minute overall suite budget.

All three corrected SQLite scale race cases passed. The 4 KiB run (1540.466s) retired one million events and 4095996052 raw bytes with zero deadline retries; the 1 KiB run (1383.566s) retired one million events and 1023999124 raw bytes with one retry. Canonical content and cursors survived, and both databases measured 1470464 bytes after reopen/manual compaction. The protected-gap run (1297.616s) stopped retirement at 499999 and preserved all 500001 protected-backlog rows plus canonical content. These are storage receipts, not latency or frozen-reader acceptance receipts.

Fresh Kubernetes CI exposed initial-prompt admission rejecting a supported peer explicitly reporting `storage_not_durable`. Admission now applies the lifecycle's genuine-legacy normalization only for the supported version; unresolved work still blocks, and future versions, lost journals and unavailable storage cannot downgrade. Real SQL/provider-dispatch RED reproduced the exact admission error. Targeted race GREEN passed (1.660s, 23 PASS entries, 11 PostgreSQL-gated skips, zero failures). The added existing-durable-protocol control also passes without allowing downgrade (1.524s, 15 PASS entries, seven PostgreSQL-gated skips). Real PostgreSQL and Kubernetes pipeline validation on the updated head remains pending. No original artifact or release-acceptance gate is replaced.

The admission correction changes native source. Earlier current-binary digests and build receipts remain historical source boundaries; rebuild native/helpers/web/plugin artifacts from the final source before the existing runtime/rollback acceptance. Keep the frozen reader and pre-retention baseline unchanged.

Subsequent isolated PostgreSQL 18.6 initial/retry admission race GREEN executed all 11 selected cases with no skips or failures (15.526s, 13 PASS entries), including supported non-durable initial dispatch and unresolved/storage-failure fences. The server uses the repository-pinned image manifest `4ef4dbc939d61acea57712655ddb4b4ab27419c913f94cca0cd57cb3ea3c2280`. This closes the earlier local PostgreSQL admission skips; updated-head required CI and existing native/runtime acceptance remain pending.

The complete PostgreSQL scale race selection also passed (1171.686s, zero skips/failures): million-event 1 KiB and 4 KiB cases (369.08s / 510.25s) retired all raw events/payloads with zero deadline retries and retained canonical history/cursors through reopen and manual maintenance. The protected-gap case (291.22s) retained the 500001-row backlog, stopped retirement at 499999 and preserved canonical content. All six affected scale cases now pass across SQLite and PostgreSQL; the full final-source integration/native/browser and matched-baseline latency gates remain open.

The reconciled native, Linux helpers, E2E web and plugin package build passed on `ad79a2559b6a0dc040395290783193d0135a832a`; native SHA-256 is `0854485ea110052e8580bc210177ab3eb60ce44f30157d9c62c385ba65aa91ca`. Fresh CI exposed an obsolete workflow contract forbidding any explicit shard timeout. The corrected contract executes timeout selection for ordinary, retention-owning, mixed and unrelated child-package shards, preserving ten minutes for ordinary shards and ninety minutes only when the retention package is selected. All 13 workflow contract tests pass locally. Required CI and the remaining Task 08 acceptance are still pending; this build does not certify frozen-reader rollback or SQLite latency.

Fresh Kubernetes compatibility CI accepted the initial prompt on both server versions but still timed out waiting for completion. An isolated real Kind API-smoke RED preserved backend evidence: legacy effects carry the ordinary session ID, and the retention workflow guard incorrectly treated that ID alone as incomplete durable delivery binding. Delivery marker detection now uses journal-specific fields, matching event marker detection; complete durable binding and guarded receipt requirements remain intact. A real-shaped legacy replay regression reproduced the blocked first transition before the fix. Race GREEN passed (2.645s) for legacy replay, five partial durable marker fences, existing bound workflow outcomes, replay settlement and initial admission. The unchanged Kubernetes v1.36.1 API smoke then passed on rebuilt source `f2367e3d23bb9884b6ec9ed8a3dfe21adcc1a4d0` (one test, no retries, 3.1 minutes), including initial launch, agentctl message round-trip and completion; exact owned cluster/image/backend cleanup completed. Native SHA-256 is `545a50ae44668047cc4cd6bffa0dcf5007739dfbf53527f0001dc8d25f51d494`. Updated-head required CI remains pending. The original frozen/baseline artifacts and Task 08 latency/native/rollback/browser gates remain unchanged.

The `ad79a2559b6a0dc040395290783193d0135a832a` CI run completed with 29 passing and 26 failed/cancelled checks. All six required checks reported: architecture, frontend and harness passed; backend, E2E and action-pinning failed. There were no unresolved review threads. The complete SQLite repository shard passed in 4352.122s. PostgreSQL 16 cleanup drained both million-event fixtures and preserved canonical content but the test's sum of acknowledged byte counters missed complete 50-row batches; the combined PostgreSQL job later reached its ninety-minute timeout while repeating SQLite scale fixtures.

The storage measurement now derives removed bytes from the committed retirement frontier of its known-size seeded stream. A deadline can hide a committed transaction's acknowledgement; retry does not imply rollback. A deterministic lost-ack RED reproduced the missing batch, while rollback and ordinary-ack controls distinguish the outcomes. All six controls pass with race detection on SQLite and the pinned CI PostgreSQL 16.14 image (4.503s, no skips). Successful batches still require exact byte accounting; retries retain the same three-attempt, 250ms and row/payload budgets, plus full raw-row removal and canonical/reopen assertions. Production transactions and acknowledged telemetry are unchanged. PostgreSQL CI excludes only the duplicated SQLite million-event and protected-gap subtests already covered by `test_shards`, retaining all other SQLite tests and every PostgreSQL case. Fifteen workflow contracts pass, including an executable Go subtest-filter check and the PostgreSQL shared-memory requirement. Full backend lint against the merged predecessor base reports zero issues. Updated-head CI and the original Task 08 acceptance remain pending.

Pinned PostgreSQL 16 full-size revalidation drained and accounted for both million-event payload workloads with zero deadline retries, then failed at the post-reopen zero-row query because Docker's default 64 MiB shared memory could not resize PostgreSQL's 67145472-byte parallel-query segment. This is a RED receipt (849.252s), not completed storage acceptance. The CI PostgreSQL 16 service now receives 256 MiB shared memory; the owned isolated service matches that allocation. Both complete fixtures then passed with all preservation/reopen checks intact (850.974s, two passing payload-size cases, zero skips): 1 KiB retired 1023999124 raw bytes with zero deadline retries (logical cleanup 291.938s); 4 KiB retired 4095996052 bytes with one retry (469.431s). The real 4 KiB retry reproduced a lost commit acknowledgement: the durable frontier accounted for 409600 bytes while the acknowledged batch reported 204800 bytes. Canonical content, empty raw/generic receipts and retained cursors survived reopen and manual maintenance. This closes the two new PostgreSQL CI regressions locally; required new-head CI and original Task 08 acceptance remain open.

CI on `617defcd02158d420dcb553b8f1c5ad5000232ed` exposed a lifecycle `goconst` finding for the terminal outcome literal. Terminal delivery publication now uses the existing shared `models.DeliverySubmissionCompleted` state, matching the failed/cancelled branches without changing wire values or ownership behavior. Existing terminal-identity and startup-lease race regressions pass (2.578s); full backend lint with CI's `GOFLAGS=-trimpath` reports zero issues against the merged base. The current pipeline is still collecting results; this validated correction remains local until the batch push. Original Task 08 acceptance remains open.
