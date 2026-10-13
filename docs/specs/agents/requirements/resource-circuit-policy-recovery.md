---
status: active
system: agents
created: 2026-10-13
owners:
  - kandev
---

# Resource Circuit Policy Recovery Requirements

## Requirements

### REQ-AGENTS-CIRCUIT-RECOVERY-001: Evidence-based windows and owner recovery

- **AC-AGENTS-CIRCUIT-RECOVERY-001.1:** OpenRouter's generic AI-SDK try-later notice shall suspend only that path/model for 5, 15, then 30 minutes. After eight completed suspensions on the same UTC date, the next failure shall wait for the next UTC reset. A new UTC date restarts this ladder. Zen's observed daily reset and explicitly classified per-day limits retain their existing policies.
- **AC-AGENTS-CIRCUIT-RECOVERY-001.2:** Loading legacy Go quota circuits shall cap an expiry longer than the current Go policy's maximum at the original `updated_at` plus that maximum (currently seven days). Shorter blocks and unrelated account holds shall remain unchanged. Repeated loads shall not slide the failure timestamp forward.
- **AC-AGENTS-CIRCUIT-RECOVERY-001.3:** An owner with the existing agent-settings mutation permission shall list and release exactly one resource circuit through Settings > Agents and its API. Release shall be durable, idempotent, clear its strike history, and leave other keys and profile settings unchanged. A failed save shall keep the circuit suspended. The existing account-block endpoint retains its profile validation and hold-only behavior.
- **AC-AGENTS-CIRCUIT-RECOVERY-001.4:** Overview account-block keys shall resolve the execution profile's name through the existing profile-name lookup, including profiles outside the initial overview model list. Desktop and phone shall offer the same individual release action.

## Scope

No production database edits, runtime builds/swaps, provider calls, permission model, or automatic prompt replay. The operator's 2026-10-13 read-only observations justify conservative retry of ambiguous OpenRouter windows; they do not establish that every limit is false.
