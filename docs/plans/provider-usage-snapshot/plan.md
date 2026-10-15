---
created: 2026-10-15
status: in_progress
requirements:
  - REQ-COSTS-PROVIDER-USAGE-SNAPSHOT-001
system_design:
  - ../../specs/costs/system-design/provider-usage-snapshot.md
legacy_specs: []
---

# Provider usage snapshot implementation

Deliver one native settings/API/client slice on current upstream. This package
covers provider snapshots only; numeric limits, learning and final-call admission
remain separate increments.

## Work orders

- [ ] [01: Native snapshot and settings](task-01-native-snapshot.md)

## Compatibility

| Binding | Reading | Unknown fallback |
| --- | --- | --- |
| Local Claude/Codex shared subscription credential | Existing provider client/cache | Unsupported profile override or unavailable credential/read |
| Explicit local TeamClaude proxy | Existing resolver/client/cache | Unrecognized proxy binding |
| OpenCode Go paid model | Account-wide paid windows | Missing/invalid credential, payload or reset |
| OpenCode Go free / Zen | Account/model identity only | No usage API; no paid windows |
| Other or remote binding | No host read | Unsupported binding |

## ASCII UI preview

UI-01, Settings > Agents, same wrapping composition on desktop and phone:

```text
Profile name                      [existing actions]
[model] [existing fallback]
Provider / shared account / paid models
Provider reading: 5-hour 42% / reset <time>  [Refresh]
```

Loading, unavailable and no-API rows use an explicit localized state instead of
the percentage. The page retains its scroll owner; the refresh button is 44px
and above the card link. Existing model/fallback/actions are preserved.

## Verification

Use existing runtime flag/profile/config and frontend feature contract checks.
Disposable fake HTTP/store and UI checks cover AC-001.1 through .5 without real
provider/model calls. Keep temporary proof outside normal test collection and
remove it after inspection; no permanent test framework or recurring checks.
Exact commands and results belong in the work order. No operating activation.

## Remaining original work

Other provider adapters, observed-window/learned-state migration, owner values,
account/model/group admission and resets remain separate T1 work. Statistics
and existing cache-backoff contributions retain their current owners.
