---
status: draft
system: agents
created: 2026-10-12
owners:
  - kandev
---

# Fixed-Profile Resource Suspension Requirements

## Overview

A concrete agent profile ("fixed profile") runs one configured model through one
credential. Kandev already classifies provider resource failures
(`quota_limited`, `rate_limited`, `model_capacity`, `account_blocked`, and the
rest of the circuit-qualifying codes) and records a resource circuit for the
failed resource. Those circuits are recorded and enforced only for the built-in
`dynamic` family; a concrete profile is neither recorded nor gated. The result
is that a fixed profile keeps sending to the same exhausted model on every new
turn, with no visible explanation, until the provider recovers on its own.

This requirement makes the fixed-profile path honor the same resource health the
dynamic path already honors, without changing the profile's configured model and
without silently replacing the turn with another model.

## Terms

- **Fixed profile:** a concrete, launchable agent profile (any agent family
  except the built-in `dynamic` family). It has one configured model and one
  credential binding.
- **Resource circuit:** the shared, restart-safe health record for one resource
  identity (a provider account/credential binding, a model, or one execution
  profile's held account block). See the
  [provider account-block holding](../system-design/provider-account-block-classification.md)
  and [dynamic agent routing](../system-design/dynamic-agent-routing-01.md)
  designs.
- **Open circuit:** a circuit whose suspension is running (`waiting`), being
  probed, or held until owner release. Reset-wait circuits end at their reset
  instant; a held account block ends only at owner release.

## Requirements

### REQ-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001: Fixed profiles honor resource health

**Intent:** A fixed profile must not repeatedly send to a resource that its own
circuit records as unavailable, and the operator must see why the turn is not
running.

**User story:** As an operator using a fixed profile, I want a turn refused or
parked with a visible reason while the profile's model or credential circuit is
open, so that I stop spending attempts on an exhausted resource and understand
when it can be tried again.

#### Acceptance criteria

- **AC-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001.1:** When a fixed profile's
  resource circuit is open, the system shall neither start a new execution for
  that profile nor dispatch a new prompt into an execution of that profile that
  is still alive. The refusal shall be visible on the session and name the
  suspended scope (credential, model, or profile account block), the reason
  code, and either the instant the resource may be tried again or that the
  owner must release it. A session that had not started a turn waits for input;
  a running turn is not stopped and its output and effects are not replayed.
- **AC-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001.2:** The refusal shall not
  change the profile's configured model or agent, and shall not select, launch,
  or imply any other model. A fixed profile never gains cross-model fallback from
  this behavior.
- **AC-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001.3:** A fixed-profile attempt
  whose failure is classified with a circuit-qualifying code shall record the
  resource circuit of the profile the failed execution actually ran, whether or
  not the attempt already produced output, using the same resource identities
  and reset policy the dynamic path uses for an equivalent candidate. A failure
  whose execution cannot be tied to the session records nothing.
- **AC-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001.4:** A fixed-profile
  `account_blocked` failure shall apply the held account block to the failing
  execution profile only, with no expiry, no probe, and no clearing on output,
  and shall be released only through the existing owner-release action.
- **AC-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001.5:** When a fixed profile's
  circuit ends because its reset instant passes, and when an account block is
  released by the owner, a subsequent operator-initiated attempt on that profile
  shall be admitted normally, and the profile's first turn after an expired
  suspension that completes with output or an effect and no provider error
  shall clear its strike history as it does for a dynamic candidate.
- **AC-AGENTS-FIXED-PROFILE-RESOURCE-SUSPENSION-001.6:** Recording a fixed
  profile's circuit shall not change dynamic candidate order or ranking. The
  credential and model circuits are the existing shared resource keys, so a
  suspension of a shared credential or model applies to every profile that uses
  that same resource; an account block stays on the failing execution profile
  only. A fixed profile's circuit is observable through the same settings
  preview health that reports a dynamic candidate's suspension.

## Out of scope

- Dynamic-family candidate selection, ordering, tiering, and fallback, which keep
  their own contract.
- New classification rules; the existing classifier and code set are reused.
- Any automatic, timed launch of a suspended fixed profile. Only a reset/expiry
  or an owner release makes the resource eligible again; this requirement does
  not add a scheduler that starts the profile by itself.
- Office routing and utility invocations, which keep their existing launch and
  prompt paths and are not gated by this requirement. A classified failure that
  reaches the shared failure attribution still records the shared circuit.
- A concrete profile's interactive availability while its circuit is closed.
