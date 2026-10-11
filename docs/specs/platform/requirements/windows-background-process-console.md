---
status: active
system: platform
created: 2026-10-01
owners:
  - kandev
---

# Windows Background Process Console Requirements

## Overview

On Windows, a console program attaches to the console of the process that
starts it. When that process has no console, Windows creates a new console
with a visible window for the child. Kandev can run without a console, for
example when a test harness starts it as a detached process. Every console
helper that it then starts opens a console window that the user did not ask
for.

Platform owns this contract because the helpers belong to several systems
(managed Git, agentctl process management, agent launch, and ACP utilities)
and share one Windows console behavior. Managed console helpers keep their
console attachment while Windows hides newly created console windows, so
console descendants inherit the hidden console.

## Terminology

- **Managed helper process:** A console process that Kandev starts in the
  background and stops together with its owner: a managed Git command,
  agentctl, an agent, script, utility, or code-server editor process started
  by agentctl, a Cursor native MCP command, or the agent process tree of the
  ACP and Codex debug tools.
- **Discovery subprocess:** A one-shot console process that the backend
  starts to read a vendor CLI's version, capability, model catalogue, or
  usage, outside the managed-helper lifecycle. It runs and exits; Kandev
  observes only its output and exit code.
- **Console window:** A terminal window that Windows shows on the desktop for
  a console process.

## Requirements

### REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001: Start managed helper processes without console windows

**Intent:** Background work must not open console windows on the user's
desktop, regardless of how the Kandev process that starts it was started.

**User story:** As a Windows user, I want Kandev's background Git, agent, and
helper processes to stay off the screen, so that running tasks does not open
console windows over my work.

#### Acceptance criteria

- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001.1:** When Kandev starts a
  managed helper process on Windows, the system shall start it without a
  console window, even when the Kandev process that starts it has no console.
- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001.2:** When a caller requests a
  new console for a managed Git command or a Cursor native MCP command, the
  system shall reject the command before it starts.
- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001.3:** When a managed helper
  process runs without a console window, the system shall still pass its input,
  capture its output, and stop it together with its owner.
- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001.4:** When a managed console
  helper runs without a visible console window, it shall keep a console handle
  so that default console descendants inherit the hidden console.

### REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002: Start discovery subprocesses without console windows

**Intent:** Periodic and on-demand discovery must not open console windows on
the user's desktop, because it runs without any user action. A discovery
subprocess that also launches a vendor self-updater must not leave the updater
running from a Kandev probe.

**User story:** As a Windows user, I want Kandev's version, capability, model,
and usage reads to stay off the screen, so discovery does not flash or stack
console windows over my work.

#### Acceptance criteria

- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.1:** When the backend starts a
  discovery subprocess on Windows, the system shall start it without a console
  window, even when the backend process has no console.
- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.2:** When a discovery subprocess
  runs without a visible console window, the system shall still pass its input,
  capture its stdout/stderr, and observe its exit code.
- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.3:** Cancelling or timing out a
  discovery subprocess shall keep its existing behavior: context cancellation
  and bounded timeouts are unchanged, and a failed or timed-out check is still
  reported as unavailable or inconclusive exactly as before.
- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.4:** A hidden discovery
  subprocess shall keep a console handle so a `.cmd` or `.bat` shim's default
  console descendants inherit the hidden console.
- **AC-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-002.5:** A discovery subprocess for a
  vendor CLI that documents an environment opt-out from its background
  self-updater shall run with that opt-out set, so the probe does not start or
  leave a vendor updater running.

## Out of scope

- Interactive terminals that run on a pseudo console.
- Processes that the CLI launcher supervises. They keep the launcher's console.
- Install and update jobs, interactive login, and processes the CLI launcher
  supervises.
- Background commands that Kandev starts with no process attributes and that
  are not discovery subprocesses, such as the `gh` and `glab` helpers, external
  editor launches, and plugin processes.
- Whether an isolated dev or mock Kandev instance executes real vendor CLIs at
  all. That is a discovery-side-effect contract, owned separately; this
  requirement only governs the console behavior of the subprocesses that are
  started.
- The desktop shell's own console and how the desktop shell starts the
  launcher.
