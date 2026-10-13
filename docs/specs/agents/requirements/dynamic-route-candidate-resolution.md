---
status: draft
system: agents
created: 2026-10-13
owners:
  - kandev
---
# Dynamic Route Candidate Resolution Requirements

## Overview

A concrete agent profile can be a candidate inside one or more dynamic profiles.
When an administrator disables or deletes that concrete profile, the existing
profile-in-use contract already refuses the change and reports the referencing
dynamic profiles. Today the settings surface shows only a generic save failure,
so the administrator has no way to finish the change without first opening every
dynamic profile and editing it by hand.

This requirement defines an explicit, informed resolution step: when the change
would break a dynamic route, the administrator chooses whether the candidate is
disabled in place or removed from every referencing route, or cancels. This
extends [Dynamic Agent Routing](dynamic-agent-routing.md)
(`AC-AGENTS-DYNAMIC-AGENT-ROUTING-001.8`) and the existing
[profile-in-use dependency flow](../system-design/dynamic-agent-routing-01.md).
Agents owns this contract because it owns profile data, candidate membership, and
the dynamic routing document.

## Terminology

- **Concrete profile:** an executable agent profile (`kind=concrete`), the only
  kind that can be a dynamic candidate.
- **Candidate reference:** one `dynamic_agent_routes` row whose
  `execution_profile_id` names the concrete profile being disabled or deleted.
- **Tier:** one maximal contiguous run of candidate rows connected by
  `selection.join_previous`. The run's first row is the tier head and owns the
  tier's `mode` and `on_failure` settings.
- **Resolution request:** an `operation` (`disable` or `delete`) paired with an
  `action` (`disable_candidate` or `remove_candidate`) applied to every candidate
  reference of the target profile. The confirmation's display choices map to
  these pairs; the effective pair is what the server stores.
- **Disable operation:** taking the target profile out of new work selection
  while keeping its row and configuration (`enabled=false`).
- **Delete operation:** deleting the target profile row.

## Requirements

### REQ-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001: Resolve dynamic route references when disabling or deleting a candidate profile

**Intent:** When a concrete profile cannot be disabled or deleted because dynamic
profiles reference it as a candidate, the administrator needs a clear choice
about what happens to those references, applied safely across every referencing
dynamic profile, instead of a dead-end save error.

**User story:** As an administrator, I want to resolve the dynamic routes that
reference a profile before disabling or deleting it, so that I can complete the
change without editing each dynamic profile by hand.

#### Acceptance criteria

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.1:** When a concrete profile is a
  candidate of at least one dynamic profile and the administrator requests a
  disable or delete that the existing dynamic-reference check refuses, the
  settings surface shall present a resolution confirmation instead of only a save
  failure. The confirmation shall name each referencing dynamic profile and, for
  each, the tier or candidate position where the profile appears.

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.2:** The confirmation shall offer
  exactly two positive choices plus Cancel, adapted to the entry point.
  - On the disable entry point: `disable_candidate` sets the candidate
    `enabled=false` in every referencing dynamic profile and then disables the
    target profile (`operation=disable`, `action=disable_candidate`);
    `remove_candidate` removes the candidate row from every referencing dynamic
    profile and then disables the target profile (`operation=disable`,
    `action=remove_candidate`).
  - On the delete entry point: the disable-instead choice disables the candidate
    in every referencing dynamic profile and disables the target profile instead
    of deleting it (`operation=disable`, `action=disable_candidate`), and its copy
    shall state that the profile is not deleted; `remove_candidate` removes the
    candidate row from every referencing dynamic profile and then deletes the
    target profile (`operation=delete`, `action=remove_candidate`).

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.3:** `operation=delete` with
  `action=disable_candidate` shall be rejected. A delete shall remove the
  candidate from every referencing dynamic profile before the profile row is
  removed, so a disabled-but-retained reference never points at a deleted
  profile.

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.4:** When `remove_candidate` removes a
  tier head (the first row or a row with `selection.join_previous=true` set to
  false), the next candidate row in the same tier shall become the new head and
  inherit that tier's `mode` and `on_failure` settings. Candidate positions shall
  renumber contiguously. No other tier shall be merged, and no configured
  candidate order shall be reordered.

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.5:** The resolution shall change every
  referencing dynamic profile and the target profile in one request. A partial
  result shall be impossible: the server shall validate the target disable/delete
  and every other reference limit inside the same transaction, and if any part
  fails, every referencing dynamic profile and the target profile shall retain
  their pre-request state.

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.6:** Each referencing dynamic profile
  shall be applied under the existing optimistic-version contract. A version
  conflict on any referencing dynamic profile shall reject the whole request with
  the existing conflict response and change no rows; Cancel shall change nothing.
  The server shall re-read the current reference set inside the transaction; when
  a new referencing dynamic profile appeared or the reference set or any
  referencing dynamic profile version differs from the confirmation snapshot, the
  whole request shall be rejected with the existing conflict contract rather than
  partially updated or ignored.

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.7:** Routing-tier and utility-agent
  references shall keep their existing block and confirmation behavior. The
  dynamic resolution shall neither bypass them nor use a blanket force to skip
  them, and a dynamic-reference lookup failure shall block the change.

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.8:** The resolution shall introduce no
  new permission model or settings screen and shall reuse the existing interim
  settings interlock. All new copy shall use the `t()`/`Trans` catalog contract,
  including the Korean catalog.

- **AC-AGENTS-DYNAMIC-ROUTE-RESOLUTION-001.9:** The resolution shall be reachable
  from the profile editor save, the profile list-row enabled toggle, and the
  profile delete flow, and shall provide the same choices and outcomes on desktop
  and phone using the existing confirmation surfaces and touch targets.

## Out of scope

- Auto-resolving routing-tier, utility-agent, session, watcher, or automation
  references; those keep their own flows.
- Changing the dynamic router's runtime selection, classification, health, or
  failure policy. This requirement only changes how a candidate reference is
  disabled or removed at configuration time.
- Editing an unrelated candidate's tier, model options, or order, or merging
  tiers when a candidate is removed.
- New permission checks, a new settings route, or a bulk dynamic-profile editor.
