---
id: P37-01
title: Bound Queue Acquisition and Cleanup
status: done
wave: 1
depends_on: []
plan: plan.md
requirements:
  - REQ-TASKS-BOUNDED-QUEUE-ADMISSION-001
acceptance_criteria:
  - AC-TASKS-BOUNDED-QUEUE-ADMISSION-001.1
  - AC-TASKS-BOUNDED-QUEUE-ADMISSION-001.2
  - AC-TASKS-BOUNDED-QUEUE-ADMISSION-001.3
  - AC-TASKS-BOUNDED-QUEUE-ADMISSION-001.4
system_design:
  - ../../specs/tasks/system-design/bounded-session-queue-admission.md
---

# Work Order: Bound Queue Acquisition and Cleanup

## Outcome

Implement bounded queue acquisition and its existing recovery-owner integration for
`REQ-TASKS-BOUNDED-QUEUE-ADMISSION-001`, criteria .1-.4, following the
[system design](../../specs/tasks/system-design/bounded-session-queue-admission.md).

## Scope and exclusions

Update messagequeue service acquisition and add its FIFO gate; add the timeout
requeue predicate, handler `UNAVAILABLE` mapping, cleanup attempt budget, and
the two existing web API error wrappers. Preserve existing user input, provider
order, queue identity/replay, callback context, and cleanup authorization.
Do not port the original standing test files/cases or #4479's independent
accepted-work and primary-ownership implementation. No deployment or Git
publication is part of this work order's implementation handoff.

## Acceptance

1. Contended acquisition returns within its context/budget with no leaked waiter
   or queue mutation; an admitted callback retains its original context/error.
2. A taken message is requeued on the new timeout and operator actions expose
   the existing retryable feedback; identity and attachment semantics remain.
3. Cleanup exhaustion leaves the durable row, and existing restart recovery
   still settles the obligation.

## Files

`orchestrator/messagequeue/service.go`, `messagequeue/session_admission_gate.go`,
`orchestrator/event_handlers_agent.go`, `handlers/queue_operations_handlers.go`,
`handlers/queue_handlers.go`, `orchestrator/queued_dispatch.go`, and web
`lib/api/domains/queue-api.ts`. Update only the existing web request assertion
when its timeout argument changes.

## Verification

From `apps/backend`:

```text
go test -trimpath ./internal/orchestrator/messagequeue -run 'TestServiceQueueOperationsWaitForAdmission|TestRestoreSendNowClaimUsesSourceSessionAdmission|TestSendNowClaimIdentityValidationRejectsBeforeRepository' -count=1
go test -trimpath ./internal/orchestrator/handlers -run 'TestWsQueueMessageMapsDeniedIdentifiedSessionToAdmissionUnavailable|TestWsQueueMessageReplaysOrdinaryAdmissionWithoutDuplicatingContent|TestWsRemoveEntryRetriesFailedAttachmentRelease|TestPendingAttachmentCleanupResumesAfterHandlerRestart|TestWsUpdateMessageCleanupRetryPreservesAuthAndPublishesStatus|TestPublishStatusDetachesCancelledRequest' -count=1
```

Invoke temporary Go contention/requeue/cleanup checks through an explicit
overlay whose backing files are outside repository collectors, then remove
them. Use the existing web `lib/api/domains/queue-api.test.ts` file after the
wrapper changes. Apply existing changed-file lint; do not repeat unrelated
full suites. Record actual commands, exit codes, limitations, and cleanup here.

## Results

The initial six-path slice was published at
`7fb36ac129344d73b4a996f782a6c7e678e386d8` on base
`15ed289c050f8bd2bd675c4542ffb796525bf991`. The follow-up below adds the existing
queued-dispatch ownership seam and corrects one existing web assertion. No
standing test case was added. The requirements, design, manifest and this work
order form the same delivery package; operating recovery remains separate.

Compared with the historical patch, deadline conversion now occurs only on
failed lock acquisition. A callback's `context.DeadlineExceeded` is returned
unchanged, preserving the acquisition-only contract rather than treating
admitted work as an acquisition failure.

- Existing messagequeue command above: exit 0, three tests passed, package
  result `ok 0.953s`.
- Explicit temporary overlay, `go test -trimpath -p 2 -race -overlay
<temporary-overlay.json> ./internal/orchestrator/messagequeue -run
'^TestP37Once' -count=1 -v -timeout=60s`: exit 0, four temporary checks passed,
  package result `ok 1.195s`. Covered session/purge contention, own/caller
  deadlines, callback context/error, FIFO cancellation, cancellation after
  grant, and reference release.
- Explicit temporary overlay, `go test -trimpath -p 2 -overlay
<temporary-overlay.json> ./internal/orchestrator/handlers -run
'TestP37Once|TestWsQueueMessageMapsDeniedIdentifiedSessionToAdmissionUnavailable|TestWsQueueMessageReplaysOrdinaryAdmissionWithoutDuplicatingContent|TestWsRemoveEntryRetriesFailedAttachmentRelease|TestPendingAttachmentCleanupResumesAfterHandlerRestart|TestWsUpdateMessageCleanupRetryPreservesAuthAndPublishesStatus|TestPublishStatusDetachesCancelledRequest'
-count=1 -v -timeout=90s`: exit 0, two temporary and six existing checks passed,
  package result `ok 0.338s`. The temporary budget check retained a durable row
  after exhaustion and settled it after reopening the existing fixture, with
  the captured user authorization preserved.
- Explicit temporary overlay, `go test -trimpath -p 2 -overlay
<temporary-overlay.json> ./internal/orchestrator -run
'^TestP37OnceAdmissionTimeoutRequeuesTakenWork$' -count=1 -v -timeout=90s`:
  exit 0, the taken message retained its content and identity after requeue,
  package result `ok 0.409s`.
- Web: `pnpm exec vitest run lib/api/domains/queue-api.test.ts`: exit 0,
  one existing file and 30 tests passed. Changed-file ESLint and Prettier:
  exit 0. Dependencies were installed once with
  `pnpm install --frozen-lockfile`; no dependency files changed.
- Specification metadata checked through the existing linter's scoped
  metadata functions: exit 0, two files. Documentation was formatted through
  existing Prettier; `git diff --check` passed.
- The first normal changed-file Go-lint run across all three affected packages
  returned exit 1 with `0 issues` followed by `Timeout exceeded`. This is not a
  lint pass or a demonstrated source/baseline defect. The invocation used the
  correct comparison base above, process-local repository defaults, and no
  operating/model/profile limit changes. A narrower package split is recorded
  separately below.

All three temporary Go backing files and their overlay JSON were removed after
their actual results were recorded. Their virtual test paths were never
created in the repository, and temporary budget changes existed only in the
test processes. No raw temporary source or overlay remains for collection.

The normal changed-file lint split for `messagequeue` and `handlers` returned
exit 0 with `0 issues`. The isolated orchestrator package also returned exit 0
with `0 issues`. Both used `scripts/lint-go-changed` and the fixed comparison
base above; splitting the initial invocation completed validation without
changing the repository's resource defaults or production code.

Integration documentation: the existing public coordination explanation now
describes the acquisition-only 30-second wait and retryable busy outcome for
Remove and Auto-run. No public action, setting, or screenshot changes.
`node scripts/validate-public-docs.mjs` returned exit 0 and validated 48
published pages. This documentation-only delta does not repeat code tests.

Publication preflight found missing work-order linkage metadata. Added the
existing required frontmatter to this work order and its plan, retaining the
same requirements and criteria. The existing `pr-docs.cjs` coverage evaluator
returned exit 0, `status: covered`, and no errors for all eleven changed paths.
Production and test sources did not change.

## Publication follow-up

At published head `7fb36ac129344d73b4a996f782a6c7e678e386d8`, source review
confirmed that the browser's five-second default could discard Auto-run and
Remove responses before the 30-second admission result. Both requests now use
the existing 35-second transport allowance. The existing Auto-run assertion was
updated; no new permanent test case was added.

An active edit lease also consumed the cleanup budget without a cleanup attempt.
The loop now counts actual cleanup/acknowledgement attempts and excludes the
post-admission lease refusal. The existing wake, stop and durable recovery paths
remain. One collector-excluded temporary check held an ordinary edit beyond a
one-attempt budget, confirmed retained pending ownership and zero cleanup calls,
then ended the lease and completed cleanup once. Its source/overlay were removed.

Existing web tests: one file, 30 tests, exit 0. Final temporary plus six existing
handler checks: exit 0, package result `ok 0.650s`. Scoped web ESLint: exit 0.
The first Go-lint attempt was rejected because the normal P46 commit was already
running golangci-lint; this is not a source finding or a pass. A sequential
changed-handler lint was started after that owner's Go lint completed.

Failed requeue/restore now waits for compensation settlement before the existing
queued worker can clear its reservation. Only acquisition timeouts are retried,
using the existing reserved-prompt callback owner's cancellation and Stop join.
The reservation phases, durable claims, original queue identity/FIFO, caller
context and terminal-error handling remain. Stop also suppresses pending drain.

Source review of the two recovery paths found no mandatory defect. Temporary
native-worker checks reproduced the original failure at virtual 61 seconds,
after two 30-second admission waits. With the correction, ordinary taken work
and a valid retained lifecycle reservation kept their markers until contention
cleared, then restored visible FIFO work and metadata. Actual Service.Stop
cancelled/joined compensation and retained durable claim data; its disposable
fixture had no running scheduler, so full shutdown success is not claimed.
Caller and terminal errors were not retried. The ordinary/Stop/error cases passed inside
a mixed command whose unsupported retained-ordinary fixture failed; those valid
results are reused, not labelled an aggregate command pass. The corrected native
retained-lifecycle case passed separately (exit 0, package `ok 0.902s`). Fixture
compile/lifetime errors and the unsupported reservation combination are recorded
separately from product failures. Final changed-handler and changed-orchestrator
lint each exited 0 with zero issues; scoped orchestrator vet also exited 0.
All four recovery-source/overlay temporary files were removed, with leftovers
`[]`. No operating provider or new standing case is used.
