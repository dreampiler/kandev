---
created: 2026-09-29
status: in_progress
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-001
system_design:
  - ../../specs/costs/requirements/subscription-usage.md
legacy_specs: []
---

# Implementation Plan: Gemini (Antigravity) Usage Provider

## Overview

Add Antigravity (Gemini CLI) as a subscription usage provider in the fork's `agent/usage` package. The implementation parses `agy -p "/usage" --output-format json` output, extracting the "Gemini Models" group buckets with `window`, `remaining_fraction`, and `reset_time`, converting to the standard `ProviderUsage` format with utilization = 1 - remaining_fraction.

Reference: ai-chatroom `lib/usage.mjs` (MIT).

## Scope

### In scope

- Antigravity usage client in `apps/backend/internal/backendapp/usage_adapter.go` registered via `ensureRegistered`
- Parse `command.data.groups[]` → find "Gemini Models" group → extract `buckets[]` with `window`, `remaining_fraction`, `reset_time`
- Convert to `ProviderUsage` with utilization = 1 - remaining_fraction (percentage)
- Skip disabled buckets (e.g., 5h bucket with no reset_time)
- Live measurement verified: SUCCESS, 0 turns, 0 tokens, 7-day 100%, reset 2026-10-02T18:47:39Z
- Unit tests for parsing logic

### Out of scope

- Billing integration or charge reconciliation
- Real-time polling (provider API not real-time)
- Other Gemini interfaces (only Antigravity CLI via `agy`)

## Work orders

- [ ] [Task 01: Add Antigravity usage client and register in usage_adapter](task-01-antigravity-usage-client.md)

## Verification results

- `cd apps/backend && go test -tags fts5 ./internal/agent/usage ./internal/backendapp` passed.
- Manual `agy -p "/usage" --output-format json` verified: SUCCESS, 0 turns, 0 tokens.

## Bundled delivery

This work order is part of the combined replacement cycle with:
- #19 (ceiling stop cause fix)
- Idle session agent process reclamation (fork #12 fix)
- Backend stop cause analysis (4e224cbf)