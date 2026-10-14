---
status: done
---

# Work Order: Bound Queue Acquisition and Cleanup

## Outcome

Implement the six-path production slice for
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
`handlers/queue_handlers.go`, and web `lib/api/domains/queue-api.ts`.

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

Source implementation is complete on base
`15ed289c050f8bd2bd675c4542ffb796525bf991`. The production slice is six files;
the requirements, design, manifest and this work order form one delivery
package. No standing test file or case was added or changed. Git publication and
existing task/runtime recovery remain separate integration responsibilities.

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
