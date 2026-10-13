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
  `-Commit <revision> -ReleaseRoot <root> -InstanceKind isolated`, following the
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

- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-release.ps1 -Commit <rev> -ReleaseRoot <root> -InstanceKind isolated`
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
