---
status: done
created: 2026-10-23
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-002
system_design:
  - ../../specs/agents/system-design/dynamic-unclassified-fallback.md
---

# feat(agents): edit unclassified failure policies per candidate

Edit existing unclassified-failure policy per dynamic candidate with integer thresholds 2 through 10 and a confirmed bulk draft action.

## Delivery order and dependencies

The implementation is one existing PR slice. Preserve its native dependencies and existing feature defaults, then verify the narrow affected paths. No dependent PR is imported into this package.

- [Implementation and verification](task-01-implementation.md)

## Scope and risks

Backend routing policy, candidate reordering, automatic activation, and changes to transient/hard policies. These remain outside this delivery slice. Preserve existing files, configuration and history; report unavailable integration coverage without inventing a successful result.

## Verification strategy

Reuse the completed focused implementation checks recorded below and the existing documentation-coverage evaluator. Only remaining review fixes require their affected checks to run again.

## ASCII UI preview

UI-01: Existing affected surface, after this change.

```text
Dynamic profile > Candidate
  [ ] Unclassified failures
      Consecutive failures: [2..10]
  [Apply threshold 2 to all...]

The bulk confirmation edits the draft only. Existing candidate layout is shared across desktop and phone.
```

This sketch records the affected structure; spacing is illustrative. It does not add a new control or approval requirement.
