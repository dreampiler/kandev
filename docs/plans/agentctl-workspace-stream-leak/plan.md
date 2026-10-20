---
created: 2026-10-20
status: done
requirements:
  - REQ-PLATFORM-AGENTCTL-WORKSPACE-STREAM-LIFETIME-001
system_design:
  - ../../specs/platform/system-design/agentctl-workspace-stream-lifetime.md
legacy_specs: []
---

# Implementation Plan: Agentctl workspace stream leak

## Overview

Every stopped agentctl instance leaves its workspace stream handler parked in
`forwardWorkspaceStream`. The input loop returns on a read error without
telling the forwarding loop, and the forwarding loop only exits on a write
error, a closed subscription, credential invalidation, or the handler's own
`done`, none of which fires after the instance stops. The parked goroutine
keeps the instance's API server, process manager, ACP adapter, and MCP server
reachable, about 5.5 MB of private memory per stopped instance in a local
reproduction.

## Scope

In scope: `apps/backend/internal/agentctl/server/api/workspace.go`
(`handleWorkspaceStreamWS`, `forwardWorkspaceStream`) and a permanent API
package test.

Excluded: closing workspace streams from instance teardown, the agent event
stream, the backend client, and the stream protocol.

## Technical approach

- The handler runs `handleWorkspaceStreamInput` in a wrapper goroutine that
  closes `inputDone` when the loop returns.
- `forwardWorkspaceStream` takes `inputDone` and returns when it closes.

## Work orders

- [x] [Task 01: End the workspace stream with its input side](task-01-workspace-stream-input-end.md)

## Changed-path coverage

The work order owns `workspace.go`, the new
`workspace_stream_input_end_test.go`, and the new workspace stream lifetime
requirement and system design.
