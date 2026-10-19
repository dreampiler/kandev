---
created: 2026-10-20
status: done
requirements:
  - REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001
system_design:
  - ../../specs/platform/system-design/windows-background-process-console.md
legacy_specs: []
---

# Implementation Plan: Keep helper consoles hidden under Windows Terminal

## Overview

With Windows Terminal as the default terminal application, a helper started
with `HideWindow` from a backend that has no console opens a visible terminal
window, because Windows Terminal does not apply the hidden show state to a new
console. Add `CREATE_NO_WINDOW` only when the starting process has no console
window, so helpers keep a windowless console that descendants inherit.

## Scope

In scope: `internal/common/winproc` `NoConsoleWindowFlag` and its use in the
agentctl launcher, agentctl process attributes, ACP utility commands and the
Cursor native MCP command.

Excluded: managed Git commands (unchanged), the ACP and Codex debug tools, and
any process path that already shares a console window.

## Work orders

- [x] [Task 01: Add CREATE_NO_WINDOW when no console window exists](task-01-no-console-window-flag.md)

## Changed-path coverage

The work order owns the winproc helper and its tests, the four process
attribute sites, and `AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001.5`.
