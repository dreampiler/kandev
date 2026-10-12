---
id: "02-transient-probe-fallback"
title: "Transient probe fallback for native OpenCode launches"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-OPENCODE-V2-001
acceptance_criteria:
  - AC-AGENTS-OPENCODE-V2-001.11
  - AC-AGENTS-OPENCODE-V2-001.12
system_design:
  - ../../specs/agents/system-design/opencode-v2-adoption.md
---

# Task 02: Transient probe fallback for native OpenCode launches

## Summary

Keep a native OpenCode launch alive when the version probe fails transiently
after its retries and no recent detection is cached, by using the selected
runtime's known version. An unsupported major and an unusable selected version
stay fail-closed.

## In scope

- Mark the definite unsupported-major answer so a transient run failure can be
  told apart from it.
- Resolve managed OpenCode command options from the selected runtime's version
  when the probe fails transiently.
- Continue the interactive passthrough launch on the already selected native
  binary when the probe fails transiently.

## Out of scope

- Backend and executor-type resolution.
- The probe's own retry and cache policy, which is unchanged.

## Acceptance

- A transient probe failure with a usable selected version returns options
  carrying that version instead of the probe error.
- A probe failure whose selected version is unusable preserves the probe error.
- An unsupported-major probe error is never softened into a fallback.
- A passthrough launch with the native binary already selected and on PATH
  proceeds on a transient probe failure and still rejects an unsupported major.

## Verification

From the repository root:

```bash
(cd apps/backend && go test -trimpath ./internal/agent/agents ./internal/agent/runtime/lifecycle -count=1)
(cd apps/backend && go vet -trimpath ./internal/agent/agents ./internal/agent/runtime/lifecycle)
(cd apps/backend && go build ./...)
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

## Files likely touched

- apps/backend/internal/agent/agents/opencode_native_detect.go
- apps/backend/internal/agent/agents/opencode_native_detect_test.go
- apps/backend/internal/agent/runtime/lifecycle/manager_launch.go
- apps/backend/internal/agent/runtime/lifecycle/manager_passthrough.go
- apps/backend/internal/agent/runtime/lifecycle/fallback_opencode_options_test.go
- docs/specs/agents/system-design/opencode-v2-adoption.md

## Dependencies

None.

## Risks

A transient probe failure now proceeds with the selected version's arguments
instead of blocking the launch; that is safe because the ACP arguments are
identical across the supported majors of a family, and an unsupported major
remains fail-closed.

## Parallelism

`sequential`

## Inputs

- REQ-AGENTS-OPENCODE-V2-001 and AC-AGENTS-OPENCODE-V2-001.11 and .12.
- The linked OpenCode v2 adoption design.

## Results

- RED: a transient probe failure returned the probe error and blocked the
  launch even though the native binary was selected and its version known.
- GREEN: the fallback tests and the detection-answer test pass; an unsupported
  major and an unusable selected version still fail closed.
