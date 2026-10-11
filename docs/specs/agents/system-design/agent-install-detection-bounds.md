---
status: current
system: agents
requirements:
  - REQ-AGENTS-INSTALL-DETECTION-001
created: 2026-10-12
owners:
  - kandev
---

# Agent Install Detection Bounds System Design

## Purpose and boundaries

`internal/agent/agents/detect.go` owns how a detection option runs a capability
check. `internal/agent/discovery` owns the sweep that runs every enabled agent's
`IsInstalled` concurrently under a 15s budget. This design changes only the
first: how one check distinguishes an elapsed bound from a nonzero exit, and how
the agent whose check is measurably slower than the shared bound spends a second
measurement. The sweep's budget, concurrency, and cache are unchanged.

Detection answers only whether the executable and its capability check are
present and healthy. Authentication and protocol compatibility still belong to
the ACP probe path.

## Components

| Surface | Responsibility |
| --- | --- |
| `runCommandCheck` | Runs one bounded check and reports `found`, `timedOut`, and caller cancellation |
| `WithCommandCheck` | Unchanged contract, now expressed through `runCommandCheck` |
| `withHermesACPCheck` | Staged measurement for Hermes, the one agent that needs it |

## Bounded check

`runCommandCheck(ctx, path, timeout, args...)` returns four distinct outcomes:

| Outcome | `found` | `timedOut` | `err` |
| --- | --- | --- | --- |
| Command exited 0 | `true` | `false` | `nil` |
| Command exited nonzero or could not start | `false` | `false` | `nil` |
| Bound elapsed first | `false` | `true` | `nil` |
| Caller cancelled | `false` | `false` | `ctx.Err()` |

`timedOut` comes from the check context's own deadline, after the parent context
is ruled out, so a caller cancellation is never reported as a timeout.
`WithCommandCheck` keeps its previous contract exactly: everything except
cancellation maps to unavailable. Agents that use it, such as MiniMax, Muse, and
Pi, see no behavior change.

## Staged measurement

```text
LookPath(hermes) -- missing -------------------------------> unavailable
      |
      +-- runCommandCheck(first = 5s)
      |     +-- exited 0 ----------------------------------> available
      |     +-- exited nonzero ----------------------------> unavailable (conclusive)
      |     +-- caller cancelled --------------------------> error
      |     +-- bound elapsed
      |
      +-- runCommandCheck(second = 8s) -- final ----------> available | unavailable | error
```

The second measurement exists only because the first bound elapsed. The first
bound is the fast path and the second is a fallback, so an install that answers
promptly is never measured twice.

## Why Hermes needs the staged form

`hermes acp --check` resolves the ACP extra on every run, and discovery starts
every enabled agent's probe in its own goroutine. At sweep width the check was
measured at 5.5s to 12.1s while exiting 0, against a shared 5s bound, which is
why an installed Hermes was reported as missing on every sweep.

The bounds sum to 13s, inside the 15s sweep budget, so the sweep stays bounded
even when both measurements run.

## Latency trade-off

Only a Hermes check whose first bound elapses costs more time. Callers that
check agents one at a time, such as inference agent listing, host utility
bootstrap and refresh pre-checks, and the settings capability synthesis, wait up
to 13s for Hermes in that case instead of 5s. Fast, failed, and missing installs
take the same time as before. This is accepted in exchange for reporting a slow
but healthy install correctly; a check that keeps hanging is still reported
unavailable after the second bound.

## Invariants

- A conclusive failure is measured at most once.
- An elapsed bound is the only reason a second measurement happens.
- A caller cancellation is an error at either measurement and is never turned
  into "unavailable".
- Every other agent's detection path and bound are unchanged.
- No configuration key, setting, or exported API exists for a bound.

## Alternatives rejected

- **Raise the shared bound for every agent.** Slows every detection sweep,
  including for the many agents that are not installed, to fix one agent.
- **One longer Hermes-specific bound with no retry.** Smaller, but it cannot
  tell a slow check from a failed one, and a hanging Hermes would delay every
  caller by the full bound.
- **Retry on any failure.** Re-runs conclusive failures and adds latency for an
  install that is already known to be unusable.

## Testing

Unit tests cover `runCommandCheck` directly for success, nonzero exit, elapsed
bound, caller cancellation, and a binary that cannot start. The staged
measurement is driven through `Detect` with sub-second bounds and a helper
binary that stalls, asserting availability and invocation counts, so "measured
twice after a timeout" and "measured once after a nonzero exit" are told apart
rather than both appearing as unavailable.

The Hermes test helper copies the test binary instead of hard-linking it. On
Windows a hard link to the running image stays locked for the test's lifetime,
so the temporary-directory cleanup could not remove it.
