---
id: "01-profile-usage-read"
title: "Read Antigravity usage by saved global profile"
status: pending
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-COSTS-SUBSCRIPTION-USAGE-002
acceptance_criteria:
  - AC-COSTS-SUBSCRIPTION-USAGE-002.2
  - AC-COSTS-SUBSCRIPTION-USAGE-002.3
  - AC-COSTS-SUBSCRIPTION-USAGE-002.4
system_design:
  - ../../specs/costs/system-design/antigravity-usage.md
---

# Task 01: Read usage by saved global profile

## Summary

Provide a read-only quota endpoint for a saved `antigravity-acp` profile using the PR #23 usage adapter and cache, with no Office instance dependency.

## Scope

- Resolve the saved, non-deleted profile, confirm agent type, and call the existing `usageProviderAdapter.GetUsage`.
- Return the existing `ProviderUsage` window envelope; distinguish missing profile, unsupported type, and source failure without leaking command output.
- Keep the route subject to existing settings read access; do not change credentials, profile configuration, Office state, or `client_antigravity.go` parsing.

## Acceptance

1. A saved Antigravity profile returns the PR #23 window shape without an Office agent; the existing usage client is invoked at most through its cache.
2. Missing ID returns 404, unsupported agent returns `{ "utilization": null }`, and a source error returns a generic unavailable response.
3. The route performs no configuration or Office write.

## Verification

After implementation, from `apps/backend`:

```text
go test ./internal/agent/settings/handlers -run 'TestCreateProfileEndpoint|TestUpdateProfileEndpoint|TestProfileWriteStoreFailuresAre500' -count=1
```

These are existing profile endpoint tests. For the new read behavior, use a disposable HTTP probe from the repository's ignored `temp/gemini-antigravity-profile-usage/` directory, outside Go's test package collection. Run it against an isolated backend with a saved Antigravity profile and no Office agent; check success, missing ID, unsupported profile, generic provider failure, and absence of writes. Record the exact probe command and observed responses in `plan.md`, then remove the probe. Verify the source behavior against PR #23's existing `go test ./internal/agent/usage -count=1` evidence only if that source changes. A permanent new handler test needs a separately approved change.

## Likely files and dependencies

- `apps/backend/internal/agent/settings/handlers/handlers.go`; reuse `profile_crud_handlers_test.go` for current endpoint behavior.
- `apps/backend/internal/backendapp/usage_adapter.go` and route wiring in `backendapp/main.go` or the current settings registration location.
- Existing `apps/backend/internal/agent/usage/types.go` defines the response; Task 02 depends on this endpoint.

## Risks

The settings handler currently does not own a usage provider dependency. Inject the existing adapter through the narrowest current registration seam; do not construct another usage service or cache.

## Results

Pending explicit implementation request.
