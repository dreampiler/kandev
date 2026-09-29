---
id: "01-antigravity-usage-client"
title: "Add Antigravity (Gemini) usage client and register in usage_adapter"
status: in_progress
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-001
acceptance_criteria:
  - AC-COSTS-SUBSCRIPTION-USAGE-001.2
  - AC-COSTS-SUBSCRIPTION-USAGE-001.5
system_design:
  - ../../specs/costs/requirements/subscription-usage.md
---

# Task 01: Add Antigravity Usage Client

## Summary

Implement Antigravity usage provider that reads `agy -p "/usage" --output-format json`, parses the "Gemini Models" group buckets, and registers in `usage_adapter.go` via `ensureRegistered`.

## Acceptance

- `ensureRegistered` includes Antigravity provider alongside Claude and Codex
- Parser handles `command.data.groups[]` → "Gemini Models" → `buckets[]` with `window`, `remaining_fraction`, `reset_time`
- Utilization computed as `1 - remaining_fraction` (percentage 0-100)
- Disabled buckets (missing `reset_time`) are skipped
- Returns `ProviderUsage` with provider="antigravity", windows with `Label`, `UtilizationPct`, `ResetAt`
- No model calls or token consumption (verified: `num_turns: 0`, `usage.total_tokens: 0`)

## Verification

```bash
cd apps/backend && go test -tags fts5 ./internal/agent/usage ./internal/backendapp
```

Manual verification:
```powershell
agy -p "/usage" --output-format json
# Verify: status SUCCESS, num_turns 0, usage.total_tokens 0
# Verify: "Gemini Models" group with buckets for weekly/5h
```

## Files likely touched

- `apps/backend/internal/backendapp/usage_adapter.go` — `ensureRegistered` registration, Antigravity client implementation
- `apps/backend/internal/agent/usage/antigravity_usage.go` — new file for parser (or inline in usage_adapter)
- Unit tests for parsing logic

## Results

- Antigravity provider registered in `usage_adapter.go` `ensureRegistered`
- Parser extracts "Gemini Models" group buckets, computes utilization = 1 - remaining_fraction
- Live measurement: 7-day window 100%, reset 2026-10-02T18:47:39Z (5h bucket disabled)
- Tests pass, commit hooks (Go lint) pass
- Fork commit: `31304b2a2` (branch `kd/antigravity-usage`)
- PR: https://github.com/dreampiler/kandev/pull/23 (draft)