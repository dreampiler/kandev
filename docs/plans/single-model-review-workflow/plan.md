---
created: 2026-10-12
status: building
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-002
system_design:
  - ../../specs/agents/system-design/native-code-review-reviewer-exclusion.md
legacy_specs: []
---

# Implementation Plan: Single-model review workflow

## Overview

Deliver the owner-approved single-model workflow in the fork: one main model owns
a task end to end, a **different** model supplies native code review, and the
configured dynamic candidate order is preserved. Three scoped changes ship
together: the built-in YAML loader accepts `run_code_review` as a step action; the
built-in `feature-dev` template keeps one session/profile, attaches a native
review to the verification step, and its prompts make the writer judge, fix, and
commit findings; and reviewer selection excludes only the origin execution, failing
closed with `review_agent_unavailable` when no distinct candidate remains.

## Scope

### In scope

- `run_code_review` in the built-in template `on_enter` allow-list, with string
  validation for its optional `agent_profile_id`.
- Writer-owned review prompts and single-session settings in `feature-dev.yml`.
- Origin-execution exclusion in native review resolution, preserving candidate
  order and failing closed when none is distinct.
- Narrow source-adjacent unit tests and one isolated runtime proof.

### Out of scope

- Deployment/install and all-project workflow settings/prompt application
  (Operations and the AIWS prompt source owner).
- Automatic delivery of findings to the origin session.
- Office participant/quorum review, a competing reviewer framework, or new
  signatures, hashes, ledgers, or audit records.

## Technical approach

- **Loader:** add `OnEnterRunCodeReview` to `validOnEnter` and mirror the
  `set_session_mode` string guard for the optional `agent_profile_id` config.
- **Template:** remove `reset_agent_context` from the Review step, add
  `run_code_review` to the QA step's `on_enter`, and rewrite the Review/QA/Spec
  prompts so the writer reads findings and owns every decision and commit.
- **Exclusion:** extend `review.RunRequest` with the origin identity; the
  workflow callback reads it from the entering session; the resolver walks the
  existing explicit → utility → default candidate order and removes only matches,
  failing closed with `ErrAgentUnavailable` when nothing distinct remains.

## Tests

- Loader: `run_code_review` round-trips; non-string `agent_profile_id` is
  rejected; the shipped template keeps the intended shape.
- Resolver: explicit/utility/default origin matches fall through; all-origin
  fails closed; same agent+model is excluded even without a profile-ID match.
- Runner: origin is forwarded; only-origin fails before creating a run; a
  distinct reviewer is recorded on the run.
- Callback: the entering session's identity is forwarded; an unreadable session
  does not block step entry.

## Verification results

Narrow package tests, `go vet`, and changed-file `gofmt`; the one isolated
`run_code_review` runtime proof is recorded in the final work order/PR body.

## Risks

- The resolver's candidate order is the fixed precedence chain, not the dynamic
  routing tiers; the exclusion preserves that order either way and always fails
  closed.
- Changing the built-in template is a product-visible behavior change; it matches
  the approved single-session decision and is called out in the PR body.

## Work orders

- [ ] [Task 01: Loader allow-list](task-01-loader-allowlist.md)
- [ ] [Task 02: Writer-owned review template](task-02-writer-owned-review-template.md)
- [ ] [Task 03: Reviewer origin exclusion](task-03-reviewer-origin-exclusion.md)
- [ ] [Task 04: Isolated verification](task-04-isolated-verification.md)
