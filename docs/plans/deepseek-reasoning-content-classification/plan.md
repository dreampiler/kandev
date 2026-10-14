---
created: 2026-10-15
status: implemented
requirements:
  - REQ-PLATFORM-PROVIDER-ERROR-RECOVERY-001
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
system_design:
  - ../../specs/platform/system-design/provider-error-recovery.md
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
legacy_specs: []
---

# Implementation Plan: DeepSeek reasoning_content Resume Corruption

## Overview

Repair [issue #4493](https://github.com/kdlbs/kandev/issues/4493). DeepSeek
thinking mode rejects a resumed turn with a 400 when the persisted assistant
history lost its `reasoning_content`. The catalogue has no rule for that
signature, so it falls through to `phase.poststart.unknown` and
`IsResumeCorrupted` reports false. Resuming replays the same history and fails
identically; only a fresh session recovers.

Add one exact catalogue rule beside the Anthropic thinking-blocks rule and
extend `IsResumeCorrupted` to the same signature. Existing resume-corruption
handling then applies: no automatic retry or fallback, and the recovery
message steers toward a fresh session.

## Scope

### In scope

- The `deepseek.reasoning_content.nonreplayable.v1` runtime-environment rule
  mapping to `resume_corrupted` with fresh-session remediation.
- `IsResumeCorrupted` recognition of the same single-line signature.
- Unit coverage for code, class, policy flags, and false-positive boundaries.

### Out of scope

- Automatic fallback to a later candidate after this failure. Resume corruption
  remains a veto for unclassified fallback.
- Repairing or reconstructing persisted `reasoning_content`.
- Changes to orchestration, recovery UI, or policy values.

## Work orders

- [x] [Task 01: Classify DeepSeek reasoning_content 400 as resume corruption](task-01-classify-reasoning-content-resume-corruption.md)

## Verification results

- `cd apps/backend && go test ./internal/agent/runtime/routingerr -run "ReasoningContent|ResumeCorrupted"` passed.
- `go vet ./internal/agent/runtime/routingerr/...` passed.
