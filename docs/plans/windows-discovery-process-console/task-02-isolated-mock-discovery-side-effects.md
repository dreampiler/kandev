---
id: "02-isolated-mock-discovery-side-effects"
title: "Isolated dev/mock discovery side effects"
status: registered
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

- Not started; registered from W100 scope additions.
