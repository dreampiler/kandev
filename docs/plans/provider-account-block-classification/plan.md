---
created: 2026-10-12
status: completed
requirements:
  - REQ-AGENTS-PROVIDER-ACCOUNT-BLOCK-001
system_design:
  - ../../specs/agents/system-design/provider-account-block-classification.md
legacy_specs: []
---
# Implementation Plan: Provider Account-Block Classification

## Overview

A gateway reported `403 Forbidden` with structured code `user_blocked` for one execution profile.
The diagnostic falls through to `agent_runtime_error` with rule `phase.poststart.unknown`, so routing cannot tell that the account is restricted and keeps the profile selectable.
The [requirements](../../specs/agents/requirements/provider-account-block-classification.md) and [design](../../specs/agents/system-design/provider-account-block-classification.md) define the contract.

## Evidence and cause

- `classifyKanbanFailure` (`apps/backend/internal/orchestrator/event_handlers_transient_messages.go`) classifies the text without an HTTP status, so the structured `403` mapping never applies.
- No rule in `routingerr/rules.go`, `provider_neutral_rules.go`, `runtime_rules.go`, or `auth_diagnostic.go` recognizes `user_blocked` or `403 Forbidden`.
- `openCircuitForFailure` (`agent/runtime/dynamic/engine.go`) opens circuits only for listed codes, on the binding key, for one minute or until a reset hint. An expired circuit admits a probe.
- `CircuitRegistry` has no release operation, and no API releases a dynamic circuit.

## Scope

### In scope

- Code, rule, policy decision, held circuit, hold recording, release operation, and the release endpoint.
- Source-adjacent unit tests and public documentation.

### Out of scope

- Other `403` causes, other providers' rules, Office routing, and UI controls.

## Tests

| AC suffix | Test file and test |
| --- | --- |
| .1-.2 | `routingerr/account_block_test.go`: `TestAccountBlockedClassification`, `TestAccountBlockedRejectsProseAndPlain403` |
| .3 | `routingpolicy/policy_test.go`: `TestAccountBlockedSkipsRetryAndResetWait`; `dynamic/account_block_test.go`: `TestAccountBlockedLegacyRetryTriesNext` |
| .4-.5 | `dynamic/account_block_test.go`: `TestAccountBlockedHoldsOnlyFailingProfile`, `TestAccountBlockedHoldHasNoExpiryOrProbe`, `TestAccountBlockedHoldSurvivesRestore`, `TestAccountBlockedHoldKeepsRouteManual`; `orchestrator/dynamic_account_block_test.go`: `TestAccountBlockedAfterOutputHoldsProfile` |
| .6 | `dynamic/account_block_test.go`: `TestReleaseAccountBlockClosesOnlyHeldProfile`; `settings/handlers/account_block_release_test.go`: `TestReleaseAccountBlockEndpoint` |

## Work orders

- [x] [Task 01: Classify and hold blocked provider accounts](task-01-classify-and-hold-blocked-accounts.md)

## Verification results

- RED on the base commit: the observed diagnostic classified as `agent_runtime_error` with rule `phase.poststart.unknown`; a hard policy with reset-wait enabled returned `waiting_for_reset`; a post-output `account_blocked` failure left the failing profile selectable.
- GREEN: `routingerr` (excluding the existing Windows-only `TestRemediateNpxCache_RemovesHashDirAndPreservesParent`), `routingpolicy`, and `dynamic` packages; `runtime`, `orchestrator`, `settings/handlers`, `settings/controller`, and `backendapp` with dynamic, preview, release, and permission filters.
- Disabling the held-circuit predicate fails six hold and release tests.
- `go build ./...` and `go vet` on the changed packages pass.
- Not verified here: a live provider restriction on an installed runtime, and the preview UI text for a hold without an end time.
