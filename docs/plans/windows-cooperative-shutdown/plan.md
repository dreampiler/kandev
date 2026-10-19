---
created: 2026-10-20
status: done
requirements:
  - REQ-DESKTOP-DESKTOP-TAURI-APP-001
system_design:
  - ../../specs/desktop/system-design/windows-cooperative-shutdown.md
legacy_specs: []
---

# Implementation Plan: Windows cooperative shutdown before forced cleanup

## Overview

On Windows the desktop shell starts the launcher without a console, and the
launcher starts the backend without one. Both parents ask their child to stop
with `taskkill /T` without `/F`, which only posts a window-close message. A
console-less child cannot receive it, so the request fails, the parent falls
through to forced tree cleanup, and the backend exits without running its
ordered teardown. One sequential work order delivers a cooperative stop request
through the existing signal-driven teardown before any forced cleanup.

## Scope

In scope: the new `apps/backend/internal/common/winproc/shutdown_*.go` event
helpers, the launcher signal handler and Windows `terminateManagedProcess`, the
backend startup signal controller, and the desktop shell's Windows
`request_graceful_child_shutdown` with its new `windows_shutdown.rs` sender.

Excluded: grace deadlines, second-signal forced exit, Unix and macOS signaling,
the development `make` wrapper path, and taskkill output localization.

## Technical approach

Follow [Windows cooperative backend stop](../../specs/desktop/system-design/windows-cooperative-shutdown.md#stop-request).
Each receiver registers a per-PID named event and forwards one `os.Interrupt`
into its existing signal channel. Each sender opens the event of the exact child
it owns, sets it, and keeps its existing `taskkill /T` request when the child has
not registered one.

## Work orders

- [x] [Task 01: Deliver cooperative shutdown to console-less children](task-01-cooperative-shutdown-event.md)

## Changed-path coverage

The work order owns the backend launcher, backend startup, and shared
`winproc` files above, the desktop `backend.rs` and `windows_shutdown.rs`
files, their regression tests, the extended
`docs/specs/desktop/requirements/desktop-tauri-app.md` acceptance criterion,
and the updated desktop system design.

## Verification

Permanent regression coverage lives beside the changed code:

- `apps/backend/internal/launcher/process_group_windows_test.go`:
  `TestWindowsManagedProcessGracefulStopReachesConsolelessChild` starts a
  console-less child running the real launcher signal handler and requires the
  graceful request to succeed and the child to exit cleanly.
- `apps/backend/internal/backendapp/startup_shutdown_windows_test.go`: the
  startup signal controller treats the request as its first signal and drops the
  event when released.
- `apps/desktop/src-tauri/src/backend.rs`:
  `terminate_child_stops_console_less_child_cooperatively` requires
  `terminate_child_with_timeout` to stop a console-less child cleanly.

## Risks

- A sender that created the event would make a later listener fail; senders only
  open an existing event.
- A repeated request must not become the second signal that forces exit; the
  waiter forwards at most one signal.
- The development `make` wrapper has no event and keeps the tree-wide request.
