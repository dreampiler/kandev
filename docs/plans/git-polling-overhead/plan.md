---
created: 2026-10-12
status: planned
requirements:
  - REQ-PLATFORM-GIT-SUBPROCESS-ADMISSION-002
  - REQ-PLATFORM-GIT-CAPTURE-RECOVERY-001
  - REQ-WORKSPACES-LOCAL-REPOSITORIES-004
  - REQ-PLATFORM-BOUNDED-TASK-STATUS-DELIVERY-001
system_design:
  - ../../specs/platform/system-design/git-subprocess-execution.md
  - ../../specs/platform/system-design/workspace-git-capture-recovery.md
  - ../../specs/workspaces/system-design/local-repositories.md
  - ../../specs/platform/system-design/bounded-task-status-delivery.md
legacy_specs: []
---

# Implementation plan: Git and polling overhead

## Overview

Four independent reductions of repeated background work. Each work order has
its own result and verification and lands as its own commit, so the orders can
ship together or separately.

1. Git executable resolution walks `PATH` for every managed Git command.
2. Each Changes capture spawns four `rev-parse --verify` processes per evidence
   read, and every capture reads its evidence twice.
3. A slow-mode tracker scans every 30 seconds while nothing changes.
4. The task status summary projector re-reads the launch queue on every event,
   including per-message and per-turn traffic.

## Evidence and confirmed scope

Baseline is upstream `main` at `add376a2af17753ec349d9e7b042306c0f91100b`.
Source reading at that commit confirms each cost:

- `subproc.NewGitCommand` calls `exec.CommandContext(ctx, "git")`, which runs
  `exec.LookPath` per command. `GitExecutablePath` also calls `LookPath`. On
  Windows each lookup stats `PATH` x `PATHEXT` candidates. A downstream Windows
  deployment reported `os/exec.LookPath` at about 1.65 cores in a 20-second
  agentctl CPU profile; that number is reported, not re-measured here.
- `captureGitStatusComparisonEvidence` calls `gitRefOID` four times, each a
  separate `rev-parse --verify`. `validateBasicGitStatusEvidence` repeats the
  read. `gitRefOID` never returns an error, so a batch can keep the same
  error contract.
- `pollIntervals(PollModeSlow)` returns a fixed 30 seconds for both loops.
  Upstream already pauses idle sessions (`AC-WORKSPACES-LOCAL-REPOSITORIES-004.4`),
  so the remaining cost is unfocused trackers with an operation in flight and
  slow UI interest. A downstream 32-tracker slow-mode measurement over 420
  seconds reported process CPU 1.02 -> 0.51 cores; reported, not re-measured.
- `Projector.handleEvent` calls `loadLaunchQueue` for every event. The
  production loader reads the task row and samples the install-wide session
  ceiling. Ceiling deferral writes and the ceiling sweep's still-deferred path
  publish `task.updated`, so the queue and its capacity sample already arrive
  through task events.

No open upstream pull request changes the production files in scope.

## Contract changes

- `AC-PLATFORM-GIT-SUBPROCESS-ADMISSION-002.7` (new): reuse the resolved Git
  executable while `PATH` is unchanged and the file exists.
- `AC-PLATFORM-GIT-CAPTURE-RECOVERY-001.7` (new): one process per comparison
  evidence read, with per-reference fallback that keeps published values equal.
- `AC-WORKSPACES-LOCAL-REPOSITORIES-004.6` (new): changes the documented
  unfocused cadence. The previous design fixed both loops at 30 seconds; the
  new rule lets an unchanged loop lengthen to 5 minutes and restores 30 seconds
  on a change or mode push. This is a visible freshness trade-off for an
  unfocused sidebar badge; focused clients, turn-end final scans, and paused
  behavior are unchanged.
- `AC-PLATFORM-BOUNDED-TASK-STATUS-DELIVERY-001.15` (new): closed launch-queue
  refresh set for the status summary projector.

The Workspace Git Status requirement and design are within 100 bytes of their
size limits, so the comparison-reference rule lives in the capture recovery
pair, which owns the capture and validation evidence reads.

## Scope

### In scope

- `internal/common/subproc/git_command.go` executable memo and its unit tests.
- `agentctl/server/process` comparison reference batching, slow-mode backoff,
  and their unit tests.
- `task/statussummary` launch-queue refresh gating and its unit tests.
- The four requirement criteria, design sections, this plan, and work orders.

### Out of scope

- Admission classes, deadlines, noninteractive environment preparation, or the
  raw Git audit.
- Fast-mode and paused intervals, the startup grace, mode aggregation, focus
  handling, and turn-end final scans.
- Session ceiling admission, sweep cadence, deferral records, or queue UI.
- New metrics, flags, settings, frameworks, integration or E2E suites.

## Work orders

| Order | Wave | Depends on | Result |
| --- | --- | --- | --- |
| [Task 01](task-01-remember-git-executable.md) | 1 | none | Resolved Git path reused per `PATH` |
| [Task 02](task-02-batch-comparison-refs.md) | 1 | none | One `rev-parse` per evidence read |
| [Task 03](task-03-slow-poll-idle-backoff.md) | 1 | none | Unchanged slow loops back off |
| [Task 04](task-04-launch-queue-refresh-sources.md) | 1 | none | Launch queue re-read only by its sources |

- [ ] [Task 01: Remember the resolved Git executable](task-01-remember-git-executable.md)
- [ ] [Task 02: Resolve comparison refs in one Git process](task-02-batch-comparison-refs.md)
- [ ] [Task 03: Back off unchanged slow-mode polling](task-03-slow-poll-idle-backoff.md)
- [ ] [Task 04: Refresh the launch queue only from its sources](task-04-launch-queue-refresh-sources.md)

## Verification strategy

Each order adds source-adjacent unit tests that fail on the baseline and pass
after its change, and keeps its existing neighbouring tests green. Commands
run only the named tests with `-run`; package-wide runs are not part of this
package. Backend `go build`, `go vet`, and scoped lint run once on the changed
packages. Documentation checks run once:

```bash
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

Run `.github/scripts/pr-docs.cjs` `validateCoverage` locally with the actual
changed paths before publication.

## E2E, docs, and mobile assessment

All four orders are backend-internal. No API, payload, copy, setting, or UI
changes, so no ASCII preview, public docs, or browser/E2E evidence is needed.
Desktop and phone consume the same summaries and Git snapshots.

## Risks

- Task 01: a stale remembered path after an in-place upgrade is bounded by the
  stat check; a test that swaps `PATH` resolves again.
- Task 02: `rev-parse` without `--verify` echoes unresolvable arguments; only
  a zero exit with one object ID per argument is accepted.
- Task 03: an unfocused badge can lag a change by up to 5 minutes after a long
  quiet period. Maintainers may prefer a different cap; the constants are local.
- Task 04: a new subscribed event that can change the queue must be added to
  the closed refresh set.
