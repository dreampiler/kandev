---
id: "02-isolated-mock-discovery-side-effects"
title: "Isolated dev/mock discovery side effects"
status: active
wave: 2
depends_on: []
plan: "plan.md"
requirements:
  - REQ-LAUNCHER-ISOLATED-SCRIPTS-002
system_design:
  - ../../specs/launcher/system-design/isolated-scripts.md
---

# Task 02: Isolated dev/mock discovery side effects

## Summary

The approved isolated dev/mock entrypoint (`scripts/dev-isolated.ps1`) sets an
isolated `LOCALAPPDATA`/`USERPROFILE` but inherits the real `PATH` and sets
`KANDEV_MOCK_AGENT=true`, so the backend loads real inference agents and runs real
vendor discovery/version probes. A vendor CLI's own self-installer can then write
the real HKCU user `PATH`/registry. This work order keeps the isolated dev/mock
path from discovering or executing real provider CLIs, without changing
production discovery or the user's configuration.

## Evidence

- `scripts/dev-isolated.ps1` sets `KANDEV_MOCK_AGENT=true`.
- `docs/test_e2e_web.md` documents `KANDEV_MOCK_AGENT=only` as "only loads mock
  agent, skips agent discovery".
- `internal/agent/registry/provider.go` loads all real agents in `true` mode;
  `internal/agent/discovery/host_cli.go` runs `hostcli.ExecRunner` per available
  agent. Kandev has no Hermes auto-install code.
- Operations removed four permanent HKCU user-`PATH` entries pointing into
  isolated Hermes homes and owns its `user-path-backup-20261011.txt`. This task
  must not change or repeat that cleanup.

## In scope

- Reuse the existing `KANDEV_MOCK_AGENT=only` selection for the isolated/mock
  default so real-agent discovery is skipped, with an explicit opt-in for a
  developer who deliberately wants real-agent probing in the isolated instance.
- Optional: the equivalent Unix `scripts/dev-isolated` parity, if the same
  contract applies.
- A requirement under the isolated-scripts owner and a source-adjacent
  PowerShell test for the mode/parameter selection.

## Out of scope

- Production discovery, user `PATH`/file associations/settings, provider
  installation, or any registry/permission gate.
- Restoration of the operating user environment (Operations-owned).

## Acceptance

- The default isolated/mock launch does not execute real provider CLIs.
- An explicit opt-in still allows real-agent probing.
- Production discovery is unchanged.

## Verification

- One finite corrected isolated run (`scripts/dev-isolated.ps1`, hidden console,
  owned pidfile, then `scripts/kandev-kill.ps1`) with ONE before/after HKCU
  user-`PATH` comparison reporting equality and the affected-entry count only (no
  raw `PATH` dump, no hash or fingerprint, no recurring audit).
- Prove any chosen harmless probe is inert rather than assuming it.

## Results

- Implemented: `scripts/isolated-instance.psm1` `Get-IsolatedMockAgentMode`
  (defaults `only`, `-RealAgents` returns `true`); `scripts/dev-isolated.ps1`
  gains a `-RealAgents` switch and passes the helper result as
  `KANDEV_MOCK_AGENT`. Requirement `REQ-LAUNCHER-ISOLATED-SCRIPTS-002` added.
- Unit-verified: `scripts/tests/isolated-instance.Tests.ps1` asserts the default
  is `only` and `-RealAgents` is `true`; the full PowerShell suite passes with a
  clean TEMP (this environment's `%TEMP%` contains a stray `.git`, unrelated to
  this change, which trips the existing home guard).
- End-to-end isolated run: BLOCKED by an approved-entry prerequisite. The
  canonical `scripts/dev-isolated.ps1` refuses a binary under
  `%USERPROFILE%\.kandev`, and this W100 worktree lives at
  `C:\Users\dream\.kandev\tasks\...`; the script refused, and no bypass was
  attempted. The requested one before/after user-`PATH` comparison was not
  meaningfully performed because no isolated instance launched. Needs an
  approved entry from outside `.kandev` (or the Operations owner).

## Non-goals / known gaps

- The Unix `scripts/dev-isolated` helper keeps its current `KANDEV_MOCK_AGENT=true`
  default; this work order scoped the change to the observed Windows helper.
