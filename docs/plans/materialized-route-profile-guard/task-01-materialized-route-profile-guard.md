---
id: "01-materialized-route-profile-guard"
title: "Refuse a profile change on a materialized dynamic route"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-DYNAMIC-AGENT-ROUTING-001
acceptance_criteria:
  - AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.28
system_design:
  - ../../specs/agents/system-design/dynamic-agent-routing-02.md
---

# Task 01: Refuse a profile change on a materialized dynamic route

## Summary

Before `startCreatedSession` persists a changed logical profile, load the
session's durable route and refuse the start when its materialized generation
belongs to a different logical profile.

## In scope

- `ErrMaterializedRouteProfileChange` and
  `Service.refuseMaterializedRouteProfileChange`.
- The call in `startCreatedSession` after profile validation and before the
  session profile write.

## Out of scope

The workflow profile-switch session handoff, concrete profiles, route actions,
and route state migration.

## Verification

- `go test ./internal/orchestrator/ -run 'TestStartCreatedSession_RefusesProfileChangeOnMaterializedDynamicRoute|TestStartCreatedSession_MaterializedRouteGuardKeepsSameProfileAndFirstSelection'`
- Existing orchestrator tests in files that start created sessions.
