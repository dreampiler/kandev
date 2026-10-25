---
id: "11-zod-catalog-schema-refresh"
title: "Zod catalog schema refresh"
status: done
wave: 11
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-AGENT-RICH-OUTPUT-002
acceptance_criteria:
  - AC-AGENTS-AGENT-RICH-OUTPUT-002.2
system_design:
  - ../../specs/agents/system-design/agent-rich-output.md
---

# Task 11: Zod catalog schema refresh

## Outcome

Keep the rich-output catalog's generated MCP schema aligned with the supported
Zod version when upgrading the direct web dependency. Preserve catalog
validation semantics and the locked workspace dependency graph.

## Scope

- Update the direct `apps/web` Zod pin and synchronized workspace lockfile.
- Regenerate the MCP rich-output catalog schema with that Zod version.
- Keep the catalog component and property contract unchanged.

## Acceptance

1. The web manifest and workspace lockfile resolve Zod 4.6.6.
2. The generated MCP catalog schema matches the output from the pinned Zod
   version and preserves the existing nullable-number property semantics.

## Verification

```sh
cd apps/web && pnpm exec tsx scripts/generate-rich-output-catalog.ts --check
```

## Results

- Updated the direct Zod pin from 4.3.6 to 4.6.6 and synchronized the workspace
  lockfile; refreshed the generated backend schema whose nullable-number
  representation changed from `anyOf` to the equivalent JSON Schema type list.
- `pnpm exec tsx scripts/generate-rich-output-catalog.ts --check`: passed with
  Zod 4.6.6 installed from the frozen workspace lockfile.
- `pnpm exec vitest run lib/rich-output scripts/generate-rich-output-catalog.test.ts`:
  8 files and 159 tests passed.
- `pnpm run typecheck`: the initial run exhausted Node's 4 GiB heap limit. One
  rerun with `NODE_OPTIONS` appended per invocation as
  `--max-old-space-size=6144` passed, including `rich-output:check` and
  `tsc --noEmit`.
