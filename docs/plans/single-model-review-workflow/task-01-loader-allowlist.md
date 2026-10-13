---
id: "01-loader-allowlist"
title: "run_code_review built-in loader allow-list"
status: building
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-002
acceptance_criteria:
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.5
system_design:
  - ../../specs/agents/system-design/native-code-review-reviewer-exclusion.md
---

# Task 01: `run_code_review` built-in loader allow-list

## Summary

Let a shipped workflow template declare the native code review as a step action.

## In scope

- Add `run_code_review` to the built-in YAML `on_enter` allow-list.
- Reject a present-but-non-string `agent_profile_id` config.

## Out of scope

- The engine/step-entry/DB path, which already supports the action.
- Template prompt changes (task 02).

## Acceptance

- A built-in template with `run_code_review` loads, with or without
  `agent_profile_id`.
- A non-string `agent_profile_id` is rejected at load.

## Verification

```bash
cd apps/backend && go test -trimpath ./config/workflows/...
```

## Files likely touched

- `apps/backend/config/workflows/loader.go`
- `apps/backend/config/workflows/loader_review_test.go` (new)

## Dependencies

None.

## Risks

- None beyond keeping the strict `KnownFields` YAML decode intact.

## Results

`validOnEnter` accepts `run_code_review`; `convertEvents` rejects a non-string
`agent_profile_id`. `go test ./config/workflows/...` passed.
