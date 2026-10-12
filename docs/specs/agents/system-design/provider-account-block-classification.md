---
status: current
system: agents
requirements:
  - REQ-AGENTS-PROVIDER-ACCOUNT-BLOCK-001
---

# Provider Account-Block Classification System Design

## Purpose and boundaries

The agent system owns provider error classification, candidate circuits, and route selection.
This design adds one classification, one held circuit kind on its own key, and one owner release. It does not change other rules, the shared credential-binding and model circuits, candidate order, or Office routing.

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| `REQ-AGENTS-PROVIDER-ACCOUNT-BLOCK-001` | [Classification](#classification), [Routing decision](#routing-decision), [Held circuit](#held-circuit), [Recording the hold](#recording-the-hold), [Owner release](#owner-release), [Verification](#verification) |

## Classification

`routingerr` adds `account_blocked` as a hard class code.
Its invariants are `FallbackAllowed: true`, `AutoRetryable: false`, and `UserAction: false`. It is not an availability code, so it does not trigger "change the model" guidance.

One provider-neutral rule, `provider.account_blocked.v1`, has high confidence. It matches only one line that:

1. opens with `Error from provider <name>:`, optionally after an `Internal error:`, `API Error:`, `AI_APICallError:`, or `Error:` label and an opening JSON `message` field;
2. continues with `403 Forbidden`; and
3. carries `"code":"user_blocked"`, with plain or escaped quotes, or the message "Your access has been restricted" later on the same line.

The observed diagnostic is:

```text
Error from provider meta-contributor: 403 Forbidden {"error":{"code":"user_blocked","message":"Your access has been restricted due to repeated policy violations."}}
```

The rule is provider-neutral because the envelope comes from the gateway, not from one adapter, so no provider ID is guessed.
The line anchor keeps quoted or summarized prose out, as the [provider error signature](provider-error-signatures.md) rules do. Other account-restriction codes are added only with their own captured diagnostic.
The structured HTTP 403 mapping (`permission_denied_by_user`) is unchanged. The dynamic failure path classifies text without an HTTP status, so the two do not compete.

## Routing decision

`routingpolicy.Evaluate` treats `account_blocked` as exhausted immediately. It skips retry and reset-wait even when the candidate's hard policy enables them, then applies the candidate's `on_exhausted` outcome: `skip` selects the next eligible candidate, and `stop` keeps the route on manual recovery.
For candidates that still use legacy rules, a same-candidate retry action becomes a try-next action for this code.
Pre-result safety, the route generation fence, interrupted-turn rules, and the continuation package are unchanged. `account_blocked` is not an interrupted-turn code, so a failure after output stays on manual recovery.

## Held circuit

The hold lives on its own key, `account_block:<execution profile ID>`. It never shares a key with the credential-binding key (`credential:` or `profile:`) or the model key, so opening or releasing it cannot affect another circuit or a profile that shares the credential.
`Candidate.resourceKeys` includes this key, so admission, fallback, retry selection, and `SuspensionFor` check it together with the binding and model keys.

`CircuitRegistry.Hold` opens the circuit with code `account_blocked` and no deadline. An open circuit with a hold code is held:

- `IsOpen` reports it open and `AcquireProbe` never grants a lease, so no selection probes it;
- `RecordSuccess` does not close it;
- `Open` does not overwrite it;
- `Inspect` reports `waiting` with no end time and the `account_blocked` code, so the preview shows the candidate as blocked and `resourceWaitDeadline` keeps a fully held route on manual recovery.

The existing snapshot stores state, code, and a null deadline in `dynamic_resource_circuits`, and the existing restore path reloads it after restart without a schema change.
The preview reports the hold with suspension scope `profile`.

## Recording the hold

`ApplyFailureContext` already opens circuits before it evaluates the policy, so a pre-result failure holds the profile even when the outcome is stop.
The orchestrator also records the hold for a current dynamic task-session failure that is not routed, such as one after output or effects, through the existing `RecordResourceFailure`. The held profile is the failed execution's profile; it falls back to the route's current profile only when the failure carries the route's current execution ID. A failure from a superseded candidate is recorded through the existing superseded-candidate path.
Recording the hold never launches a successor or replays the failed turn.

## Owner release

`POST /api/v1/agent-profiles/:id/account-block/release` is registered with the existing `org.config.manage` scope and settings interlock.
The settings controller checks that the profile exists and calls the runtime through a narrow seam. The runtime calls `CircuitRegistry.ReleaseHold` on that profile's `account_block:` key, which closes a held circuit, clears its strikes, and persists the closed snapshot. It does nothing for a circuit that is not held and never reads another key.
The response is `{"released": true}` when a hold was closed and `{"released": false}` otherwise. An unknown profile returns `404`, and a runtime without dynamic routing returns `503`.
Profile enablement and edits do not call the release, so they cannot unblock a restricted account.

## Verification

- Classifier tests: the exact diagnostic, the escaped-quote and labeled forms, the message-only form, a plain `403`, a non-`403` body with `user_blocked`, and prose mentions.
- Policy tests: a hard policy with retry and reset-wait enabled still skips or stops for `account_blocked`.
- Circuit and engine tests: profile-scoped hold, no probe, no expiry, no close on success, restore after restart, a credential-sharing profile and other candidates still selectable in order, and release closes only the held key.
- Release endpoint test: permission, idempotence, unknown profile, and no change to other circuits.
- Orchestrator test: a post-output `account_blocked` failure holds the failing profile without launching a successor.
