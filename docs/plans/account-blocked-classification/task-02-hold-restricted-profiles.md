---
id: "02-hold-restricted-profiles"
title: "Hold restricted profiles"
status: done
wave: 2
depends_on:
  - "01-classify-restricted-accounts"
plan: "plan.md"
requirements:
  - REQ-AGENTS-ACCOUNT-BLOCK-001
acceptance_criteria:
  - AC-AGENTS-ACCOUNT-BLOCK-001.4
  - AC-AGENTS-ACCOUNT-BLOCK-001.5
  - AC-AGENTS-ACCOUNT-BLOCK-001.6
system_design:
  - ../../specs/agents/system-design/account-blocked-classification.md
---

# Task 02: Hold restricted profiles

## Summary

Hold the failed execution profile in the circuit registry with no expiry and no
probe, skip it during selection, and report the failed profile from the
orchestrator.

## In scope

- `Hold`, `ReleaseHold`, and `IsHeld` on the circuit registry.
- Engine selection, failure handling, and resolver methods.
- Orchestrator hold for every dynamic-route failure.

## Out of scope

The settings release route and any change to candidate order.

## Acceptance

`AC-AGENTS-ACCOUNT-BLOCK-001.4` through `001.6`.

## Verification

```bash
cd apps/backend && go test ./internal/agent/runtime/dynamic -count=1
cd apps/backend && go test ./internal/orchestrator -run AccountBlocked -count=1
```
