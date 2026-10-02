---
status: current
system: agents
requirements:
  - REQ-AGENTS-INSTALL-DETECTION-001
---

# Agent install detection bounds system design

## Purpose and boundaries

`internal/agent/agents/detect.go` owns how a detection option runs a
capability check; `internal/agent/discovery` owns the sweep that runs every
agent's probe concurrently under a 15s budget. This design changes only the
first: how one check distinguishes an elapsed bound from a nonzero exit, and
how an agent whose check is measurably slower than the shared bound spends a
second measurement. The sweep's budget, concurrency, and cache are unchanged.

Discovery versus ACP probing is unchanged: detection answers only whether the
executable and its capability check are present and healthy. Authentication and
protocol compatibility still belong to the ACP probe path.

## Components

| Surface | Owner |
| --- | --- |
| `runCommandCheck` | Bounded check, reports `found`, `timedOut`, and caller cancellation |
| `WithCommandCheck` | Unchanged behavior, now expressed through `runCommandCheck` |
| `withHermesACPCheck` | Staged measurement for the one agent that needs it |

## Bounded check

`runCommandCheck(ctx, path, timeout, args...)` returns three distinct outcomes:

| Outcome | `found` | `timedOut` | `err` |
| --- | --- | --- | --- |
| Command exited 0 | `true` | `false` | `nil` |
| Command exited nonzero | `false` | `false` | `nil` |
| Bound elapsed first | `false` | `true` | `nil` |
| Caller cancelled | `false` | `false` | `ctx.Err()` |

`timedOut` is derived from the check context's own deadline, after the parent
context is ruled out, so a caller cancellation is never reported as a timeout.
`WithCommandCheck` keeps its previous contract exactly: everything except
cancellation maps to unavailable.

## Staged measurement

```
LookPath(hermes) ── miss ──────────────────────────────► unavailable
      │
      ├── runCommandCheck(first = 5s)
      │     ├── found        ──────────────────────► available
      │     ├── !timedOut    ──────────────────────► unavailable (conclusive)
      │     └── caller cancel──────────────────────► error
      │
      └── runCommandCheck(second = 8s)  ── final ──► available | unavailable
```

The second measurement exists only because the first bound elapsed. That makes
the first bound a fast path and the second a fallback, so an install that
answers promptly is never measured twice.

## Why Hermes needs the staged form

`hermes acp --check` resolves the ACP extra on every run, and discovery starts
every enabled agent's probe in its own goroutine. At sweep width the check was
measured at 5.5s to 12.1s while exiting 0, against a shared 5s bound, which is
why an installed Hermes was reported as missing on every sweep.

The bound pair sums to 13s, inside the 15s sweep budget, so the sweep stays
bounded even when both measurements run.

## Invariants

- A conclusive failure is measured exactly once.
- An elapsed bound is the only reason a second measurement happens.
- A caller cancellation is an error at either measurement and is never
  swallowed into "unavailable".
- Every other agent's detection path and bound are unchanged.
- No new configuration or exported API exists for a bound.

## Alternatives rejected

- **Raise the shared bound for every agent.** Slows every detection sweep,
  including for the many agents that are not installed, to fix one agent.
- **One longer Hermes-specific bound with no retry.** Smaller change, but it
  cannot distinguish a slow check from a failed one, and a genuinely hanging
  Hermes would delay the sweep by the full bound.
- **Retry on any failure.** Re-runs conclusive failures, adding latency for an
  install that is already known to be unusable.

## Testing

`runCommandCheck` is covered directly for all five outcomes. The staged
measurement is driven through `Detect` with injected sub-second bounds and a
helper that stalls, asserting availability outcomes *and* invocation counts, so
"measured twice after a timeout" and "measured once after a nonzero exit" are
distinguishable rather than merely both reported unavailable.