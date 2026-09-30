---
id: "01-classify-opencode-credit-exhaustion"
title: "Classify opencode credit-exhaustion diagnostics"
status: done
wave: 1
depends_on: []
plan: "plan.md"
requirements:
  - REQ-AGENTS-OPENCODE-CREDIT-001
acceptance_criteria:
  - AC-AGENTS-OPENCODE-CREDIT-001.1
  - AC-AGENTS-OPENCODE-CREDIT-001.2
  - AC-AGENTS-OPENCODE-CREDIT-001.3
system_design:
  - ../../specs/agents/system-design/opencode-credit-classification.md
---

# Task 01: Classify opencode credit-exhaustion diagnostics

## Summary

Recognize the bounded credit-exhaustion wording in `opencode-acp` stderr as a
high-confidence `quota_limited` classification that allows fallback, so dynamic
routing advances past a depleted credit-plan account.

## In scope

- Add the `opencode.stderr.credit.v1` rule to the `opencode-acp` rules in
  `apps/backend/internal/agent/runtime/routingerr/rules.go`, matching only
  `credit limit reached`, `out of credits`, `insufficient credit(s)`,
  `insufficient balance`, and `payment required`.
- Add the observed diagnostic as a focused case in the existing
  `classify_test.go`.

## Out of scope

- Rules for providers other than `opencode-acp`.
- Reset-time parsing for credit-exhaustion text.
- Candidate ordering, circuit behavior, or provider selection.

## Acceptance

- `AC-AGENTS-OPENCODE-CREDIT-001.1`: the observed diagnostic classifies as
  `quota_limited` at high confidence with fallback allowed.
- `AC-AGENTS-OPENCODE-CREDIT-001.2`: a bare "credit" mention does not match.
- `AC-AGENTS-OPENCODE-CREDIT-001.3`: the rule does not match the redacted
  renewal URL or date.

## Verification

```bash
gofmt -w apps/backend/internal/agent/runtime/routingerr/rules.go apps/backend/internal/agent/runtime/routingerr/classify_test.go
cd apps/backend && go test ./internal/agent/runtime/routingerr -count=1
cd apps/backend && golangci-lint run ./internal/agent/runtime/routingerr --new-from-rev=<base-sha> --timeout=5m
python3 scripts/list-docs.py validate
python3 scripts/lint-spec-files.py --all
git diff --check
```

## Results

- Rule `opencode.stderr.credit.v1` added in `rules.go`; focused test
  `TestClassify_OpenCodeCreditLimitReachedIsHighConfidenceQuota` added in
  `classify_test.go`.
- `go test ./internal/agent/runtime/routingerr -count=1` passed (the
  pre-existing Windows-only `TestRemediateNpxCache_*` home-path failure is
  unrelated and skips in CI).
- `golangci-lint run ... --new-from-rev=<base-sha>`: 0 issues.
- `python3 scripts/list-docs.py validate` and
  `python3 scripts/lint-spec-files.py --all`: pass.
