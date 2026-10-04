---
created: 2026-10-05
status: in_progress
requirements:
  - REQ-AGENTS-MCP-PROTOCOL-001
system_design:
  - ../../specs/agents/system-design/mcp-protocol-compatibility.md
legacy_specs: []
---

# Implementation Plan: Resumed MCP Notification Compatibility

## Overview

An agent restoring a conversation can send `notifications/roots/list_changed`
with a modern protocol header but no body metadata. The pinned SDK validates
modern metadata before notification dispatch, returning 400 and causing MCP
initialization to fail. Supplement the notification's missing version, retain
SDK dispatch, then verify transport behavior and the real resume flow.

## Scope

- Implement AC-AGENTS-MCP-PROTOCOL-001.9 in both shared `/mcp` route registrations.
- Preserve modern request validation, legacy/SSE behavior, identity, and auth.
- Reconcile the existing requirement, design, ADR, and MCP reference page.
- Use existing tests and temporary checks; add no permanent tests.

Excluded: SDK upgrade/fork, model fallback policy, third-party MCP proxying,
frontend changes, new flags or persistence, and deployment.

## Technical approach

`notification_protocol_compatibility.go` wraps the SDK handler. Only a valid
single JSON-RPC notification without an `id` or `params._meta` can be supplemented
from a supported modern HTTP header. Raw fields preserve existing payloads.
Every path delegates to the SDK, including read failures. `server.go` installs
the same adapter for agentctl and backend routes.

| Client shape | Result | Evidence |
| --- | --- | --- |
| Header-only modern notification | Supplement version; accepted empty 202 | Temporary wired-route check |
| Modern request or existing metadata | Existing SDK behavior | Existing compatibility tests and temporary controls |
| Legacy HTTP/SSE | Existing behavior | Existing compatibility and external route tests |
| Actual Antigravity resume | Tools work after restoring the same conversation | Real-client verification pending |
| Third-party MCP | No change | Outside shared server adapter |

## Work orders

- [x] [Task 01: Notification compatibility](task-01-notification-compatibility.md)
- [ ] [Task 02: Resume verification](task-02-resume-verification.md)

## Verification results

Temporary Go overlay checks reproduced the exact missing-metadata 400 before
the fix (exit 1). After the fix, both wired routes returned empty 202 and allowed
subsequent tool listing/calling; payload, ID/metadata/version boundaries, and
DNS protection passed (exit 0). Existing modern/legacy protocol and external
tests passed (exit 0). Additional controls confirmed unchanged behavior for
case variants recognized by the SDK. Lint reported zero issues; catalog,
specification, and public-document validation passed, including all 62 existing
public-document tests (exit 0). Formatting and diff whitespace checks passed.

The temporary checks are not part of this repository's permanent test suite.
Antigravity ACP 1.2.1 advertised Gemini 3.8 Flash and successfully called the
harmless MCP fixture tool on the corrected server. The restore phase did not
reach `session/load`: initialization of the replacement ACP process exceeded
the overall 240-second probe budget. Real idle/resume verification remains
pending; neither the synthetic HTTP check nor the initial real-client call
establishes that result. No production instance was changed.

## Risks

- Repairing a request or existing metadata would loosen validation.
- A direct 202 shortcut would bypass SDK protection.
- The real agent may expose another resume failure after this 400 is removed.
- Release validation must distinguish the same restored native conversation
  from a fresh session or a different-model fallback.
