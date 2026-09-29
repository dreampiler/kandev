---
id: "01-antigravity-usage-client"
title: "Add Antigravity (Gemini) usage client and register in usage_adapter"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-001
acceptance_criteria:
  - AC-COSTS-SUBSCRIPTION-USAGE-001.9
  - AC-COSTS-SUBSCRIPTION-USAGE-001.10
system_design:
  - ../../specs/costs/system-design/antigravity-usage.md
---

# Task 01: Add Antigravity Usage Client

## Summary

Implement Antigravity usage provider that reads `agy -p "/usage" --output-format json`, parses the "Gemini Models" group buckets, and registers in `usage_adapter.go` via `ensureRegistered`.

## Acceptance

- `ensureRegistered` includes Antigravity provider alongside Claude and Codex
- Parser handles `command.data.groups[]` → "Gemini Models" → `buckets[]` with `window`, `remaining_fraction`, `reset_time`
- Utilization computed as `1 - remaining_fraction` (percentage 0-100)
- Disabled buckets (missing `reset_time`) are skipped
- Returns `ProviderUsage` with provider="google", windows with `Label`, `UtilizationPct`, `ResetAt`
- No model calls or token consumption (verified: `num_turns: 0`, `usage.total_tokens: 0`)

## Verification

```bash
cd apps/backend && go test ./internal/agent/usage -count=1
cd apps/backend && go test ./internal/agent/agents -run '^TestAntigravityACP_' -count=1
cd apps/backend && go test ./internal/backendapp -run '^TestTeamClaudeStatusURL$' -count=1
```

Manual verification:
```powershell
agy -p "/usage" --output-format json
# Verify: status SUCCESS, num_turns 0, usage.total_tokens 0
# Verify: "Gemini Models" group with buckets for weekly/5h
```

## Files likely touched

- `apps/backend/internal/backendapp/usage_adapter.go` — `ensureRegistered` registration
- `apps/backend/internal/agent/usage/client_antigravity.go` — CLI client and parser
- `apps/backend/internal/agent/agents/antigravity_acp.go` — subscription billing classification
- `apps/backend/internal/agent/agents/antigravity_acp_test.go` — updated billing expectation

## Results

- Antigravity provider registered in `usage_adapter.go` `ensureRegistered`
- Parser extracts "Gemini Models" group buckets, computes utilization = 1 - remaining_fraction
- Live measurement of `agy -p "/usage" --output-format json`: `status` `SUCCESS`,
  `num_turns` 0, `usage.total_tokens` 0. The Gemini Models weekly bucket reported
  `remaining_fraction` 0 (100% used) with reset time `2026-10-02T18:47:39Z`; the
  disabled 5-hour bucket (no `reset_time`) was omitted, and the separate
  "Claude and GPT models" group was not attributed to Gemini.
- A disposable in-package test parsed the captured CLI output into provider `google`
  with a single `7-day` window at 100%; the file was removed after the run and is
  not committed.
- Tests pass, commit hooks (Go lint) pass
- Fork commit: `31304b2a2` (branch `kd/antigravity-usage`)
- PR: https://github.com/dreampiler/kandev/pull/23
