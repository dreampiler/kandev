---
status: draft
system: agents
created: 2026-10-02
owners:
  - kandev
---

# Agent install detection bounds

## Overview

Kandev answers "is this agent installed?" by running each agent's `IsInstalled`
probe. Several probes execute a real capability check, and discovery runs every
agent's probe concurrently under one overall budget. A probe that is bounded too
tightly therefore reports an installed agent as missing whenever unrelated
checks compete for the same machine, even though the agent itself is healthy.

## Requirements

### REQ-AGENTS-INSTALL-DETECTION-001: Honest install detection under concurrent load

**Intent:** An installed agent is reported available unless its own check
actually fails. Contention from a concurrent detection sweep must not turn a
healthy installation into a "not installed" result, and a conclusive failure
must not be re-run, because the extra attempt would add latency for an install
that is already known to be unusable.

#### Acceptance criteria

- **AC-AGENTS-INSTALL-DETECTION-001.1:** A capability check that fails because
  its bound elapsed before the command answered is distinguishable from a check
  that failed because the command answered with a nonzero exit.
- **AC-AGENTS-INSTALL-DETECTION-001.2:** Only a caller cancellation is reported
  as a discovery error. A missing binary, a nonzero exit, and an elapsed bound
  all report the agent as unavailable without an error.
- **AC-AGENTS-INSTALL-DETECTION-001.3:** When the first bound elapses, the agent
  whose check is measurably slower than the shared bound measures once more at a
  larger bound, and that second measurement is final.
- **AC-AGENTS-INSTALL-DETECTION-001.4:** A missing binary and a nonzero exit are
  conclusive and are never re-measured.
- **AC-AGENTS-INSTALL-DETECTION-001.5:** The bounds used by any single agent's
  detection remain inside the overall discovery budget.
- **AC-AGENTS-INSTALL-DETECTION-001.6:** Agents other than the one being repaired
  keep their existing detection behavior, including their existing bounds.
- **AC-AGENTS-INSTALL-DETECTION-001.7:** No new configuration, setting, or
  exported API is introduced to control a detection bound.

### REQ-AGENTS-INSTALL-DETECTION-002: One installation answer across surfaces

**Intent:** An agent that discovery reports installed is never shown as "not
installed" by the capability record that drives agent badges, profile panels,
and pickers. One failed measurement, typically taken at boot under load, must
not outlive the next successful one, and a virtual agent family that has no
executable is never presented as an uninstalled CLI.

#### Acceptance criteria

- **AC-AGENTS-INSTALL-DETECTION-002.1:** The host utility decides whether an
  agent is installed from the same discovery sweep the discovery endpoints
  serve. It runs the agent's own detection only when that sweep has no answer
  for the agent.
- **AC-AGENTS-INSTALL-DETECTION-002.2:** Every discovery sweep that reports an
  agent available re-measures that agent's capability when its record says
  `not_installed`. Records in any other status are left alone.
- **AC-AGENTS-INSTALL-DETECTION-002.3:** Agents that boot left `not_installed`
  are measured once more after a delay longer than the discovery cache TTL, so
  the record converges even when no page triggers a sweep.
- **AC-AGENTS-INSTALL-DETECTION-002.4:** A re-measurement that moves an agent
  out of `not_installed` publishes the available-agents snapshot so open pages
  update without a reload. An agent that is still not installed publishes
  nothing.
- **AC-AGENTS-INSTALL-DETECTION-002.5:** Concurrent callers that miss the
  discovery cache share one sweep, and a caller that leaves does not cancel the
  sweep the others are waiting on.
- **AC-AGENTS-INSTALL-DETECTION-002.6:** The settings menu never marks the
  virtual Dynamic family as not installed.
- **AC-AGENTS-INSTALL-DETECTION-002.7:** Stopping the host utility cancels and
  waits for every background re-measurement, and none starts afterwards.

## Migrated source detail

## Why

`WithCommandCheck` bounded every capability check at one shared timeout and
collapsed a local timeout into the same `(false, "", nil)` result as a nonzero
exit. The caller could not tell the two apart, so it had no way to distinguish an
unhealthy install from a slow measurement.

## What

- A capability check reports whether it succeeded, whether its bound elapsed
  before the command answered, and whether the caller cancelled it.
- Only a caller cancellation is an error.
- An agent whose check is measurably slower than the shared bound measures once
  more, at a larger bound, only after the first bound elapsed.
- A missing binary and a nonzero exit stay conclusive and are not re-measured.
- The second measurement keeps the pair of bounds inside the discovery budget.
- Every other agent keeps its current detection path and bound.
- Detection bounds are compile-time choices, not user configuration.

## Measured evidence

On the reporter's host, `hermes acp --check` exits 0 in about 2.0s when run
alone, about 3.0s at 8-way concurrency, and 5.5s to 12.1s at the 24-way
concurrency that a full discovery sweep produces. The shared bound is 5s, so a
healthy Hermes was reported as not installed on every sweep, consistently rather
than intermittently.

## Failure modes

- If the second measurement also fails, the agent is reported unavailable; a
  genuinely unusable or hanging install is never reported as available.
- If the caller cancels during either measurement, discovery surfaces the
  cancellation error as before.
- If the overall discovery budget ever tightens below the sum of an agent's
  bounds, that agent's bounds must be revisited so the sweep stays bounded.

## Out of scope

- Changing the discovery budget or the concurrency of the detection sweep.
- Raising or otherwise changing the bound for any agent other than the one
  being repaired.
- New configuration, settings, or an exported API for detection bounds.
- Replacing capability checks with a faster probe, or detecting installation
  through package metadata instead of the agent's own check.

## Implementation plan

[Agent install detection bounds](../../../plans/agent-install-detection-bounds/plan.md)

[Agent capability install status](../../../plans/agent-capability-install-status/plan.md)