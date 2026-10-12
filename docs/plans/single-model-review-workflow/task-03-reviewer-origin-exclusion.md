---
id: "03-reviewer-origin-exclusion"
title: "Reviewer origin-execution exclusion"
status: building
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-002
acceptance_criteria:
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.1
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.2
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.3
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.4
system_design:
  - ../../specs/agents/system-design/native-code-review-reviewer-exclusion.md
---

# Task 03: Reviewer origin-execution exclusion

## Summary

Never select the writer's own execution as the reviewer, and fail closed when no
distinct candidate exists.

## In scope

- Carry the origin identity on `review.RunRequest`.
- Exclude candidate profiles matching the origin's concrete/logical profile ID or
  its effective agent and model, preserving candidate order.
- Fail with `ErrAgentUnavailable` (`review_agent_unavailable`) when no distinct
  candidate remains and create no run row.
- Read the origin identity from the entering session in the workflow callback;
  an unreadable or unidentifiable session marks the origin required with a nil
  origin, so resolution fails closed without blocking step entry.

## Out of scope

- Reordering or ranking candidates beyond removing origin matches.
- Office participant/quorum review.

## Acceptance

- A distinct explicit/utility/default candidate is selected in order.
- All-origin selection fails closed with `review_agent_unavailable`.
- An unreadable origin session fails the review with `review_agent_unavailable`
  and still lets the task enter the step.

## Verification

```bash
cd apps/backend && go test -trimpath ./internal/review/... ./internal/orchestrator/...
```

## Files likely touched

- `apps/backend/internal/review/{resolver.go,runner.go}` + new tests
- `apps/backend/internal/orchestrator/workflow_callbacks.go` + new test

## Dependencies

None.

## Risks

- The candidate order is the resolver's precedence chain, not the dynamic tiers;
  documented in the system design.

## Results

Origin exclusion implemented and unit-tested; `go test` for both packages passed.

### Review execution repair (2026-10-13)

Production runs reached reviewer execution but failed authentication, OpenCode ACP initialization,
or response parsing. The repair preserves the reviewed writer exclusion and dynamic routing design,
and restores the profile execution contract in
[Utility agent profiles](../../specs/agents/system-design/utility-agent-profiles.md).

- Reuse session-launch environment definitions, secret resolution, required credentials, and
  managed/native command selection for session-bound and host utility profile execution.
- Preserve the session workspace, profile flags/prefix, mode, strip-env and permission policy.
- Retry only structural launch failures on the same dynamic route, excluding failed profiles and
  the writer. Record the reviewer that completed the pass. Prompt/parsing errors do not retry.
- Accept an explicit empty review and independently decode JSON objects surrounded by prose.
  Keep finding validation unchanged and reject null/malformed findings envelopes.

Failing-before evidence at `0d12c83d82`: `TestInferenceReviewLaunchResolvesProfileSecret` omitted
`CLAUDE_CODE_OAUTH_TOKEN`; `TestInferenceReviewLaunchUsesSessionCommandAndRuntimeEnv` sent
`opencode-ai@1.18.32` instead of selected `@opencode/cli@2.0.3`; `TestParseReviewNormalOutput`
rejected explicit empty responses and prose containing other braces;
`TestRunnerDynamicRouteContinuesAfterStructuralLaunchFailure` ended the pass at its first failure.
Fenced JSON and ordinary prose around a single JSON envelope already passed on the baseline.

Targeted verification covers `internal/review`, `internal/agent/hostutility`,
`internal/agentctl/server/utility`, lifecycle inference/profile environment tests, backendapp review
wiring, and task review service tests. No production smoke, process control, or API mutation is used.

Verification commands:

```bash
cd apps/backend
go test -trimpath ./internal/review ./internal/agentctl/server/utility
go test -trimpath ./internal/agent/hostutility -run 'TestHostProfilePrompt|TestExecuteProfilePrompt|TestHostUtilityResolvesOpenCodeOnlyAfterOperationAdmission|TestOpenCodeRuntimeCommandAdmissionOrdersUtilityAndMigration' -count=1 -timeout=30s
go test -trimpath ./internal/agent/runtime/lifecycle -run 'Test.*Inference|Test.*Profile.*Env|Test.*Utility|TestResolveAgentProfileEnvVars' -count=1
go test -trimpath ./internal/backendapp ./internal/task/service -run Review -count=1
```

The full hostutility suite stops at `TestManagedRuntimeProbeRecoveryPreservesSharedTree` in
`managedruntime/cache_windows.go:151`. A Go overlay restoring all modified Go sources/tests to
`0d12c83d82` reproduced the same timeout at that Windows directory-read site with a 30-second
budget. The repair does not modify that cache deletion path. Related host launch/admission tests
pass independently; the baseline failure is retained as a validation limitation.

After integrating fork main, the existing inference harness's installed-agent list assertion varied
between two host CLI discovery calls (7 versus 6 agents). The harness now registers only its
explicit fake agents, and the OpenCode command-parity case explicitly registers OpenCode. This
keeps the reviewer unit tests independent of real provider installations without changing runtime
selection or installation behavior.
