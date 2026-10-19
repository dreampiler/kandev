---
id: "01-start-model-transport-failure"
title: "Pass startup model transport failures through"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-006
acceptance_criteria:
  - AC-AGENTS-AGENT-RESUME-RUNTIME-RECOVERY-006.35
system_design:
  - ../../specs/agents/system-design/session-startup-failure-explanations.md
---

# Task 01: Pass startup model transport failures through

## Summary

Return a startup `SetModel` failure caused by a disconnected agent stream or a
dead ACP transport unchanged instead of wrapping it as a model application
failure.

## In scope

- `isStartModelTransportError` in `start_model.go`, combining the existing
  transport-dead and stream-not-connected helpers.
- The strict failure branches of `applyStartModelPolicy`,
  `applyUniqueAdvertisedVariation`, and `applyAdvertisedFallback`.

## Out of scope

Auto-fallback, method-not-found, permission-mode application, and the
presentation of the generic bootstrap cause.

## Verification

- `go test ./internal/agent/runtime/lifecycle/ -run 'TestStartModelTransportFailureIsNotReportedAsModelApplicationFailure|TestStartModelProviderRejectionStaysModelApplicationFailure'`
- The existing start model tests in `start_model*_test.go`.
