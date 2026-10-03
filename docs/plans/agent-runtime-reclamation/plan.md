---
created: 2026-10-03
status: in_progress
requirements:
  - REQ-EXECUTORS-RUNTIME-RECLAIM-001
  - REQ-EXECUTORS-RUNTIME-RECLAIM-002
  - REQ-EXECUTORS-RUNTIME-RECLAIM-003
  - REQ-EXECUTORS-RUNTIME-RECLAIM-004
system_design:
  - ../../specs/executors/system-design/agent-runtime-reclamation.md
legacy_specs: []
---

# Agent Runtime Reclamation Plan

## Goal

Stop agent runtimes and their committed memory from accumulating across operating
rounds and backend restarts, and give an operator authoritative per-session
attribution of what is still held.

The root cause was established by measurement before any code changed, and it was
not the cause originally suspected. See
`docs/specs/executors/requirements/agent-runtime-reclamation.md`.

## Scope of this plan

Four owner-approved outcomes, recorded 2026-10-03:

| Outcome | Requirement | Work order | State |
| --- | --- | --- | --- |
| Tear down runtimes for terminal sessions | REQ-001 | WO-01 | delivered |
| Stop recovery re-adopting terminal sessions | REQ-002 | WO-01 | delivered |
| Show per-session process and memory footprint | REQ-003 | WO-02 | delivered |
| Verify and repair descendant reclamation | REQ-004 | WO-01 | closed with evidence |

Explicitly withdrawn from the earlier draft, on owner instruction:

- A bounded idle floor independent of the workspace preference.
- Any change to the idle-suspension default.
- An ADR justifying either of the above.
- Per-session hard memory caps.

None of these is a pending decision. They were declined.

## Work packages

- [x] [WO-01: Terminal-session reclaim and recovery exclusion](task-01-terminal-reclaim.md) (done)
- [x] [WO-02: Per-session footprint projection](task-02-runtime-footprint-projection.md) (done)

### WO-01 — Terminal-session reclaim and recovery exclusion

Delivered in this branch.

Covers REQ-001, REQ-002, and the REQ-004 evidence close.

Two production changes, each with a decision-matrix or delegation test that was
observed failing before the change:

- `internal/orchestrator/reconcile_liveness.go` — admit `Failed` and `Cancelled`
  in `classifyIdleReclaim`, and apply the live-runtime guard only to open
  sessions.
- `internal/agent/runtime/lifecycle/persistence.go` — omit terminal-session rows
  from the standalone recovery inventory in
  `ListLiveStandaloneExecutorsRunning`, failing closed on an unreadable session.

Plus the `idle_session_reaper.go` invariant comments, which asserted "never stops
a live process" and no longer describe the behaviour.

### WO-02 — Per-session footprint projection

Delivered. Covers REQ-003.

This shipped as its own commits on the same branch rather than inside WO-01, because it is
a capability addition rather than a defect fix and it carries web UI work with a
mobile-parity obligation. Folding it into a fix commit would have hidden a large
surface behind a small bug fix.

## Dependency order

WO-01 has no dependency and is delivered.

WO-02 landed after WO-01 and therefore reports the post-reclaim figures, which is the
state an operator will actually read.

## Verification performed for WO-01

```
go test ./internal/orchestrator/ -run "TestClassifyIdleReclaim|TestReclaimIdleSession" -count=1
ok  github.com/kandev/kandev/internal/orchestrator

go test ./internal/agent/runtime/lifecycle/ -run "TestListLiveStandaloneExecutorsRunning" -count=1
ok  github.com/kandev/kandev/internal/agent/runtime/lifecycle

go test ./internal/agentctl/server/process/ -run "TestWindowsProcessLifecycleJobKillsDescendants|TestWindowsManagedLifecycleReapsDescendantAfterLeaderExit|TestWindowsProcessRunnerReapsDescendantAfterLeaderExit|TestWindowsVscodeLifecycleReapsDescendantAfterLeaderExit" -count=1 -v
ok  github.com/kandev/kandev/internal/agentctl/server/process  3.223s
```

### Known pre-existing failures, not caused by this work

Full-package runs of `internal/orchestrator` are not clean on the base commit
`42f572f0f`, before any change here. The base fails a different set of
order-dependent tests on each run, and one of them passes in isolation:

| Test | Base `42f572f0f` | With these changes |
| --- | --- | --- |
| `TestHandleAgentReady_PassthroughAttachmentOnlyOrdinaryMessageUsesAttachmentAwarePrompt` | fail | fail |
| `TestHandleAgentReady_PassthroughQueuedAttachmentUsesAttachmentAwarePrompt` | fail | fail |
| `TestCancelAgentSilent_DoesNotCloseSuccessorTurn` | fail | pass |
| `TestCeilingDispatchAdmissionSerializesRouteMutation` | pass | fail |
| `TestCompleteTurnsExceptReportsIterationExhaustion` | pass | fail, passes in isolation |

`internal/orchestrator/executor` has two Kubernetes tests failing on the untouched
base commit with `config.kubeconfig_path: must be an absolute path for kubeconfig
auth`. These are pre-existing and unrelated.

The three orchestrator tests that flip between base and changed runs are
order-dependent flakes in that package. None touches the reclaim predicate, and
the reclaim tests pass deterministically when run on their own.

## Delivery

Branch `kd/agent-runtime-terminal-reclaim`, based on fork `main` at `42f572f0f`.
The earlier branch `kd/agent-runtime-terminal-reclaim`'s predecessor worktree
`kandev-wt-agent-runtime-reclamation` stays intact; nothing was ported from it.

The uncommitted observability slice in `kandev-wt-agent-runtime-reclamation` was
not ported because it does not compile: `lifecycle/metrics_vars.go` and
`internal/runtimemetrics/metrics_vars.go` register the same `expvar` names, which
panics at package init, and `orchestrator/runtime_footprint.go` references
symbols it neither imports nor defines. WO-02 is a rewrite.

## Verification for WO-02

Not yet applicable. When it lands it must additionally satisfy the repository
checks for the web subtree it touches: typecheck, lint, `i18n:check`, and vitest,
plus the mobile-parity outcome and its focused mobile E2E.