---
status: active
system: agents
created: 2026-10-12
owners:
  - kandev
---

# Provider Account-Block Classification Requirements

## Overview

A model gateway can reject every request from an account that it has restricted, for example with `403 Forbidden` and a structured `user_blocked` code.
Without a rule for this diagnostic, the agent system falls through to an unknown post-start failure, so dynamic routing either stops for manual recovery or treats it like any other repeated unknown failure, and the restricted profile stays selectable.
A restricted account does not recover by retrying. Repeated requests can also make the restriction worse.
The agent system owns provider error classification and the dynamic candidate circuits, so it must recognize the restriction, stop selecting only the affected execution profile, and keep that profile out of rotation until its owner releases it.

## Terminology

- **Account-block diagnostic:** A provider error that reports `403 Forbidden` together with the structured account-restriction code `user_blocked` or the account-restriction message "Your access has been restricted".
- **Held circuit:** A candidate circuit that has no expiry and admits no probe. Only an owner release closes it.
- **Owner release:** The authenticated settings API request that closes the held circuit of one execution profile.

## Requirements

### REQ-AGENTS-PROVIDER-ACCOUNT-BLOCK-001: Classify and hold blocked provider accounts

**Intent:** A restricted account must stop receiving requests from its execution profile until the owner resolves the restriction, while every other candidate keeps working in its configured order.

**User story:** As an operator, I want a provider account restriction to take only the affected execution profile out of dynamic routing until I release it, so that Kandev stops sending requests to a blocked account without disturbing other profiles.

#### Acceptance criteria

- **AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.1:** A provider error line that opens with the provider error envelope `Error from provider <name>:` and reports `403 Forbidden` shall classify as `account_blocked` with high confidence when the same line carries the structured code `user_blocked` or the message "Your access has been restricted". The envelope may follow an `Internal error:`, `API Error:`, `AI_APICallError:`, or `Error:` label and an opening JSON `message` field, and the body may use plain or escaped quotes.
- **AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.2:** A `403` without an account-restriction code or message, an account-restriction code or message in a non-`403` response, and agent prose that mentions `user_blocked` or an account restriction shall not classify as `account_blocked`. Structured HTTP status precedence and every existing rule shall keep their classification.
- **AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.3:** `account_blocked` shall be a hard failure that never retries or waits on the same candidate, regardless of the candidate's saved hard-error retry and reset-wait settings or a legacy same-candidate retry rule. The candidate's saved exhausted outcome shall decide between skipping to the next eligible candidate and stopping for manual recovery. The existing pre-result safety, generation, and continuation requirements shall still apply.
- **AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.4:** An `account_blocked` failure attributed to an execution profile shall open a held circuit scoped to that execution profile only, including when the failed attempt already produced output or effects or the route already moved past that profile. Other execution profiles, including profiles that share its credential binding, their circuits, and the configured candidate order shall be unchanged.
- **AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.5:** A held circuit shall not expire, shall admit no probe, shall not close after output from another attempt, and shall survive a backend restart. Selection, fallback, and the settings preview shall treat the held profile as unavailable in every dynamic profile that lists it, and a fully held route shall stay on manual recovery without a resource-wait timer.
- **AC-AGENTS-PROVIDER-ACCOUNT-BLOCK-001.6:** `POST /api/v1/agent-profiles/:id/account-block/release` shall require the existing agent-settings mutation permission and close only the held account-block circuit of the named execution profile. It shall be idempotent, report whether a held circuit was closed, return not found for an unknown profile, and leave credential-binding, model, and other circuits, the profile's account, credentials, model, enablement, and candidate order unchanged. Enabling, disabling, or editing a profile shall not release a held circuit.

## Out of scope

- Changing provider accounts, credentials, or policy causes. The account owner resolves the restriction outside Kandev.
- Classifying every `403`, every permission or authentication denial, or every mention of a restriction.
- Account-restriction codes or messages other than the ones above until a real diagnostic is captured.
- Changing Office routing, utility calls, or concrete-profile launches beyond the shared classifier result.
- New settings or UI controls. The release is API-only.
