---
status: draft
system: agents
requirements:
  - REQ-AGENTS-SESSION-CEILING-001
  - REQ-AGENTS-SESSION-CEILING-002
created: 2026-11-04
owners:
  - kandev
---

# Session capacity population and observation

## Purpose and boundaries

This is the existing population and logging detail extracted from
[session concurrency ceiling](session-concurrency-ceiling.md) for file size.
It adds no behavior, observer, collector, authority or acceptance condition.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-SESSION-CEILING-001` | Current inference population and historical continuity |
| `REQ-AGENTS-SESSION-CEILING-002` | Correlated execution boundaries |

## Population and observation

### Current inference population and historical continuity

Finite `all` capacity includes native inference reservations and children in the
same total and grade limits as task launches. Agentctl's attempt snapshot keeps
historical `known` separate from versioned `current` evidence. Missing historical
owner metadata alone does not make an otherwise complete current registry unknown.
Concrete unknown children, unavailable owner storage, missing bindings or failed
provider settlement still block admission; an empty list alone proves nothing.

The authenticated control inventory applies the reservation requirement under the
instance-publication lock to existing and future instances. Token consumption uses
the same owner lock as this mode change. The backend prepares a retained token,
persists its exact capacity binding, validates provider settlement authority, then
admits that token before starting the child. Admission permission is process-local.
A tokenless direct start either precedes the gate and appears in the inventory or
is rejected. Runtime leases fence replaced control servers and malformed lists
fail closed. Finite `all` mode requires the new control API and gated current
snapshots; unsupported runtimes cannot certify a free seat. Optional capacity modes
can observe the current registry without requiring this gate.

After real teardown, an owner closes only when every concrete attempt is known,
settled and acknowledged. A same-session successor still needs matching
incarnation/generation and the existing journal owner CAS. Settled, acknowledged
predecessor entries stay in history but leave the current snapshot. Historical
`known` remains false throughout. Terminal observation, provider settlement,
binding removal and retained-token ACK retain their existing order. This changes
no SQL schema and adds no background poller; backend and agentctl must be updated
together for the new gated inventory path.

Upgrade compatibility also covers the old `unproven` phase when historical
continuity is false and the retained attempt map contains no entries. The
exclusive journal, matching session/incarnation/generation and owner CAS remain
required. This transfers no attempt or reservation and leaves historical
continuity false. New process admission durably marks even legacy owners active;
active predecessors, any retained attempt entry, unknown children, identity
conflicts and storage failures do not qualify for this compatibility case.

### Correlated execution boundaries

The existing structured log sinks carry an `execution flow` envelope at actual
boundaries. Request acceptance, asynchronous dispatch, executor preparation,
process start, session restore, turn persistence, first message segment, tool
start, durable delivery receipt, terminal settlement, and reservation release
are distinct outcomes. A received request is not evidence of a model call or
useful work. A completed turn is not evidence that its task is complete.

The envelope joins request/correlation, task, session, attempt, turn, execution,
message, submission, stream/sequence, profile, workflow/step/entry and reservation
identities when those identities exist at that boundary. Route, harness and
prompt generations have separate fields. The backend boot ID is the same one
reported by system info; agentctl events join to that boot through execution ID.
Paired operations record elapsed milliseconds. Admission records known/unknown
population, overall and grade limits, and the concrete waiting reason.

Success transitions are not sampled. A first message segment uses the existing
append marker, not a new unbounded deduplication map. Token/reasoning appends and
status polls do not produce flow events. Capacity wait logging occurs when its
reason changes. Existing bounded asynchronous sinks and rotation remain in use;
sink loss counters remain authoritative if storage cannot keep up.

No prompt, response, command, path, environment, arbitrary metadata or raw error
is part of the envelope. Identifier fields are bounded to 192 ASCII identifier
characters. The agentctl relay accepts only the typed envelope on the fixed flow
message, caps its encoded input at 8 KiB, and discards unknown properties. Other
child log fields continue to be excluded. Startup refusal names the owner-claim,
identity, revision, storage or activation-persistence boundary rather than using
`generic_launch_failure` as a root cause.

These logs prove native receipt and execution boundaries. They do not infer the
semantic relevance of an agent's output or a separate desktop conversation's
queue behavior; those require the corresponding official conversation records.
Operational acceptance still requires a real same-session first tool, useful
output, completion, settled/released capacity and a subsequent assignment after
installation. Isolated fixtures do not establish installed recovery or sustained
operation near the configured overall and grade limits.
