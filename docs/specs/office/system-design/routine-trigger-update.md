---
status: current
system: office
requirements:
  - REQ-OFFICE-ROUTINE-TRIGGER-UPDATE-001
created: 2026-10-29
owners:
  - kandev
---

# Office Routine Trigger Update System Design

## Purpose and boundaries

Office owns routine triggers and their `next_run_at` cursor. This design adds an
in-place schedule update that preserves trigger identity and does not race the
scheduler's claim or re-arm. It does not change trigger creation, deletion,
dispatch, or retention.

## Requirement mapping

| Requirement | Design section |
| --- | --- |
| `REQ-OFFICE-ROUTINE-TRIGGER-UPDATE-001` | [Data and contracts](#data-and-contracts), [Control flow](#control-flow) |

## Data and contracts

`PATCH /api/v1/office/routine-triggers/:triggerId` accepts optional
`cron_expression`, `timezone`, and `enabled`. The response is the existing
`TriggerResponse` envelope with the secret cleared. A missing trigger returns
404, an invalid cron expression or timezone returns 400, and a claimed or
concurrently edited trigger returns 409.

Only the trigger's schedule fields are writable. The trigger ID, routine, kind,
webhook authentication fields, creation time, last fire, and past runs are not
part of the update.

## Control flow

The service reads the current trigger from the writer connection, applies the
requested fields, prepares the next fire with the existing trigger helper, and
writes the next row with a compare-and-set on the trigger identity plus
`cron_expression`, `timezone`, `enabled`, and `next_run_at`.

A scheduler claim clears `next_run_at`; the comparison then loses and the update
returns 409 rather than overwriting the claimed fire. Disabling clears the next
fire but does not cancel a fire already claimed. An enabled cron trigger with no
next fire is treated as owned by a dispatch and also returns 409.

## Failure and recovery

- The caller reloads after the dispatch re-arms and applies the edit again.
- An invalid expression or timezone leaves the stored trigger unchanged.
- A lost comparison leaves the stored trigger unchanged.
- Replacing or deleting the trigger to work around a 409 is not part of this
  contract.

## Testing

The endpoint is covered by the existing Office route scope inventory and by the
PR's manual/preview checks. A dedicated compare-and-set unit test is a follow-up
outside the documentation-coverage change.
