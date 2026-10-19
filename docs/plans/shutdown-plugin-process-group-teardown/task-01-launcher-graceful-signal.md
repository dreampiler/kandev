---
id: "01-launcher-graceful-signal"
title: "Signal launcher roots before force cleanup"
status: done
wave: 1
depends_on: []
plan: "plan.md"
spec: "../../specs/platform/requirements/shutdown-log-noise.md"
parallelism: sequential
---

# Task 01: Signal launcher roots before force cleanup

## Intent

Prevent Ctrl+C from directly terminating backend-owned plugin processes before
the backend can run its plugin cleanup. On platforms with safe root-only
signaling, the first graceful signal targets the supervised root process. The
existing process-group force path remains the fallback for grace expiry and
repeated interrupts; platforms with unsafe wrapper processes retain the
tree-wide graceful fallback.

## Acceptance

- On supported platforms, `managedProcess.kill` sends the first graceful
  termination signal only to the supervised root process. For the Unix dev
  make wrapper, it targets the backend PID written by the backend binary. A
  remaining descendant in the same process group is not directly signalled
  during that phase.
- If graceful shutdown does not complete before the existing grace period, or
  the launcher receives a second signal, process-group force cleanup still
  terminates the complete supervised tree, including descendants after root
  exit, where process-group support is available.
- The existing plugin adapter policy remains conservative: deliberate plugin
  termination is not logged at ERROR, while an unexpected active-runtime exit
  remains visible at ERROR/WARN through the existing paths. No broad log-level
  suppression is added.
- No change occurs to shutdown timeout, exit-code mapping, plugin lifecycle or
  restart policy, task/session state, or WS responses.

## Verification

From the repository root:

```bash
cd apps/backend && go test ./internal/launcher ./internal/plugins/runtime
```

If platform-specific launcher tests are unavailable on the current host, run
the available Unix/Linux package tests and record the unverified platform in
`## Results`.

## Files likely touched

- `apps/backend/internal/launcher/process.go`
- `apps/backend/internal/launcher/process_group_unix.go`
- `apps/backend/internal/launcher/process_group_windows.go`
- `apps/backend/internal/launcher/process_group_default.go`
- `apps/backend/internal/launcher/process_group_unix_test.go`
- `apps/backend/internal/launcher/process_group_windows_test.go` (if needed)
- `apps/backend/cmd/kandev/main.go` and `main_test.go`
- `apps/backend/internal/plugins/runtime/hclog_adapter.go` and its test only
  if focused verification proves the existing policy is still bypassed

## Dependencies and risks

No external dependencies. The main platform risk is preserving descendant
cleanup on Windows, where the dev backend is launched through `make` without a
POSIX `exec`; the implementation therefore retains Windows tree-wide graceful
termination.

## Results

- `go test -race ./internal/launcher -run
  'TestManagedProcessKillSignalsOnlyRootBeforeForceKill|TestAttachSignalsSecondSignalForceKillsChildren' -count=1`
  passed, including descendant cleanup after root exit.
- `go test ./internal/launcher -run
  'TestManagedProcessKill(SignalsOnlyRootBeforeForceKill|TargetsBackendPIDFile)$' -count=1`
  passed (2 tests).
- `go test ./cmd/kandev -run '^TestDispatchWritesBackendPIDFile$' -count=1`
  passed.
- The process-tree assertion treats a reaped Linux zombie as terminated, which
  avoids false leak failures while container init reaps force-killed children.
- `go test ./internal/plugins/runtime -run '^TestHCLogAdapter' -count=1`
  passed (6 tests).
- `go test ./internal/launcher ./internal/plugins/runtime` passed (227 tests).
- `GOOS=windows GOARCH=amd64 go test -c` passed for `./internal/launcher` and
  `./cmd/kandev`.
- `golangci-lint run ./internal/launcher ./cmd/kandev --timeout=5m` passed
  (0 issues).
- `git diff --check` passed.

Unix and Darwin use root-only graceful signaling, with the Unix dev make
wrapper targeting the backend PID handoff. Windows retains the existing
tree-wide graceful fallback because signaling only that wrapper could orphan
descendants. Process-group cleanup runs after root exit on supported platforms.
The plugin adapter was not modified: its existing DEBUG-for-deliberate-stop and
ERROR-for-unexpected-exit policy passed the focused tests.

### Windows cooperative shutdown follow-up (2026-10-19)

The console-less Windows child regression reproduced a normal-stop defect:
`taskkill /T` failed, the existing force fallback exited the child with code 1,
and its retained inference owner remained active. The same isolated owner
closed normally when its ordered teardown ran. This establishes a source
failure mode; it does not prove which internal stop branch a past operating
Desktop took.

Desktop now opens the still-owned launcher's local shutdown event. The
launcher opens the corresponding backend event. Each recipient bridges one
event into its existing signal-driven teardown; repeated event requests do
not become the second signal that forces exit. Receivers own and drain the
waiter and both event handles. Failed duplicate registration releases its
handle. Senders cannot create an event or mark a journal settled. Existing
wrapper fallback, grace deadlines, second-signal handling, and forced cleanup
remain available. Backend detach and inference ownership rules are unchanged.

After cleanup reports an error, the Windows launcher checks the root process
handle to distinguish confirmed exit from localized taskkill output. An
inaccessible or live process remains a failure. This retains the existing
missing-root semantics and does not add a guarantee about orphan descendants.

Focused temporary overlays, with no permanent test additions, established:

- Original real-child stop: RED, active owner, forced exit (0.615s).
- Existing exited-PID regression: RED on localized taskkill output (2.334s).
- Corrected real-child stop and actual Rust sender: closed owner, exit 0;
  managed stop reports graceful with no force. Launcher race checks passed
  (7.566s), backend startup signal bridge passed (1.236s), event drain passed
  (1.044s).
- Rust library compilation and the existing Desktop shutdown-budget test
  passed with and without `desktop-runtime`, with the unchanged lockfile.
- Duplicate event registration/drain and the existing corrupt-owner,
  foreign-token, prepared-versus-started finalization, and consume/cancel race
  checks passed (winproc 1.036s, utility 1.074s).
- Normal commit hooks are recorded in the accompanying source handoff.

Already-retained active owners are not adopted or rewritten by this change.
Their exact predecessor settlement remains a separate recovery prerequisite.
The changed Desktop shell and native launcher/backend must travel together;
a six-binary native-only replacement is not sufficient for this follow-up.
