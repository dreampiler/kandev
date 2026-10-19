---
id: "01-no-console-window-flag"
title: "Add CREATE_NO_WINDOW when no console window exists"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001
acceptance_criteria:
  - AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001.5
system_design:
  - ../../specs/platform/system-design/windows-background-process-console.md
---

# Task 01: Add CREATE_NO_WINDOW when no console window exists

## Summary

`winproc.NoConsoleWindowFlag` returns `CREATE_NO_WINDOW` when
`GetConsoleWindow` returns null and zero otherwise. The agentctl launcher,
agentctl process attributes, ACP utility commands and the Cursor native MCP
command add it to their creation flags.

## Verification

- `go test ./internal/common/winproc/ -run NoConsoleWindow` covers both branches.
- Manual probe on Windows 11 with Windows Terminal as the default terminal:
  detached parent, child with these flags, default-flag grandchild; both
  descendants had a console and no Windows Terminal or OpenConsole process
  appeared.
