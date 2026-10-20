---
created: 2026-10-20
status: done
requirements:
  - REQ-TASKS-PRIMARY-SESSION-OWNERSHIP-001
system_design:
  - ../../specs/tasks/system-design/primary-session-ownership.md
legacy_specs: []
---

# Implementation Plan: Replaced session reclaim

## Overview

When a session loses the primary role or hands its queued messages to a
replacement, its agent process keeps running until workspace idle suspension
or a manual stop. Each replaced session therefore holds a full agent process
tree for the idle window. Behind `features.replacedSessionReclaim`, off in every
shipped profile, Kandev now stops that obsolete execution once it settles.

## Scope

In scope: `apps/backend/internal/orchestrator` (`replaced_session_reclaim.go`,
`SetPrimarySession`, `transferQueuedSessionState`, the idle-reaper tick, and
`ServiceConfig`), the runtime flag (`FeaturesConfig`, `runtimeflags/registry.go`,
`profiles.yaml`, `backendapp/orchestrator.go`, frontend `defaultFeatureFlags`),
and `docs/public/configuration.md` and `agent-communication.md`.

Excluded: automatic promotion, idle suspension policy, Office and coordinator
lifecycles, and queue routing.

## Technical approach

Register the exact execution the replaced session held, then stop it through
the existing session stop once the session has no known idle work, reusing the
idle reaper's eligibility checks and its tick for retries.

## Work orders

- [x] [Task 01: Stop settled replaced session runtimes](task-01-replaced-session-reclaim.md)

## Changed-path coverage

The work order owns the orchestrator files above, their permanent tests, the
flag plumbing, `AC-TASKS-PRIMARY-SESSION-OWNERSHIP-001.6`, the replaced session
reclaim section of the primary session ownership design, and the public docs.
