---
id: "01-hide-unconfigured-plugin-tools"
title: "Hide tools of plugins with unset required config"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-PLUGINS-AGENT-TOOLS-001
acceptance_criteria:
  - AC-PLUGINS-AGENT-TOOLS-001.2
  - AC-PLUGINS-AGENT-TOOLS-001.6
  - AC-PLUGINS-AGENT-TOOLS-001.9
  - AC-PLUGINS-AGENT-TOOLS-001.10
system_design:
  - ../../specs/plugins/system-design/agent-tool-config-readiness.md
---

# Task 01: Hide tools of plugins with unset required config

## Summary

Build the agent tool catalog only from active plugins whose required config is set,
recompute it when config is saved, and keep one plugin's unreadable config from
affecting other plugins.

## In scope and owned files

- `apps/backend/internal/plugins/agent_tools.go`: filter records by status and config
  readiness before `buildAgentToolDefinitions`; readiness helpers.
- `apps/backend/internal/plugins/service_config.go`: signal a catalog recompute after a
  committed config save, before the restart attempt.
- `apps/backend/internal/plugins/agent_tools_test.go`: focused unit tests.
- `docs/public/plugins-manifest.md` and `docs/public/plugins-authoring.md`: state that a
  plugin's tools are listed only once its required config is set.

## Out of scope

No change to manifest or config validation, secret storage, `InvokeAgentTool`, plugin
status, HTTP or WebSocket APIs, the MCP server, agentctl, or the web settings UI.

## Acceptance

1. An active plugin with required fields and no stored config contributes no tools; the
   same plugin contributes its tools after `UpdateConfig` stores every required field,
   and the save signals the catalog listener (AC 001.9, 001.10, 001.6).
2. A stored `false` or `0` satisfies a required field; an absent key, null, or blank
   string does not (AC 001.9).
3. A plugin without required fields, and a configured plugin, keep their tools; inactive
   plugins still contribute none (AC 001.2, 001.9).
4. A plugin whose config file cannot be parsed contributes no tools while another
   plugin's tools stay listed and the catalog call succeeds (AC 001.10).

## TDD and exact verification

Write the tests first and run them against the unchanged catalog. Acceptance 1, 2 (blank
string), and 4 must fail because the tools are still listed or no signal is sent, not
because of fixture setup.

From `apps/backend`:

```bash
go test ./internal/plugins/ -run 'TestAgentToolCatalog|TestServiceUpdateConfig' -count=1
go build ./...
go vet ./internal/plugins/
```

Fixtures use placeholder values only; no real credentials.
