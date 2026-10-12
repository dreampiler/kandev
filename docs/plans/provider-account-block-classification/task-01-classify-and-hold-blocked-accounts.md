---
id: "01-classify-and-hold-blocked-accounts"
title: "Classify and hold blocked provider accounts"
status: in_progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-PROVIDER-ACCOUNT-BLOCK-001
acceptance_criteria:
  - AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.1
  - AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.2
  - AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.3
  - AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.4
  - AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.5
  - AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.6
system_design:
  - ../../specs/agents/system-design/provider-account-block-classification.md
---
# Task 01: Classify and hold blocked provider accounts

## Summary

Recognize the `403 Forbidden` account-restriction diagnostic, stop retrying it, and hold only the failing execution profile until its owner releases it through the settings API.

## In scope

- Write `TestAccountBlockedClassification` first and confirm that it fails on the base commit.
- `apps/backend/internal/agent/runtime/routingerr/routingerr.go`: `CodeAccountBlocked`, hard class, invariants, hold helper.
- `apps/backend/internal/agent/runtime/routingerr/provider_neutral_rules.go`: `provider.account_blocked.v1`.
- `apps/backend/internal/agent/runtime/routingpolicy/policy.go`: no retry or reset-wait for `account_blocked`.
- `apps/backend/internal/agent/runtime/dynamic/circuit.go`: held circuit, `Hold`, and `ReleaseHold`.
- `apps/backend/internal/agent/runtime/dynamic/engine.go`, `suspension.go`, `suspension_policy.go`, `types.go`: hold key, hold opening, selection, legacy retry, preview scope, and release.
- `apps/backend/internal/agent/runtime/dynamic_resource_limits.go`: resolver release.
- `apps/backend/internal/orchestrator/dynamic_launch.go`: record the hold for a dynamic failure that is not routed.
- `apps/backend/internal/agent/settings/controller` and `handlers`: release seam and endpoint; `apps/backend/internal/backendapp/main.go`: wiring.
- `apps/web/lib/api/domains/dynamic-preview-api.ts`: `profile` suspension scope in the response type.
- Public note in `docs/public/agents-and-profiles.md` and the provider error signature contract.

## Out of scope

- Other `403` causes, other providers, Office routing, and new settings or UI controls.

## Acceptance

- The reported diagnostic and the account-restriction message classify as `account_blocked`, and a plain `403` and prose do not.
- A hard policy with retry enabled still skips or stops.
- Only the failing profile is held; a credential-sharing profile and other candidates stay selectable in order.
- The hold has no probe or expiry, survives restore, and closes only through the release endpoint.

## Validation

```bash
cd apps/backend
go test ./internal/agent/runtime/routingerr -run 'AccountBlocked|Gate|Catalogue' -count=1
go test ./internal/agent/runtime/routingpolicy -run 'AccountBlocked' -count=1
go test ./internal/agent/runtime/dynamic -run 'AccountBlock' -count=1
go test ./internal/agent/settings/handlers -run 'ReleaseAccountBlock' -count=1
go test ./internal/orchestrator -run 'AccountBlocked' -count=1
go build ./... && go vet ./internal/agent/runtime/... ./internal/agent/settings/... ./internal/orchestrator/...
```
