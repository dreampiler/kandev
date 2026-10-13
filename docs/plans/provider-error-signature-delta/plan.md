---
created: 2026-10-13
status: completed
requirements:
  - REQ-AGENTS-PROVIDER-ERROR-SIGNATURES-001
  - REQ-AGENTS-PROVIDER-ACCOUNT-BLOCK-001
system_design:
  - ../../specs/agents/system-design/provider-error-signatures.md
  - ../../specs/agents/system-design/provider-account-block-classification.md
legacy_specs: []
---
# Implementation Plan: Provider Error Signature Delta

## Overview

A day's monitoring roll-up showed the same provider diagnostic classified
differently by path, or falling through to `agent_runtime_error` with rule
`phase.poststart.unknown`. Three captured signatures are covered here:

1. An account-restriction envelope whose JSON object leads with a scalar `code`
   field before its `message` field, so `provider.account_blocked.v1` never
   matched it and the failure became an unclassified runtime error.
2. An AI-SDK envelope whose structured `"error_type":"provider_unavailable"`
   field has an underscore, so the space-separated prose in
   `provider.unavailable.v1` never matched it.
3. A context-window rejection (`maximum context length is <n> tokens`) with no
   rule at all, so it stayed anonymous.

The [provider error signature requirements](../../specs/agents/requirements/provider-error-signatures.md),
[provider error signature design](../../specs/agents/system-design/provider-error-signatures.md),
and [account-block requirements](../../specs/agents/requirements/provider-account-block-classification.md)
own the contract.

## Evidence and cause

- `provider.account_blocked.v1` (`apps/backend/internal/agent/runtime/routingerr/provider_neutral_rules.go`) allowed only an opening `{"message":"` before the `Error from provider <name>: 403 Forbidden` envelope, so a `{"code":<n>,"message":"...` envelope fell through to `phase.poststart.unknown` (`routingerr.go`).
- `provider.unavailable.v1` matches `provider unavailable` with a space boundary; the structured value is `provider_unavailable` with an underscore, so it did not match.
- No rule in `routingerr` recognized `maximum context length is <n> tokens`.
- The dynamic route's pre-result provider-diagnostic path in `internal/orchestrator/dynamic_evidence.go` accepts only high-confidence, fallback-allowed classifications, so a medium-confidence match would be treated as ordinary output.

## Scope

### In scope

- Two provider-neutral signatures in `provider_neutral_rules.go` and the
  account-block envelope widening, plus rule IDs.
- The context-length rule, which names the failure as `task_error` without
  adding a code, a frontend enum value, or a route-policy change.
- Source-adjacent classifier tests.

### Out of scope

- The `opencode_db_statement` failure: no evidence yet of whether it is a local
  agent-side fault or a provider fault, so its classification is unchanged.
- `free-models-per-day`: it already classifies as `rate_limited`, and the
  account-daily scope and reset are owned by `dynamic.failureCauseRules`, not the
  code.
- Adding a `context_length_exceeded` code and redirecting the failure to a
  larger-context candidate.

## Behavior note

The context-length rule names the failure `task_error`. The orchestrator's
unclassified-failure route accepts only `agent_runtime_error` at its origin gate
(`internal/orchestrator/dynamic_launch.go`, `internal/agent/runtime/dynamic/types.go`),
so a `task_error` failure is not routed there. The shipped default document
disables the per-candidate unclassified policy, so under the default the routing
is identical (both reach manual recovery); the difference appears only when an
operator enables that policy. Preserving the old routing exactly would require
extending the orchestrator gate, which is out of scope for a classifier change.

## Tests

| AC suffix | Test file and test |
| --- | --- |
| SIGNATURES-001.7 | `routingerr/failover_signatures_test.go`: `TestFailoverCatalogueSignatures`, `TestStructuredSignatureRulesIgnoreQuotedProse` |
| SIGNATURES-001.8 | `routingerr/failover_signatures_test.go`: `TestFailoverCatalogueSignatures`, `TestStructuredSignatureRulesIgnoreQuotedProse` |
| ACCOUNT-BLOCK-001.7 | `routingerr/account_block_test.go`: `TestAccountBlockedClassification`, `TestAccountBlockedRejectsProseAndPlain403` |

## Work orders

- [x] [Task 01: Classify observed provider signatures](task-01-classify-observed-provider-signatures.md)

## Verification results

- `go test ./internal/agent/runtime/routingerr/...` passes except the existing
  Windows-only `TestRemediateNpxCache_RemovesHashDirAndPreservesParent`, which
  fails on the base commit too.
- `go test ./internal/agent/runtime/dynamic/... -run 'AccountBlock|FailureCause'`
  and `go test ./internal/orchestrator/... -run 'AccountBlock'` pass, so the
  account-block circuit behavior is unchanged.
- `go build ./...` and `go vet ./...` pass.
- Not verified here: the live provider diagnostics themselves and the next
  monitoring roll-up.
