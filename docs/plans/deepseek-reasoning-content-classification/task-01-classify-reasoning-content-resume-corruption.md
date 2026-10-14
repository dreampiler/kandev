---
id: "01-classify-reasoning-content-resume-corruption"
title: "Classify DeepSeek reasoning_content 400 as resume corruption"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLATFORM-PROVIDER-ERROR-RECOVERY-001
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
acceptance_criteria:
  - AC-PLATFORM-PROVIDER-ERROR-RECOVERY-001.3
  - AC-PLATFORM-PROVIDER-ERROR-RECOVERY-001.5
  - AC-PLATFORM-PROVIDER-ERROR-RECOVERY-001.6
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-002.6
system_design:
  - ../../specs/platform/system-design/provider-error-recovery.md
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
---

# Task 01: Classify DeepSeek reasoning_content 400 as Resume Corruption

## Summary

Map the DeepSeek thinking-mode `reasoning_content ... must be passed back`
400 to the existing `resume_corrupted` code with fresh-session remediation,
and make `IsResumeCorrupted` recognize the same signature.

## Acceptance

- The exact single-line signature classifies as high-confidence
  `resume_corrupted` under rule `deepseek.reasoning_content.nonreplayable.v1`,
  with no automatic retry or fallback.
- `IsResumeCorrupted` returns true for the signature and false for empty text,
  unrelated `reasoning_content` prose, and a gap beyond the line bound.
- The failure is corrupt resume state, so it never qualifies for unclassified
  fallback.

## Verification

```bash
cd apps/backend && go test ./internal/agent/runtime/routingerr -run "ReasoningContent|ResumeCorrupted"
```

## Files likely touched

- `apps/backend/internal/agent/runtime/routingerr/runtime_rules.go`
- `apps/backend/internal/agent/runtime/routingerr/reasoning_content_nonreplayable_test.go`

## Risks

A broad `reasoning_content` match could turn agent prose into a veto. The rule
requires `must be passed back` within 80 characters on the same line.

## Results

- `TestClassify_ReasoningContentNonReplayable` and
  `TestIsResumeCorrupted_ReasoningContent` pass with the existing
  resume-corruption tests.
