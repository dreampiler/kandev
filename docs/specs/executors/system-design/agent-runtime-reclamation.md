---
status: current
system: executors
requirements:
  - REQ-EXECUTORS-RUNTIME-RECLAIM-001
  - REQ-EXECUTORS-RUNTIME-RECLAIM-002
  - REQ-EXECUTORS-RUNTIME-RECLAIM-003
  - REQ-EXECUTORS-RUNTIME-RECLAIM-004
---

# Agent Runtime Reclamation System Design

## Overview

Two independent defects retain agent runtimes after a session can no longer use
them, and one capability is missing that would have made both visible.

The two defects are separate mechanisms and are fixed separately:

1. A terminal session's runtime is never released, because the shared reclaim
   predicate excludes `Failed` and `Cancelled`. Fixed in
   `internal/orchestrator/reconcile_liveness.go`.
2. A terminal session's stale row is re-adopted at every startup, because the
   recovery inventory offers it as a re-adoption candidate. Fixed in
   `internal/agent/runtime/lifecycle/persistence.go`.

Neither fix changes a setting, a default, or a wire contract.

## Design decision: fix the predicate, not a new retention policy

The obvious remedy for unbounded growth is a retention bound: keep at most N
runtimes, or keep nothing idle for longer than a floor. That was drafted and then
withdrawn. Two reasons:

- It changes a durable default, which the owner declined.
- It does not address the actual defect. A terminal session has no reader at all,
  so it is not a retention-policy question; releasing it is simply correct.

## REQ-001: terminal-session reclaim

### Where the decision lives

`classifyIdleReclaim` in `internal/orchestrator/reconcile_liveness.go` is the
single decision point for both reclaim paths. `reclaimIdleSessionsOnce` scans
non-stopped rows on the periodic tick; the synchronous settle points
(`setSessionWaitingForInputIfRequested`, `handleAgentCompleted`) call the same
primitive. Both therefore change together, and there is no second predicate to
keep in sync.

### The change

The predicate distinguishes terminal from open sessions:

```go
terminal := false
switch sessionState {
case WaitingForInput, Idle, Completed:
case Failed, Cancelled:
    terminal = true
default:
    return idleReclaimDispositionSkippedState
}
if agentRunning && !terminal {
    return idleReclaimDispositionSkippedLive
}
if hasActiveTurn {
    return idleReclaimDispositionSkippedTurn
}
```

### Why the live-runtime guard is conditional

For an open session the guard is the safety property: `WaitingForInput` and
`Completed` runtimes are resumable, so a live process means the session can still
be resumed and must not be torn down.

For a terminal session that reasoning inverts. The probe observing a live process
is not evidence of a pending reader; it is the retained footprint. Gating on it
means the reclaim can never run, which is the current behaviour.

The guard that still applies is the active-turn one. A session that went terminal
while a turn was genuinely in flight is not yet safe to release, and that is the
case the guard exists for. This is why the terminal branch bypasses exactly one
guard and not the others.

### What reclaim does downstream

`reclaimIdleSession` releases through
`CleanupStaleExecutionBySessionIDIfCurrent`, which CAS-guards on the execution
identity and `UpdatedAt` it read, then routes to the lifecycle cleanup that stops
the agent runtime and repairs the row to `status=stopped, local_pid=0`. The CAS
means a row that changed since the read is not acted on. The resume token and
worktree path are preserved, so the session stays resumable.

### Rejected alternative: terminal teardown on the state transition

Tearing the runtime down at the moment a session becomes terminal would be more
direct. It was rejected because a terminal transition can be speculative (a
resume attempt that fails, a cancellation that a later step reopens), and the
transition sites are numerous and shared with the retry paths. The reclaim
primitive already has the fail-closed guard set and the CAS; routing terminal
sessions through it reuses that instead of adding a second teardown path with its
own guard set.

## REQ-002: recovery inventory excludes terminal sessions

### Where the filter lives

`Manager.ListLiveStandaloneExecutorsRunning` in
`internal/agent/runtime/lifecycle/persistence.go`. This is the single producer of
the standalone recovery inventory, read at startup step 3 before any
control-server contact.

Filtering here rather than in `CorrelateRecoveryInstances` is deliberate. The
correlation function is a pure policy over `(records, instances)` and is unit
tested as such; giving it session state would require threading a repository read
into a pure function or changing the `ExecutorBackend.RecoverInstances` signature,
which eight executor implementations share. The manager already holds a session
reader, so the filter costs one bounded read per candidate row at startup.

### The change

`dropTerminalSessionRecords` removes rows whose owning session is in `Completed`,
`Failed`, or `Cancelled`. With the record gone, `CorrelateRecoveryInstances` finds
no record for that session and the instance is already classified as a record-less
orphan, which the existing recovery stop path dispatches. No correlation policy
changes.

### Why `Created` is not terminal here

`Created` is a never-started placeholder with no runtime behind it, so it has
nothing to reclaim and no process to stop. Treating it as terminal here would drop
its row from the inventory and stop nothing, changing recovery input for no gain.

### Fail-closed direction

A session read that errors, or returns nil, keeps its record. The filter may only
ever remove a record whose terminal state it actually read. An unreadable session
is not evidence of a terminal state, so existing recovery behaviour is unchanged
when the read is unavailable.

## REQ-003: per-session footprint projection

Not yet implemented. See the work order for the shape.

### Where the measurement belongs

The owning process tree is only knowable where the process was launched, which is
the agentctl instance manager. Measurement therefore starts at the agent process
identifier that manager owns and follows parent links, so every measured process
is a descendant of that instance's own agent rather than a host-wide name match.

### Identity and privacy

The projection carries counts, bytes, runtime state, and timestamps. It carries no
command line, environment value, credential, or resume token, because it is safe
to project on a diagnostic surface. No task, session, execution, user, or agent
identifier is ever a metric label; per-session values live on the bounded
projection, not in expvar.

### Lower bounds are explicit

A process that exits between the snapshot and the measurement is recorded as
unreadable rather than dropped, and unreadable owned processes make the byte
totals a lower bound. A platform that cannot report commit charge per process
reports zero committed bytes rather than estimating from resident bytes. A
caller must never read a nonzero unreadable count as zero memory.

### Bounded read

The observation runs on the existing maintenance tick, not a request path, so it
costs at most one bounded process walk per interval. A control server that cannot
enumerate or measure returns an error rather than a partial answer presented as
complete.

## REQ-004: descendant reclamation

Closed with evidence; no design change. See the requirements document for the two
rejected hypotheses, the four covering regression tests, and their passing run on
the observed host.

The ownership boundary is the kill-on-close Job Object an agent process is bound
to before it can spawn descendants, plus the equivalent reap path in the process
runner and the VS Code manager.

## Cross-cutting invariants

- **Fail closed.** Every new guard defaults to skip, never to force. The terminal
  branch is the single deliberate exception, and it is bounded by the active-turn
  guard.
- **Identity, not names.** Reclaim and adoption decisions key on session and
  execution identity. Process names and bare process identifiers are never
  ownership evidence.
- **No default change.** This design adds no setting, changes no default, and
  requires no migration.
- **Ownership is host-local.** Runtimes whose processes live on another host have
  no host-local footprint and are outside every rule here.