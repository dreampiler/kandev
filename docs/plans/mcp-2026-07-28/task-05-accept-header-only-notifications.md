---
id: "05-accept-header-only-notifications"
title: "Accept header-only modern notifications"
status: done
wave: 4
depends_on: ["01-serve-dual-era-mcp"]
plan: "plan.md"
requirements:
  - REQ-AGENTS-MCP-PROTOCOL-001
acceptance_criteria:
  - AC-AGENTS-MCP-PROTOCOL-001.9
system_design:
  - ../../specs/agents/system-design/mcp-protocol-compatibility.md
---

# Task 05: Accept Header-Only Modern Notifications

## Inputs

- `AC-AGENTS-MCP-PROTOCOL-001.9`.
- `docs/specs/agents/system-design/mcp-protocol-compatibility.md` section
  `Header-only notifications`.
- The notification compatibility amendment in
  [the dual-era decision](../../decisions/2026-08-30-dual-era-mcp-protocol.md).
- The shared route registrations in `apps/backend/internal/mcp/server/server.go`.

## Change

1. Route both `/mcp` registrations through a narrow HTTP adapter.
2. For a POST with a supported modern protocol header, supply
   `params._meta` protocol version metadata only on a valid notification
   with no `id` member and no `params._meta`.
3. Read the body lazily, when the SDK first reads it, so the SDK's transport
   checks run before the body is consumed.
4. Leave requests, explicit metadata, batches, invalid JSON, legacy versions,
   and body read errors on the SDK path.

## Acceptance

- A header-only `notifications/roots/list_changed` returns HTTP 202 with no
  body through the adapter.
- Requests with an `id` member and explicit metadata keep SDK validation.
- A request rejected by SDK DNS-rebinding protection does not read the body.

## Verification

```bash
cd apps/backend
go test ./internal/mcp/server/ -count=1
go vet ./internal/mcp/server/
```

## Files likely touched

- `apps/backend/internal/mcp/server/notification_protocol_compatibility.go`
- `apps/backend/internal/mcp/server/notification_protocol_compatibility_test.go`
- `apps/backend/internal/mcp/server/server.go`

## Dependencies

Task 01. The adapter wraps the shared dual-era server.

## Result

- Added the lazy notification adapter and wired both `/mcp` registrations to it.
- Focused tests cover acceptance, preserved SDK validation, untouched message
  shapes, legacy and unsupported versions, body read errors, and body ordering
  behind DNS-rebinding protection.
- `go test ./internal/mcp/server/ -count=1` and `go vet` passed.
