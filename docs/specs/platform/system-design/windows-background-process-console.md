---
status: current
system: platform
requirements:
  - REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001
---

# Windows Background Process Console System Design

## Purpose and boundaries

Platform owns the Windows process setting that managed console helpers share.
Each owning package keeps its process lifetime. [Managed Git
execution](git-subprocess-execution.md) owns Git admission and cleanup, agentctl
owns agent and script processes, and the agent runtime owns the agentctl
launch. This design hides newly created console windows while keeping helpers
attached to a console.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-PLATFORM-WINDOWS-BACKGROUND-CONSOLE-001` | [Console inheritance](#console-inheritance), [Process attributes](#process-attributes), [Excluded process paths](#excluded-process-paths) |

## Console inheritance

A console program started without console flags attaches to its parent's
console. If the parent has no console, Windows creates a console with a visible
window for the child. A process has no console when its parent started it with
`DETACHED_PROCESS`, or when it is a GUI-subsystem program that has not allocated
one. On one Windows 11 machine, a probe started by Node.js 24 `spawn` with
`detached: true` had no console, with or without `windowsHide: true`, and a
GUI-subsystem probe had no console either. The end-to-end backend fixture starts
`kandev __backend` with `detached: true`.

Go's `syscall.SysProcAttr.HideWindow` sets `STARTF_USESHOWWINDOW` and
`SW_HIDE`. For a console program that creates a console, Windows applies this
setting to the new console window. The process remains attached to the console
and can use console APIs. Go's standard-handle setup remains separate, so
`os/exec` pipes continue to carry input and output.

When a managed helper starts from a backend with no console, the helper gets a
hidden console. A console descendant started with default flags attaches to
that same hidden console. This keeps descendants hidden and leaves console
APIs available. `HideWindow` also supplies an initial show state to a GUI
program, so the process paths in this design must remain console-helper paths.

When Windows Terminal is the default terminal application, a new console is
handed to Windows Terminal, which opens a visible window and does not apply the
`SW_HIDE` show state. `HideWindow` alone then opens one terminal window per
helper. For that case `winproc.NoConsoleWindowFlag` adds `CREATE_NO_WINDOW` when
the starting process has no console window (`GetConsoleWindow` returns null).
The helper still gets a console without a window, and a descendant started with
default flags inherits it: on a Windows 11 machine with Windows Terminal as the
default terminal, a probe started with `DETACHED_PROCESS` started a child with
these flags and that child started a default-flag grandchild; the child and
grandchild had a console (`GetConsoleCP` 949, `GetConsoleWindow` null), their
pipes carried output, and no Windows Terminal or OpenConsole process appeared.
Before the change the same machine opened a Windows Terminal window for every
agent that agentctl started from a detached backend. When the starting process
has a console window, the flag is zero and helpers keep attaching to it.

This reverses the earlier decision in
[the background process console plan](../../../plans/windows-background-process-console/plan.md)
to avoid `CREATE_NO_WINDOW`, for the four paths above only. That decision rested
on the documented statement that `CREATE_NO_WINDOW` starts a console program
without a console handle, so default descendants could open their own visible
console. The probe above observed a console for both the flagged child and its
default-flag grandchild, so that risk did not occur on the tested machine.
Managed Git commands keep the earlier `HideWindow`-only behavior.

The inheritance applies to processes that a flagged helper starts itself with
default console flags. It does not make kandev-started helpers share one
console. A `CREATE_NO_WINDOW` console has no window, so agentctl started from a
console-less backend itself reports no console window, and every agent, script,
utility or MCP command that agentctl starts through these paths gets its own
windowless console instead of sharing agentctl's hidden console as before.
Commands that agentctl starts without process attributes, such as `taskkill`
and `tasklist`, still inherit agentctl's windowless console.

When the backend already has a console, the helper attaches to that console.
`HideWindow` does not hide the shared console window. This preserves terminal
visibility for a backend started from a terminal.

## Process attributes

| Package | Function | Windows process attributes |
| --- | --- | --- |
| `internal/common/subproc` | `prepareGitLifecycleCommand` | Caller flags, `HideWindow`, `CREATE_NEW_PROCESS_GROUP`, `CREATE_SUSPENDED` |
| `internal/agent/mcpconfig` | `prepareNativeMCPProcess` | Caller flags, `HideWindow`, `CREATE_NEW_PROCESS_GROUP`, `CREATE_SUSPENDED`, `NoConsoleWindowFlag` |
| `internal/agentctl/server/utility` | `setACPCommandProcAttr` | `HideWindow`, `CREATE_NEW_PROCESS_GROUP`, `CREATE_SUSPENDED`, `NoConsoleWindowFlag` |
| `internal/agentctl/server/process` | `setProcGroup`, `setManagedProcGroup`, `setAgentProcGroup` | `HideWindow`, `CREATE_NEW_PROCESS_GROUP`, `NoConsoleWindowFlag`, plus `CREATE_SUSPENDED` for managed processes (scripts, piped commands, code-server) and agents |
| `internal/agent/runtime/agentctl/launcher` | `buildSysProcAttr` | `HideWindow`, `CREATE_NEW_PROCESS_GROUP`, `CREATE_SUSPENDED`, `NoConsoleWindowFlag` |
| `internal/agent/acpdbg`, `internal/agent/codexdbg` | `configureProcessTree` | `HideWindow`, `CREATE_NEW_PROCESS_GROUP`, `CREATE_SUSPENDED` |

`prepareGitLifecycleCommand` and `prepareNativeMCPProcess` reject a caller
that sets `CREATE_NEW_CONSOLE` before the process starts. This preserves the
existing console-ownership contract for these managed commands.

Each process in the table is bound to a kill-on-close Job Object that its owner
holds. A suspended process is bound before it resumes. If binding the agentctl
process fails, the launcher logs a warning and continues, as before.

`CREATE_NEW_PROCESS_GROUP` continues to disable Ctrl+C for the helper group.
No Kandev code calls `GenerateConsoleCtrlEvent`. With a detached backend, the
helper's hidden console is separate from the backend. With a terminal-run
backend, the helper shares the terminal console as it did before. On Windows,
Go's `os.Process.Signal` supports only `os.Kill` and returns `EWINDOWS` for
`os.Interrupt`, so the agentctl launcher's graceful stop already falls back to
kill.

Graceful `taskkill` without `/F` remains the first stop attempt for process
groups that use it. Without `/F`, `taskkill` asks the process to close through
a window. A helper started with `HideWindow` from a console window keeps a
hidden console window. A helper started with `CREATE_NO_WINDOW` has a console
but no window, so for these helpers the graceful `taskkill` is expected to fail
with "can only be terminated forcefully" or have no effect. This has not been
measured. The agentctl stop path already handles both outcomes: it first waits
for the agent to exit after stdin closes, then on a `taskkill` error or after
`processGroupTerminateGrace` it force-kills the process tree, and the Job
Object stays the final cleanup. The expected effect is that the `WM_CLOSE`
step is lost for helpers that do not exit from stdin close, and those helpers
are force-killed after the grace period instead of closing on request. The
cooperative backend shutdown signal does not cover this path; it is between
the launcher and the backend only.

A graceful replacement would send `CTRL_BREAK_EVENT` to the helper's process
group with `GenerateConsoleCtrlEvent`. That call only reaches processes on the
caller's console, so agentctl would have to attach to each helper's console in
turn, or helpers would have to share agentctl's windowless console instead of
receiving `CREATE_NO_WINDOW`. Both change console ownership and need process
tests on Windows, so they are left as a follow-up. A helper can still ignore
or fail to handle a close request in any case, so owners must keep their
existing wait, Job Object, and forced-termination paths. The detached parent
regression test checks that a managed Git command and its default console
descendant use the same hidden console window; it does not claim that every
helper exits gracefully.

## Excluded process paths

- `internal/agentctl/server/shell` prepares the embedded shell for
  `pty.StartWithSize`, which is an interactive pseudo-terminal session.
  `configureShellProcess` keeps its flags.
- `internal/common/ptyexec` starts interactive terminals through ConPTY. ConPTY
  creates the process itself and does not read `SysProcAttr`.
- `internal/launcher` supervises processes that have no Job Object. They keep
  the launcher's console, so closing the terminal still delivers the console
  close event to them.
- Commands started without process attributes inherit the console of the
  process that starts them. These include `gh`, `glab`, external editor
  launches, plugin processes, and the `taskkill` and `tasklist` calls that
  agentctl and the CLI launcher make.

## Verification

Existing Windows build-tagged tests assert `HideWindow`, the original process
flags, and the conditional `CREATE_NO_WINDOW` bit for these four helper paths.
Both console-probe outcomes can be checked with a temporary overlay without
opening a process or depending on the test runner's console. No new standing
test or CI step is required.

The existing detached-parent regression test targets managed Git commands,
whose flags are unchanged. It checks that the managed Git helper and its
default console descendant share a hidden console window; it does not prove
the same console ownership or graceful-close behavior for windowless helpers.
Existing Windows lifecycle tests cover start, output capture, and Job Object
cleanup for Git, agentctl processes, and ACP utility commands. Existing CI and
`make test-windows` selections remain unchanged.
