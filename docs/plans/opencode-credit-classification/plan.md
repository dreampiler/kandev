---
status: planned
created: 2026-09-30
requirements:
  - REQ-AGENTS-OPENCODE-CREDIT-001
system_design:
  - ../../specs/agents/system-design/opencode-credit-classification.md
---

# OpenCode credit-exhaustion classification

## Overview

OpenCode against a credit-metered provider plan reports a depleted account as a
stderr diagnostic naming the credit plan. The diagnostic matches none of the
current `opencode-acp` rules, so it falls through to the post-start phase
fallback with fallback disabled and dynamic routing stalls on the exhausted
account. Recognize the bounded credit-exhaustion wording as a high-confidence
quota classification so routing advances.

## Technical approach

Add one `opencode-acp` stderr rule after the period usage-limit rule. It
matches only the bounded credit-exhaustion phrases anchored on word boundaries
and yields `quota_limited` at high confidence with fallback allowed. The bare
word "credit" is deliberately unmatched. Sanitization collapses the renewal URL
and date before classification, so the rule never depends on that text.

## Delivery order

1. [task-01-classify-opencode-credit-exhaustion.md](task-01-classify-opencode-credit-exhaustion.md):
   add the rule and the focused classification test in one work order.

## Verification strategy

- `go test ./internal/agent/runtime/routingerr -count=1` covers the new
  classification case and proves existing classifications are unchanged.
- `golangci-lint run ./internal/agent/runtime/routingerr
  --new-from-rev=<base-sha> --timeout=5m` covers changed-code limits.
