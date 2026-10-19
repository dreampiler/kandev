---
created: 2026-10-20
status: done
requirements:
  - REQ-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-006
system_design:
  - ../../specs/agents/system-design/session-startup-failure-explanations.md
legacy_specs: []
---

# Implementation Plan: Start model transport failure

## Overview

A startup `SetModel` can run after the agent stream is already gone, for
example when the agent is torn down between `session/new` and the model step.
The strict model branches wrapped that failure as
`model_selection_failed`/`application_failed`, so the session error card named
the requested model as the cause of a transport or teardown race.

## Scope

In scope: `apps/backend/internal/agent/runtime/lifecycle/start_model.go` and its
permanent test.

Excluded: auto-fallback and provider-default continuations, method-not-found
handling, permission-mode application, and any change to how the generic
bootstrap cause is presented.

## Technical approach

Reuse the existing `isTransportDeadErr` and `isAgentStreamNotConnectedErr`
helpers that `session/load` already uses. The three strict SetModel branches
(requested model, unique variation, advertised fallback) return such an error
unchanged, as they already did for cancellation and deadlines.

## Work orders

- [x] [Task 01: Pass startup model transport failures through](task-01-start-model-transport-failure.md)

## Changed-path coverage

The work order owns `start_model.go`, its permanent test, the startup transport
amendment in the session recovery failures requirement, and the matching note in
the startup failure explanations design.
