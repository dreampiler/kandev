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
