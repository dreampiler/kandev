---
id: "01-classify-observed-provider-signatures"
title: "Classify observed provider signatures"
status: completed
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-PROVIDER-ERROR-SIGNATURES-001
  - REQ-AGENTS-PROVIDER-ACCOUNT-BLOCK-001
acceptance_criteria:
  - AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.7
  - AC-AGENTS-PROVIDER-ERROR-SIGNATURES-001.8
  - AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.7
system_design:
  - ../../specs/agents/system-design/provider-error-signatures.md
  - ../../specs/agents/system-design/provider-account-block-classification.md
---
# Task 01: Classify observed provider signatures

## Summary

Close three classification gaps observed across dynamic paths: the
account-block envelope that leads with a JSON `code` field, the AI-SDK
`"error_type":"provider_unavailable"` structured field, and the context-window
rejection.

## In scope

- `apps/backend/internal/agent/runtime/routingerr/provider_neutral_rules.go`:
  widen `provider.account_blocked.v1`; add `provider.structured_error_type.v1`
  and `provider.context_length_exceeded.v1`.
- `apps/backend/internal/agent/runtime/routingerr/rule_ids.go`: the two new rule
  IDs.
- `apps/backend/internal/agent/runtime/routingerr/account_block_test.go` and
  `failover_signatures_test.go`: the new cases.
- The provider error signature requirements and design, and the account-block
  requirements and design.

## Out of scope

- A new code or frontend enum value; the context-length signature keeps the
  `agent_runtime_error` code and changes no route policy.
- The `opencode_db_statement` and `free-models-per-day` signatures.

## Acceptance

- An account-block envelope with a leading `code` field classifies as
  `account_blocked` at high confidence; a plain `403` inside the envelope does
  not.
- An envelope carrying `"error_type":"provider_unavailable"` classifies as
  `provider_unavailable` at high confidence, and prose quoting the field does
  not.
- `maximum context length is <n> tokens` classifies at high confidence under
  `provider.context_length_exceeded.v1` while keeping the `agent_runtime_error`
  code, so routing is unchanged, and prose quoting it does not.
- The account-block circuit behavior is unchanged.

## Validation

```bash
cd apps/backend
go test ./internal/agent/runtime/routingerr/... -count=1
go test ./internal/agent/runtime/dynamic/... -run 'AccountBlock|FailureCause' -count=1
go test ./internal/orchestrator/... -run 'AccountBlock' -count=1
go build ./...
go vet ./...
```
