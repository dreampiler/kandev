---
status: current
system: agents
requirements:
  - REQ-AGENTS-INSTALL-DETECTION-002
---

# Agent capability install status system design

## Purpose and boundaries

Two records answer "is this agent installed?". `internal/agent/discovery`
sweeps every agent's `IsInstalled` under a 15s budget, caches the result for
30s, and serves `/agents/discovery` and `/agents/available`.
`internal/agent/hostutility` keeps one capability record per ACP agent; its
`capability_status` drives agent badges, the profile status panel, and picker
warnings, and reaches the UI through `/agents` and the `agent.available.updated`
snapshot.

Before this design the host utility measured installation itself, once, during
boot, and kept a `not_installed` answer until an install job or a manual
refresh replaced it. Boot is when detection is most contended: bootstraps,
the first discovery sweep, and runtime probes all start together. One check
that lost that race therefore left the capability record saying "not installed"
for the life of the process while discovery, re-measuring every 30s, already
reported the agent available.

This design makes the capability record follow discovery. Detection itself
(`IsInstalled`, its bounds, and the sweep budget) is unchanged.

## Components

| Surface | Owner |
| --- | --- |
| `discovery.Registry.Detect` | Shares one in-flight sweep between concurrent callers |
| `discovery.Registry.AgentAvailability` | One agent's answer from the cached sweep |
| `discovery.Registry.OnSweep` | Notifies listeners of every sweep that becomes the cache |
| `hostutility.InstallationSource` | The installation answer the host utility consults |
| `hostutility.Manager.RecheckNotInstalled` | Re-measures `not_installed` records discovery contradicts |
| `controller.connectInstallationStatus` | Wires the two together and publishes changes |

## Installation answer

`Manager.agentInstalled` asks the installation source first. A known answer is
final. When the source has no answer (the agent is not in the sweep because its
detection errored or did not finish within the budget) or the source errors,
the manager runs the agent's own `IsInstalled`, which is the previous behavior.
Bootstrap and lazy instance creation both use this path, so boot reads the same
sweep the discovery endpoints serve instead of running a second, concurrent
check of the same agent.

## Shared sweep

`Detect` keys a `singleflight` group on the cache generation the caller
observed. Callers that miss the cache together wait on one sweep. A caller
arriving after `InvalidateCache` uses a new key and starts a fresh sweep, so it
never receives results the invalidation superseded. The sweep runs detached
from the caller that started it (`context.WithoutCancel`), bounded by the 15s
sweep budget; a caller that cancels gets `ctx.Err()` while the sweep finishes
for the others and still populates the cache.

## Re-measurement

```
discovery sweep published ── OnSweep ──► RecheckNotInstalled(available names)
                                               │  status == not_installed only
boot finished ── after 60s ──► recheck every not_installed agent
                                               │
                                     recheckInstall (singleflight per agent)
                                               │
                         agentInstalled? ── no ──► unchanged, no notification
                                               │ yes
                                       Refresh (instance + probe)
                                               │
                         status != not_installed ──► capability change listener
                                                       └► BroadcastAvailableAgents
```

The delayed pass exists because a sweep only happens when something asks for
discovery. 60s exceeds the 30s cache TTL, so the delayed pass reads a fresh
sweep rather than the boot one.

Re-measurement cannot loop. The listener publishes only when a record leaves
`not_installed`. Publishing reads the discovery cache; if that read starts a new
sweep, the record is no longer `not_installed`, so the sweep triggers nothing.

## Lifecycle

Background work runs on a manager-owned context. `goBackground` refuses to
start once the manager is stopped, and `Stop` marks the manager stopped,
cancels that context, and waits for the work before deleting instances, so no
re-measurement outlives the manager or recreates an instance after shutdown.

## Virtual families

Discovery skips virtual families such as Dynamic because they have no
executable to detect. The settings menu used "absent from discovery" as "not
installed", which badged Dynamic. The menu now exempts the Dynamic family; the
agents page already renders it through its own card. `/agents/available` keeps
reporting Dynamic as unavailable, because nothing about it is installed; only
the presentation that read that as a missing CLI changes.

## Invariants

- A known discovery answer and the capability record never disagree for longer
  than one re-measurement.
- Only `not_installed` records are re-measured; `failed`, `auth_required`, and
  `ok` records are left to their existing refresh paths.
- An agent that is still not installed publishes nothing.
- No background re-measurement runs after `Stop` returns.

## Alternatives rejected

- **Periodic re-measurement of every agent.** Spawns probes for agents nobody
  installed, forever, to fix records that discovery already corrects.
- **Report the boot timeout as a separate status.** Requires a new detection
  result shape across every agent and still leaves the record stale until
  something re-measures it.
- **Report Dynamic as available.** It would appear among detected CLIs and blur
  what availability means for every consumer of `/agents/available`.

## Testing

Discovery tests cover the shared sweep (one `IsInstalled` call for concurrent
callers), caller cancellation without cancelling the sweep, `OnSweep` firing
only for published sweeps, and `AgentAvailability` for known, missing, virtual,
and unregistered agents. Host utility tests drive bootstrap through a fake
installation source, re-measurement on discovery, the delayed pass, the
status filter, and cancellation on `Stop`, against loopback agentctl stand-ins.
A controller test runs the real discovery registry and host utility together
and asserts that a sweep finding the agent moves its record out of
`not_installed`. The settings menu test covers the Dynamic exemption.
