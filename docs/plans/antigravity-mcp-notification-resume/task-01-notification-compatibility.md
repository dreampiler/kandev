---
id: "01-notification-compatibility"
title: "Notification compatibility"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-MCP-PROTOCOL-001
acceptance_criteria:
  - AC-AGENTS-MCP-PROTOCOL-001.3
  - AC-AGENTS-MCP-PROTOCOL-001.6
  - AC-AGENTS-MCP-PROTOCOL-001.8
  - AC-AGENTS-MCP-PROTOCOL-001.9
system_design:
  - ../../specs/agents/system-design/mcp-protocol-compatibility.md
---

# Task 01: Notification Compatibility

## Scope and acceptance

Implement the design's header-only notification adapter and wire both `/mcp`
routes. The exact roots-change reproduction must return empty 202, followed by
successful tool list/call. Requests and existing metadata retain SDK validation;
transport protection and legacy behavior remain intact.

No permanent tests, dependency changes, adapter-specific ACP logic, or deployment.

## Files

- `apps/backend/internal/mcp/server/notification_protocol_compatibility.go`
- `apps/backend/internal/mcp/server/server.go`
- Existing MCP compatibility requirement, design, and dual-era ADR.
- `docs/public/automation-and-mcp.md` and this delivery package.

## Verification

Run temporary tests through a Go overlay outside normal test collection; keep
them uncommitted. From `apps/backend`:

```text
go test -overlay=../../.scratch/w032/overlay.json ./internal/mcp/server -run '^TestW032NotificationCompatibility_' -count=1 -timeout=180s
go test ./internal/mcp/server -run '^(TestMCPProtocolCompatibility_|TestExternal)' -count=1 -timeout=180s
golangci-lint run ./internal/mcp/server
```

From the repository root:

```text
python scripts/list-docs.py validate
python scripts/lint-spec-files.py --all
node scripts/validate-public-docs.mjs
node --test scripts/validate-public-docs.test.mjs
git diff --check
```

Quote the overlay argument when using PowerShell. Repeat only checks affected
by later changes. Record actual results before marking this work order done.

## Results

Temporary reproduction failed with the expected missing-metadata 400 before
implementation. Temporary notification/boundary/DNS checks and existing focused
compatibility/external tests passed after implementation. The final combined
temporary and existing focused suite passed (exit 0). Lint reported zero issues
(exit 0); catalog/specification/public-document checks passed (exit 0), including
62 existing public-document tests. Formatting and diff checks passed.

Temporary boundary checks also detected differently cased fields that the SDK
recognizes. The adapter leaves these messages untouched; their control checks
passed after that correction. No permanent test files or cases were added.
An additional temporary body-edge check passed (exit 0), covering byte and
numeric-precision preservation plus SDK body-read failure and original-body
closure.
