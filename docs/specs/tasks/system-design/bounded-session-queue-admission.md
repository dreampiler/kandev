---
status: current
system: tasks
requirements:
  - REQ-TASKS-BOUNDED-QUEUE-ADMISSION-001
created: 2026-10-15
owners:
  - kandev
---

# Bounded Session Queue Admission Design

## Boundary and mapping

The messagequeue service owns admission acquisition; handlers own transport
feedback and the current attachment-cleanup retry loop. Durable queue identity
and replay remain owned by [queue admission](queue-admission.md).

| Criteria | Implementation boundary                                           |
| -------- | ----------------------------------------------------------------- |
| .1, .2   | `messagequeue.Service.WithSessionAdmission` and session gate      |
| .3       | Queued-dispatch error handling, queue handlers, queue API mapping |
| .4       | Existing handler attachment-cleanup obligation and recovery       |

## Acquisition and callback lifetime

Replace the per-session mutex with a small FIFO waiter gate. Cancellation removes
an ungranted waiter; cancellation after grant hands admission to the next waiter.
Recheck context after grant. Admission entries retain their existing reference
count and disappear only when the last holder or waiter releases its reference.

Derive an acquisition context with a 30-second default, retaining an earlier
caller deadline. Acquire the lifecycle read barrier by `TryRLock` with a
20-millisecond context-aware pause, then the session gate under the same budget.
Nested same-session calls reuse their admission token; multi-session operations
retain their existing stable session-ID order.

Use the acquisition context only at lock acquisition. The admitted callback and
its token retain the caller context and the existing after-lifecycle-unlock
callbacks. Convert only an internally budgeted acquisition deadline into
`ErrSessionAdmissionTimeout`; callback errors are returned unchanged.

## Failure and recovery

Add the new timeout sentinel to the existing queued-dispatch requeue branch.
Preserve all other accepted-work and primary-ownership predicates; the separate
restart-recovery proposal owns those changes. Do not reinterpret uncertain
provider delivery as a safe acquisition failure.

Keep the existing queued worker's reservation until its requeue or restore
mutation returns. Retry only acquisition-budget timeouts through the existing
reserved-prompt callback owner's cancellable lifetime; do not start another
provider prompt or retry identity, storage, caller-cancellation, or uncertain
delivery errors. The caller waits, so a borrowed admission token stays within
its owner's lock lifetime. Stop cancels and joins compensation and suppresses
the subsequent pending drain. Each acquisition keeps its own 30-second budget;
the same live compensation can wait for contention to clear without dropping
the known-undelivered message.

Auto-run and both removal contracts map this sentinel to the existing retryable
`UNAVAILABLE` response. Their web wrappers select the existing admission error
mapping, including translated queue-unavailable feedback. Identity and
entry-not-found errors retain their precedence.
The two web requests wait 35 seconds, so the server's acquisition result can
arrive within its 30-second budget plus the ordinary transport allowance.

Bound the attachment-cleanup loop at 60 attempts. Exhaustion releases only its
in-memory retry obligation; the persisted cleanup row remains available for
handler restart recovery. Successful cleanup, captured authorization, edit-lease
coordination, and attachment-transfer handling retain their current behavior.
Waiting for an active edit lease, including a lease discovered after admission,
does not spend a cleanup attempt. Keep the existing wake path until actual
cleanup or acknowledgement can run.

## Scope and verification

This ports the bounded-acquisition production slice of fork commit
`0797bea018f8802210e913dc30008cefb34f521e` onto upstream base
`15ed289c050f8bd2bd675c4542ffb796525bf991`. It excludes the original new standing
tests and unrelated damaged comment. Reuse existing focused tests and explicitly
invoked temporary overlays for the new contention cases, then remove those
temporary inputs. No core schema, grant, runtime policy, or deployment changes.
