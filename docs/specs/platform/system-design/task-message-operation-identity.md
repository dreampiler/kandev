---
status: draft
system: platform
requirements:
  - REQ-PLATFORM-TASK-MSG-OPID-001
  - REQ-PLATFORM-TASK-MSG-OPID-002
  - REQ-PLATFORM-TASK-MSG-OPID-003
  - REQ-PLATFORM-TASK-MSG-OPID-004
---

# Task Message Operation Identity System Design

## Purpose and boundaries

This design makes one agent-to-agent send identifiable and its outcome
readable. It owns the operation record, the claim-before-dispatch ordering in
the message-task handler, the response receipt fields, the readback tool, and
retention.

It uses but does not own: message creation through `recordUserMessage`, the
message queue and its entry identities, session launch through
`StartCreatedSession`, the parent-question claim, and the `external_id`
identity convention it mirrors.

Boundaries it deliberately does not cross: it does not make delivery and the
response atomic, which is impossible across a process boundary, and it does not
attempt to shorten the launch budget.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-PLATFORM-TASK-MSG-OPID-001` | Operation record, Receipt in the response |
| `REQ-PLATFORM-TASK-MSG-OPID-002` | Readback, Settlement |
| `REQ-PLATFORM-TASK-MSG-OPID-003` | Claim before dispatch, Replay |
| `REQ-PLATFORM-TASK-MSG-OPID-004` | Ownership and reach, Retention, Observability |

## Components and responsibilities

| Component | Responsibility |
| --- | --- |
| `task_message_send_operations` table | Durable operation record and its unique identity constraint. |
| `TaskMessageSendOperation` model in `internal/task/models` | Row shape plus `MetaKeySendOperationID`, the key that rides on the delivered artifact. |
| `SendOperationRepository` in `internal/task/repository` and its `sqlite` implementation | Claim, read, and settle; the only writer of operation rows. |
| New `internal/mcp/handlers/message_task_operation.go` | Claim and replay orchestration, direct request-field comparison, receipt assembly. Extracted so the message-task handler file, already past the file-size limit, does not grow. |
| `handleMessageTask` | Calls the new seam once, after validation and before dispatch. |
| `internal/mcp/server/server.go` tool declaration | New `operation_id` parameter, new readback tool, description update. |
| New readback handler beside `get_task_conversation.go` | Identity-scoped, side-effect-free outcome lookup. |

## Operation record

New table `task_message_send_operations`:

| Column | Notes |
| --- | --- |
| `id` | Primary key. |
| `operation_id` | Caller token, validated for length and charset. |
| `sender_task_id`, `sender_session_id` | Owning caller; the scoping pair for read and replay. |
| `target_task_id`, `target_session_id` | Resolved destination. |
| `requested_prompt`, `requested_delivery_mode`, `requested_session_id` | The delivery-determining request fields exactly as sent, stored verbatim so a replay is compared against them by value. No derived digest column. |
| `state` | `pending` to `committed` or `failed`. Closed set. |
| `delivery_status` | `sent`, `queued`, or `started`; null until committed. |
| `message_id`, `queued_entry_id` | The receipt. `queued_entry_id` becomes reachable on the wire for the first time. |
| `failure_code` | Stable code when `failed`; never free text that could echo content. |
| `created_at`, `updated_at`, `settled_at` | Retention clock and audit. |

`UNIQUE(sender_task_id, sender_session_id, operation_id)` is the identity
constraint. It follows the `external_id` precedent in
`internal/task/repository/sqlite/base_migrations.go`: explicit deterministic
collation, and a partial index so non-identified sends write nothing.

Migration discipline follows `apps/backend/AGENTS.md`. A brand-new table needs
no `ADD COLUMN` step, but replay regression tests plus fresh-DB and same-DB
replay tests are required, and `internal/persistence/requiredstores` plus a
`storeconformance` adapter must be registered because this is a new built-in
SQL schema owner. A PostgreSQL behavior test is env-gated through
`KANDEV_TEST_POSTGRES_DSN` for the unique-violation classification, which must
not rely on SQLite error text.

## Control flow

### Claim before dispatch

1. Validate as today: task and session existence, sender, delivery mode,
   interrupt eligibility, parent-question validation.
2. If no `operation_id`, continue on today's path, byte-for-byte unchanged
   (AC-001.2).
3. If `operation_id` is present, attempt an insert-claim of
   `(sender_task_id, sender_session_id, operation_id)` with
   `state = 'pending'`, storing the delivery-determining request fields
   (prompt, delivery mode, requested target session) verbatim alongside it.
   - Insert won: this call owns the delivery. Proceed to step 4.
   - Unique violation: the identity is already claimed. Go to Replay. The
     violation is classified by constraint name, not by message text, mirroring
     `isExternalIDUniqueViolation`.
4. Dispatch exactly as today, inside the same call, so the common case is
   unchanged.
5. Settle the record from the dispatch result.

The claim is a single-row insert committed before step 4. That ordering is the
whole mechanism: once the row exists, an interrupted or timed-out caller has
something to read, and the pre-existing `queuedEntryID` and
`*models.Message` identities become persistable as a receipt.

### Settlement

`SettleOperation` writes `state`, `delivery_status`, `message_id`,
`queued_entry_id`, `failure_code`, and `settled_at` in one conditional update
guarded by `WHERE state = 'pending'`, so a late or duplicate settle cannot
rewrite a terminal outcome.

- The immediate path yields `message_id` from `recordUserMessage`, the ID that
  already exists and is simply discarded today.
- The queue path yields `queued_entry_id` from
  `QueueMessageWithMetadataForSession`, the value
  `taskMessageDispatchResult.queuedEntryID` already carries and whose doc
  comment already reserves it from the wire.
- `failed` covers a definite dispatch rejection, so the caller's readback
  distinguishes a rejected send, safe to send again under a new identity, from
  an in-flight send that must not be touched.
- Settlement runs on a context detached from the request, following the
  established `StepHistoryWriteTimeout` precedent in
  `internal/common/constants/timeouts.go`, so a client disconnect cannot drop
  the settlement of a delivery that committed. The row remains `pending` if
  the process dies before settlement, which readback reports as still in
  flight, the safe direction.

### Replay

On a unique violation the handler reads the existing row:

- All three stored request fields equal the replayed values: return the
  recorded outcome verbatim, with the original delivery status and receipt. No
  message row, no queue entry, no turn start, no interrupt (AC-003.1,
  AC-003.2). The comparison is a plain field-by-field equality check against
  the stored values.
- Any stored request field differs from the replayed value: reject with the
  stable code `operation_id_conflict`, delivering nothing and echoing no stored
  content (AC-003.3, AC-004.3).
- State `pending`: return the in-flight outcome and say the send must not be
  retried (AC-002.2).

`reply_to_question_id` composes: its existing claim still runs and still
short-circuits, so a reply that carries an operation identity is claimed in
both places without either mechanism double-delivering (AC-003.4). Precedence
when both are present: the parent-question claim is evaluated first, exactly
as today.

### Receipt in the response

The message-task handler's success payload gains `operation_id` as an echo,
plus `message_id` and `queued_entry_id`, each omitted when not applicable. The
existing `task_id`, `session_id`, and `status` fields are unchanged, so
current callers and any response parser are unaffected.

## Readback

A new tool `get_task_message_operation_kandev(task_id, operation_id)` returns
`state`, `delivery_status`, `message_id`, `queued_entry_id`, `failure_code`,
`created_at`, `settled_at`, and a `retry_safe` boolean that is true only for
`failed`. It reads one row and performs no delivery.

Scoping under AC-004.1: the row is looked up by the calling session's own
`(sender_task_id, sender_session_id)` as resolved by the existing in-session
MCP identity seam, the same way `get_task_conversation` resolves its session.
A row owned by another sender is reported as not found, so existence does not
leak.

`retry_safe` exists so the caller's decision is explicit: `pending` means wait
and re-read, `committed` means done and do not resend, `failed` means resend
under a fresh identity.

`get_task_conversation_kandev` is left unchanged. Content matching against it
remains possible for humans debugging an incident, but it is not the contract.

## Failure and recovery

| Failure | Behavior |
| --- | --- |
| Claim insert fails for a non-conflict reason | Fail the call before any delivery. Nothing was sent, so the caller may retry freely. |
| Replay with a differing request field | `operation_id_conflict`, no delivery. |
| Readback of an unknown identity | Conclusive not-claimed result (AC-002.3), distinct from `pending`. |
| Process dies between claim and settle | Row stays `pending`; readback says in flight. The delivery either committed, and its own transcript or queue state shows it, or did not; the record never lies in the unsafe direction. |
| Readback row deleted by retention before the caller reads it | Reported as not claimed only after the retention window; within the window the row is always present. Retention is bounded well beyond any plausible caller retry interval. |
| Caller supplies an over-long or malformed identity | Rejected as a validation error before any claim. |

## Persistence

- Storage ownership: `internal/task/repository/sqlite`, which also serves
  PostgreSQL. One transaction per claim, one per settle.
- Migration: idempotent `CREATE TABLE IF NOT EXISTS` plus
  `CREATE UNIQUE INDEX IF NOT EXISTS`, with fresh-DB and same-DB replay
  tests.
- Restart: operation rows are ordinary committed rows, so readback works
  immediately after restart with no recovery step (AC-002.4). This is the
  decisive difference from the in-memory workflow `operationLedger`.
- Dialect: no `rowid`, JSON, or date syntax in the changed methods; the
  conflict classification is by constraint name so PostgreSQL's 63-byte name
  truncation does not break it.

## Security

- The operation identity is caller-supplied and therefore untrusted input:
  bounded length, restricted charset, parameterized queries only.
- Authorization is on the sender pair, enforced before the row is read.
- Rejections never echo stored prompt content, so the identity cannot be used
  to probe another sender's traffic.
- The identity adds no new trust boundary: it does not widen who may message
  whom, and the deliberate cross-workspace stance of the message handler is
  unchanged.
- Scoping rules from `apps/backend/AGENTS.md` apply: the readback is a new
  session-keyed entry point and must authorize through the existing
  `authorize*` helpers, guarding before any dependency use.

## Observability

- `task_message_send_operation_total`, labeled `outcome` from the closed set
  `claimed`, `replayed`, or `conflict_rejected`, and `delivery_status` from
  `sent`, `queued`, or `started`. No task, session, prompt, or operation
  identity is ever a label.
- A structured zap log at claim, replay, and settle carrying the correlated
  identity for operators; logs, not metrics, carry the identifiers.
- Exposed under `/debug/vars` in dev mode per the repository's existing
  observability convention.

## Related decisions

No new ADR is required. This design generalizes the in-tree uncertain-delivery
contract in `explicit-turn-steering.md` and mirrors the accepted `external_id`
identity convention; it introduces no new architectural boundary. If
implementation reveals that agent sends need a cross-cutting identity
convention shared with other mutating MCP tools, raise an ADR then.