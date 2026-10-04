---
id: "01-observe-foreground-progress"
title: "Observe foreground tool progress"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-TOOL-STALL-PROGRESS-001
acceptance_criteria:
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.2
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.4
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.7
  - AC-AGENTS-TOOL-STALL-PROGRESS-001.8
system_design:
  - ../../specs/agents/system-design/agent-stall-recovery.md
---

# Observe foreground tool progress

## Scope

Implement accepted-output counters before display truncation, an optional
agentctl stream request, and execution-local foreground CPU/root-exit sampling.
Keep background-work admission probing unchanged. Do not expose raw process
commands or logs in observation responses.

Likely files: agentctl `types/streams`, ACP `shell_progress.go` and
`adapter_tools.go`, API `tool_progress.go`, process `tool_progress*.go`, and
the runtime agentctl client.

## Acceptance

1. Output growth remains observable after truncation; duplicate cumulative
   frames contribute no progress.
2. Comparable process CPU increases count only while the pinned invoking root
   is live; reused PIDs and detached descendants cannot replace it.
3. Unavailable/unsupported observations return no evidence within the request
   budget, preserving the older-runtime fallback.

## Verification

From `apps/backend`, with the explicitly prepared temporary overlay:

```text
go test -race -overlay <temporary-overlay.json> -p 2 ./internal/agentctl/server/process ./internal/agentctl/server/api ./internal/agentctl/server/adapter/transport/acp ./internal/agent/runtime/agentctl -run 'TestW027|TestNormalizeShellTool|TestProbeBackgroundWorkloads_' -count=1 -timeout 8m
```

Run focused lint for these packages and `internal/agentctl/types/streams`.
Do not add permanent tests or a reusable verification framework.

## Results

Temporary checks confirm capped output counters, client wire/fallback behavior,
PID reuse, foreground child CPU, and detached root exclusion. Real Windows
checks run the process-manager observation path against a quiet CPU-active
PowerShell command and a redirected `Start-Process` launch whose child remains
live after the invoking shell exits. Targeted race tests and focused Go lint
passed (exit 0). Linux-target compilation of process, API, and lifecycle
packages also passed (exit 0); Linux real-process behavior was not exercised
on this Windows host.
