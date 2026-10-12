---
id: "02-writer-owned-review-template"
title: "Writer-owned review in the built-in feature-dev template"
status: building
wave: 1
depends_on: ["01-loader-allowlist"]
plan: "plan.md"
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-002
acceptance_criteria:
  - AC-AGENTS-NATIVE-CODE-REVIEW-002.1
system_design:
  - ../../specs/agents/system-design/native-code-review-reviewer-exclusion.md
---

# Task 02: Writer-owned review in the built-in `feature-dev` template

## Summary

Make the shipped template the single-model shape: one session/profile across
steps, a native review on verification, and prompts where the writer judges,
fixes, and commits.

## In scope

- Remove `reset_agent_context` from the Review step's `on_enter`.
- Add `run_code_review` to the QA step's `on_enter`.
- Rewrite the Review prompt so the writer reads native findings and owns every
  fix and commit; note the same ownership in the QA prompt.
- Add the optional design-advice clause to the Spec step.

## Out of scope

- Live project workflow settings or stored prompts; those are applied later by
  the AIWS prompt source owner.
- Explicit per-step `agent_profile_id` or session-policy fields; the defaults
  (`reuse`/`park`) already provide one continuous session.

## Acceptance

- The Review step no longer clears context on entry.
- The QA step starts a native review on entry.
- The Review/QA prompts place judging, fixing, and committing with the writer.

## Verification

```bash
cd apps/backend && go test -trimpath ./config/workflows/...
```

## Files likely touched

- `apps/backend/config/workflows/feature-dev.yml`
- `apps/backend/config/workflows/loader_review_test.go` (guard assertion)

## Dependencies

- Task 01 (the template action must load).

## Risks

- Product-visible behavior change for the built-in template; disclosed in the PR.

## Results

`feature-dev.yml` Review/QA/Spec steps updated and the template still loads.
