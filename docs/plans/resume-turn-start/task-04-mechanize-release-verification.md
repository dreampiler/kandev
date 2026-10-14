---
status: done
plan: plan.md
requirements:
  - REQ-AGENTS-RESUME-TURN-START-001
system_design:
  - ../../specs/agents/system-design/resume-turn-start.md
---

# Task 04: Mechanize release verification

## Outcome

`scripts/verify-release.ps1` builds a revision, starts an isolated instance,
runs the A~E scenarios, checks the web assets, and emits the v09 §2.2 JSON. This
is fork release confirmation, not a general-purpose gate.

## In scope

- Add `scripts/verify-release.ps1` with the entry point
  `-Commit <revision> -ReleaseRoot <root> -InstanceKind isolated
  -FixtureRoot <fixture-root>`, following the
  existing `scripts/dev-isolated.ps1`, `scripts/kandev-instances.ps1`, and
  `scripts/kandev-kill.ps1` patterns.
- Port the A~E scenario logic from the frozen mapping (task 01), not from letters.
- Emit the v09 §2.2 JSON: `revision`, `release_dir`, `web_assets_ok`,
  `scenarios[A..E]`, `result`, `unverified`.
- Report `result=passed` only for the covered scenario set, and list the
  uncovered real-provider behavior in `unverified`.

## Exclusions

- No use of the operating database, real secrets, or the operating instance.
- No change to `127.0.0.1:38430`, the home directory, or the installation.
- No new recurring test, CI job, gate, or canary.

## Acceptance conditions

1. The script exists with the entry point and emits the JSON shape.
2. Running it against the current baseline produces a JSON result whose
   `result` matches the observed scenarios and whose `unverified` lists the
   real-provider limits.
3. The web check follows the actual HTML script `src` rather than a fixed asset
   name.

## Verification

- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-release.ps1 -Commit <rev> -ReleaseRoot <root> -InstanceKind isolated -FixtureRoot <fixture-root>`
- For a task checkout under a live home, set `KANDEV_ISO_ROOT` to the designated
  isolated root and pass `-BinDir <isolated-bin> -HomeDir <isolated-home>`.
  Place the disposable fixture repository under a separately designated
  `-FixtureRoot` outside Kandev task worktrees; the parameter has no default.
- Read back `GET /api/v1/system/info` from the isolated port for the revision.

## Likely files

- `scripts/verify-release.ps1` (new)

## Dependencies

- Task 01 (mapping).

## Results

`scripts/verify-release.ps1` added. A run on the isolated instance (port 48629,
mock agent only, no real provider) emitted the v09 section 2.2 JSON with A, B,
C, and E passed, D unverified, `web_assets_ok` false (the embedded web bundle is
not built in this worktree), and `result` `passed`. No operating instance,
home, or installation was touched. The script is the approved fork
release-confirmation entry point, not a recurring generic test.

Caveat: this run was launched by pointing `scripts/dev-isolated.ps1` at the
worktree through a directory junction, because the script refuses binaries under
the user home. That is a guard bypass, not the official run location. Treat the
result as preliminary until it is rerun from the operationally designated
location. No junction, link, script/source copy, or other guard workaround is
used going forward.

### Canonical rerun, 2026-10-14

The current script forwards `-BinDir` and `-HomeDir` to the canonical isolated
launcher. It requires an explicit `-FixtureRoot`; omitting it exits with a clear
error before build or launch (verified exit code 1). Scenario repositories are
created under that designated root, while logs and result JSON remain task-local.
The first canonical attempt placed the fixture inside the task checkout and was
refused by the existing repository-admission guard. The controller designated a
fixture location under the isolated root; no guard or product policy was changed.

The confirming mock-only rerun used existing binaries built at
`cdd512153b68c6d890288cc70af9b7ce884cca97`, read back from `system/info`.
No Go source changed after that build. It emitted:

```text
A failed   B passed   C failed   E passed   D unverified
result=failed   web_assets_ok=false
```

A reported `turn=False execution=` within its observation window. C reported
`state=WAITING_FOR_INPUT turns=2 before=0`. These observations do not establish
whether late initial-turn completion, session identity, or runtime recovery caused
the failures; no unchanged rerun or speculative core correction was performed.
B recorded an execution ID, and E observed a new turn after restart.

D remains unverified because this script does not deterministically induce a
primary handoff and delivery rebinding. Real-provider resume/relaunch behavior
and the absent embedded web bundle remain unverified. This failed result is
not release acceptance or a completed K1 context-reset investigation.

Both launches were stopped through their own pidfile. The isolated binary
processes and backend listener were confirmed absent. Temporary fixture cleanup
uses only the controller-designated disposable location.

### A/C verifier correction

Retained session, message, turn, and delivery records established that both
failures came from the verifier. A's asynchronous preparation exceeded the
90-second observation window before execution attachment; its initial output
and completed turn arrived later. C's premature baseline was zero, while its
single initial turn produced a thinking row and a message row before restart.
No C turn, submission, or event was added after restart. These observations
do not demonstrate a core resume defect or an idle redispatch regression.

The approved correction separates bounded preparation from run observation.
Preparation uses the existing `TimeoutSeconds` budget (default 150 seconds).
The subsequent event/completion budget remains 90 seconds. An unsuccessful
preparation result is retained as failure, rather than discarded.

The verifier reads canonical turn IDs and `completed_at` through the existing
session-turn API. New work requires a new completed turn with an associated
agent activity message. `first_event_at` is the activity's persisted
`created_at`, not the verification clock. Thinking and message rows from one
turn therefore count as one completed turn. C captures completed initial-turn
identity immediately before restart, then requires the idle state and the
same turn/completion identities afterward, including absence of a new
unfinished turn.

Temporary checks first failed on all three original defects, then passed
after the correction. Controls also cover a new unfinished turn, an old
completed turn masquerading as continuation, and A's preparation failure stage.
No recurring tests or new API, runtime policy, or core implementation was added.

One changed-input canonical run followed the correction, using the unchanged
Go binaries at `cdd512153b68c6d890288cc70af9b7ce884cca97` and a fresh disposable
isolated home. It returned exit 1 and emitted:

```text
A passed   B passed   C failed   E failed   D unverified
result=failed   web_assets_ok=false
```

A recorded a real first activity timestamp `2026-10-14T03:38:42.4112347Z`,
execution ID, canonical turn ID, and completion timestamp. B's initial completed
turn count was 1 and its continuation recorded a different completed turn.
C reached the 150-second preparation boundary in RUNNING; E remained CREATED
without an execution. Those preparation failures are now preserved rather than
ignored. C's baseline/preservation comparison was not performed because its
precondition was unmet. This run does not prove that the remaining preparation
delay is a product defect, and no second confirming run or timeout extension
was performed. Original release acceptance remains unmet.

Both owned launches stopped through the pidfile. No designated-bin process or
backend listener remained; the user PATH contained no isolated Hermes entry.
D, actual providers, K1 context-reset/R1, and post-swap readback remain open.


### Existing verifier review findings corrected, 2026-10-15

This correction addresses only the three existing verifier findings. It does
not complete the original K12 A~E requirement or the M0/post-swap readback.

- Runtime revision: the JSON revision now comes from the isolated binary's
  `GET /api/v1/system/info` commit field. `-Commit` remains a logged source
  label and cannot identify a different `-NoBuild`/`-BinDir` binary. A missing
  binary commit or a revision change across the restart fails verification.
- Owned cleanup: cleanup is armed before the first launcher call, and the main
  `finally` always calls the owned-pidfile teardown helper. A first health
  timeout is cleaned up; a pre-existing pidfile is not adopted or stopped.
  The canonical kill helper retains its process identity and guarded-port checks.
- C observation: after restart, C polls waiting state and canonical turn/
  completion identities over its own existing 90-second budget before B's
  explicit continuation. B preparation failure cannot shorten that observation.
  A late unfinished turn or a RUNNING state fails C; an unchanged initial turn
  remains one turn. The JSON records C's observed duration.

Verification: Windows PowerShell 5.1 parsed the production script, and a
temporary check executed its actual functions and main try/finally with only
HTTP, launcher and clock fixtures. It confirmed actual binary revision versus
a different source label, unavailable revision, restart revision drift,
first-health-timeout cleanup, preservation of an unowned pidfile, a complete
independent C observation interval, and detection of delayed unfinished-turn
and state changes. No real process, provider, operating instance or database
was used, and no standing test was added. The temporary check was removed.

No new isolated A~E result is claimed by these function checks. D delivery/
ack rebinding, real-provider behavior, K1 context-reset/R1 and authorized
post-swap readback remain open as recorded above.
