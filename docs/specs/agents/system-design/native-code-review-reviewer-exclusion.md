---
status: draft
system: agents
requirements:
  - REQ-AGENTS-NATIVE-CODE-REVIEW-002
---

# Native Code Review Reviewer Exclusion System Design

## Purpose and boundaries

This design keeps a workflow-step native code review independent of the agent
that wrote the reviewed change. It extends the existing native code-review
subsystem (`internal/review/`) and its `run_code_review` step-entry trigger; it
does not introduce a second review framework, a role handoff, or a new routing
engine. Candidate ordering, provider/model eligibility, and the review run
lifecycle remain owned by the existing review subsystem and the agent-profile
lookups it already uses.

## Candidate order and the dynamic router

The review subsystem has no dynamic routing tiers: its reviewer candidates are
the configured precedence chain (explicit step profile, enabled `code-review`
utility agent/profile, user default utility profile), and that is the "configured
candidate order" this change must preserve. Exclusion removes only exact origin
matches in place, so every remaining candidate keeps its position and
eligibility, and no dynamic-engine import or parallel routing path is added. The
task's own dynamic profile routing (the writer's launch) is untouched by this
change; the writer still runs under its configured candidate order.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-AGENTS-NATIVE-CODE-REVIEW-002` | [Components and responsibilities](#components-and-responsibilities) |

## Components and responsibilities

- **Workflow callback (`internal/orchestrator/workflow_callbacks.go`)** — when a
  step enters with `run_code_review`, reads the entering session's
  `AgentProfileID` and `ExecutionProfileID` and passes them on the run request as
  the origin identity, marking the origin required. A failed or absent read sends
  a nil origin with the requirement set, so resolution fails closed; step entry
  itself is never blocked.
- **Review runner (`internal/review/runner.go`)** — carries the origin identity
  on `RunRequest` and hands it to resolution. It creates no run row when
  resolution fails closed.
- **Review resolver (`internal/review/resolver.go`)** — walks the existing
  candidate order (explicit step profile, then the enabled `code-review` utility
  agent/profile, then the user default utility profile) and returns the first
  candidate that is not the origin execution.
- **Built-in YAML loader (`apps/backend/config/workflows/loader.go`)** — accepts
  `run_code_review` in the `on_enter` allow-list and validates its optional
  `agent_profile_id` config as a string.
- **Built-in template (`apps/backend/config/workflows/feature-dev.yml`)** — the
  single-model shape: the review step no longer clears the agent context on
  entry, the verification step starts a native review, and the prompts keep the
  writer as the owner of judging, fixing, and committing.

## Data and contracts

- **Origin identity:** the logical agent profile ID and the concrete execution
  profile ID of the entering session. It is intentionally not persisted on the
  run; it is an input to one resolution.
- **Candidate identity:** a resolved reviewer carries either a profile ID (which
  the resolver loads for its agent/model/mode) or an agent/model pair.
- **Comparison:** a candidate is excluded when its concrete profile ID equals the
  origin's logical or concrete profile ID, or when its effective agent and model
  equal the origin's. A permission mode is a posture, not a distinct model, so an
  empty mode on either side still matches; the contract is "do not silently
  review with the writer's own model", not "differ by mode".
- **No new persisted fields, tables, events, signatures, or hashes** are added.

## Control flow

1. The engine fires the `run_code_review` on-enter action on step entry.
2. The callback reads the entering session and builds the origin identity.
3. The runner asks the resolver to resolve a reviewer, passing the origin.
4. The resolver builds the exclusion set (profile IDs plus the origin's
   agent/model), then walks the candidate order, skipping matches in place.
5. The first remaining candidate runs the review; the run records that reviewer's
   agent and model as before.

## Failure and recovery

- **No distinct candidate:** resolution fails with `ErrAgentUnavailable`, which
  maps to the existing `review_agent_unavailable` run code and the Review
  surface's Settings hint. No run row or finding is created, and step entry is
  unaffected.
- **Origin session unreadable:** the callback still lets the task enter the step,
  but the run resolution fails with `ErrAgentUnavailable` (`review_agent_unavailable`)
  and creates no run row or findings. Reviewing without a provable distinct model
  would break the invariant the feature exists to protect, so this path fails
  closed instead of launching an unrestricted review.
- **Runtime failures** (diff unavailable, provider error, unparseable response)
  keep their existing handling; they are not changed by exclusion.

## Persistence

Exclusion changes no storage. Runs and findings are persisted exactly as before
under the native code-review contract.

## Security

The origin identity is task-scoped session metadata already used by the review
path. No credential, token, or new identifier is created or exposed, and the
change keeps the agent-profile isolation and worktree-isolation invariants.

## Observability

Review-run outcomes use the existing run row and logs. When resolution fails
because every candidate is the origin, the failure is recorded on the run (or
logged when no run row exists, matching the current resolve-before-create order)
with the `review_agent_unavailable` code. No new metric is added.

## Related decisions

- [Single-Session Model Switching](../../../decisions/2026-07-26-single-session-model-switching.md)
- [Native Code Review requirements](../requirements/native-code-review.md)
- [Profile-backed Utility Agents](utility-agent-profiles.md)
