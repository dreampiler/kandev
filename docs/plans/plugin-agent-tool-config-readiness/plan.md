---
created: 2026-10-12
status: done
requirements:
  - REQ-PLUGINS-AGENT-TOOLS-001
system_design:
  - ../../specs/plugins/system-design/agent-tool-config-readiness.md
legacy_specs: []
---

# Implementation plan: agent tool config readiness

## Outcome and ownership

Task agents see a plugin's tools only once the plugin can serve them. An active plugin
that declares required config fields contributes no agent tools until the operator sets
every required field, and saving the config makes its tools appear without a restart.
Configured plugins, plugins without required fields, and every other plugin keep their
tools.

The plugin service owns the authoritative catalog, so the change stays in
`apps/backend/internal/plugins`. MCP handlers, the refresher, agentctl, and the web
settings UI are unchanged.

## Existing contract and design

- [Requirements](../../specs/plugins/requirements/agent-tools.md), adding
  `AC-PLUGINS-AGENT-TOOLS-001.9` and `.10`.
- [Config readiness design](../../specs/plugins/system-design/agent-tool-config-readiness.md).
- [Original agent tools plan](../plugin-agent-tools/plan.md).

## Work orders

| Wave | Work order | Outcome |
| --- | --- | --- |
| 1 | [Hide tools of plugins with unset required config](task-01-hide-unconfigured-plugin-tools.md) | Config-ready catalog membership, recompute on save, failure isolation, docs |

## Verification

Focused unit tests in `apps/backend/internal/plugins` (catalog membership, readiness
rule, recompute signal, failure isolation), then `go build ./...` and `go vet` for the
package from `apps/backend`.
