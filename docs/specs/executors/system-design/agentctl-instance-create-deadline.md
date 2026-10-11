---
status: draft
system: executors
requirements:
  - REQ-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001
---

# Agentctl instance creation deadline

## Purpose and boundaries

`internal/agent/runtime/agentctl.ControlClient` is the backend client for the
agentctl control API. Docker, standalone, Kubernetes, plugin-provided, and host
utility runtimes all create instances through `ControlClient.CreateInstance`.

This design selects the client deadline for that one call. It does not change
the controller's creation logic in `internal/agentctl/server/instance`, the
control API, or any caller's launch context.

## Requirement mapping

| Requirement | Design sections |
| --- | --- |
| REQ-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001 | Deadline selection, Cancellation and errors, Verification |

## Current behavior

`NewControlClient` builds one `http.Client` with a 30-second `Timeout`. Every
control method, including `CreateInstance`, sends its request through that
client.

`POST /api/v1/instances` responds only after `instance.Manager.CreateInstance`
has built the process manager, prepared comparison targets, and started the
workspace trackers. That manager serializes creation. Concurrent session starts
therefore queue behind one another, and a request can exceed 30 seconds while
the controller is still progressing.

Callers bound creation with longer contexts. The lifecycle launch phase uses
`constants.AgentLaunchTimeout`, which includes the setup-script allowance. The
30-second client timeout was therefore the effective bound for creation.

## Deadline selection

`CreateInstance` sends its request through a per-call copy of the client's
`http.Client` whose `Timeout` is `createInstanceHTTPTimeout`, 120 seconds.

The copy shares the configured `Transport`. The bearer credential
(`authTransport`) and the runtime binding guard (`runtimeBindingTransport`)
therefore apply exactly as they do for other control calls. A copy made per
call observes the transport that is installed at that time.

The client's shared `http.Client` keeps its 30-second `Timeout`. All other
control methods continue to use it.

A fixed constant is sufficient. No configuration key, option, or environment
variable is introduced.

## Cancellation and errors

The request keeps the caller's context through `http.NewRequestWithContext`.
The effective bound is the earlier of the caller's deadline or cancellation and
the 120-second client timeout.

Transport and timeout failures keep the existing `failed to create instance`
error wrapping. Non-success responses, response decoding, and logging are
unchanged.

## Known limits

A longer client deadline lets a slow but progressing creation complete. It does
not reduce creation latency or the controller's serialization. A creation that
exceeds 120 seconds still fails. Abandoned-creation cleanup remains the
controller's existing `ctx.Err()` checks.

## Verification

Tests stay alongside the client in `internal/agent/runtime/agentctl`:

- A creation request observes a deadline close to 120 seconds through a
  stubbed transport (`AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.1`).
- The existing constructor test keeps the shared 30-second timeout
  (`AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.2`).
- A caller deadline shorter than 120 seconds remains the request deadline and
  fails creation (`AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.3`).
- A token-authenticated client's creation request still carries the bearer
  credential (`AC-EXECUTORS-AGENTCTL-CREATE-DEADLINE-001.4`).
