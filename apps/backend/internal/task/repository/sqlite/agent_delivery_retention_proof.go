package sqlite

import (
	"strings"
)

// A terminal can stop protecting its stream only when its exact compact
// settlement and workflow evidence survives independently of the inbox.
var agentDeliveryRetainedTerminalProofSQL = `EXISTS (
 SELECT 1 FROM agent_delivery_submissions settled
 JOIN agent_delivery_effects term ON term.effect_key =
  'agent_delivery.terminal:' || terminal.stream_id || ':' || CAST(terminal.sequence AS TEXT)
 JOIN agent_delivery_effects workflow ON workflow.effect_key = 'workflow.on_turn_complete:' || term.turn_id
 WHERE settled.id = terminal.submission_id
  AND settled.session_id = terminal.session_id AND settled.incarnation_id = terminal.incarnation_id
  AND settled.harness_generation = terminal.harness_generation
  AND settled.fully_settled_at IS NOT NULL
  AND settled.state IN ('completed', 'failed', 'cancelled') AND settled.outcome = settled.state
  AND EXISTS (
   SELECT 1 FROM agent_delivery_effects latest
   JOIN agent_delivery_effects wf ON wf.effect_key = 'workflow.on_turn_complete:' || settled.terminal_turn_id
   WHERE latest.effect_key = 'agent_delivery.terminal:' || settled.terminal_stream_id || ':' || CAST(settled.terminal_sequence AS TEXT)
    AND latest.effect_type = 'agent_delivery.terminal_settlement'
    AND latest.state = 'completed' AND latest.completed_at IS NOT NULL
    AND latest.stream_id = settled.terminal_stream_id AND latest.sequence = settled.terminal_sequence
    AND latest.session_id = settled.session_id AND latest.incarnation_id = settled.incarnation_id
    AND latest.harness_generation = settled.harness_generation AND latest.submission_id = settled.id
    AND latest.turn_id = settled.terminal_turn_id AND latest.outcome = settled.outcome
    AND wf.effect_type = 'workflow.on_turn_complete' AND wf.state = 'completed' AND wf.completed_at IS NOT NULL
    AND wf.stream_id = latest.stream_id AND wf.sequence = latest.sequence
    AND wf.session_id = latest.session_id AND wf.incarnation_id = latest.incarnation_id
    AND wf.harness_generation = latest.harness_generation AND wf.submission_id = latest.submission_id
    AND wf.turn_id = latest.turn_id AND wf.outcome IN ('applied', 'no_automatic_transition', 'not_required')
  )
  AND settled.terminal_stream_id = terminal.stream_id AND settled.terminal_sequence >= terminal.sequence
  AND term.effect_type = 'agent_delivery.terminal_settlement' AND term.state = 'completed'
  AND term.completed_at IS NOT NULL AND term.stream_id = terminal.stream_id AND term.sequence = terminal.sequence
  AND term.session_id = terminal.session_id AND term.incarnation_id = terminal.incarnation_id
  AND term.harness_generation = terminal.harness_generation AND term.submission_id = settled.id
  AND term.turn_id <> ''
  AND ((terminal.event_type = 'complete' AND term.outcome IN ('completed', 'failed', 'cancelled'))
   OR (terminal.event_type = 'error' AND term.outcome = 'failed')
   OR (terminal.event_type IN ('cancelled', 'canceled') AND term.outcome = 'cancelled'))
  AND (settled.terminal_sequence > terminal.sequence OR
   (settled.terminal_turn_id = term.turn_id AND settled.outcome = term.outcome))
  AND workflow.effect_type = 'workflow.on_turn_complete' AND workflow.state = 'completed'
  AND workflow.completed_at IS NOT NULL AND workflow.stream_id = term.stream_id AND workflow.sequence = term.sequence
  AND workflow.session_id = term.session_id AND workflow.incarnation_id = term.incarnation_id
  AND workflow.harness_generation = term.harness_generation AND workflow.submission_id = term.submission_id
  AND workflow.turn_id = term.turn_id AND workflow.outcome IN ('applied', 'no_automatic_transition', 'not_required')
  AND NOT EXISTS (
   SELECT 1 FROM session_recovery_blocks blocked WHERE blocked.session_id = settled.session_id
    AND blocked.incarnation_id = settled.incarnation_id AND blocked.expected_generation = settled.harness_generation
    AND blocked.consumer_reference = 'agent_delivery' AND blocked.delivery_submission_id = settled.id
    AND blocked.state = 'open' AND ` + agentDeliveryLiveRecoveryBlockOwnerSQL + `
  )
)`

// A recovery block stays the admission fence of its session until an operator
// resolves it, but it protects delivery evidence only while its session can
// still be recovered as the task's current conversation. A block whose session
// is terminal (COMPLETED, FAILED, CANCELLED) or was superseded by a newer
// primary session of the same task never resolves on its own; keeping its
// rows froze payload cleanup and prefix retirement for that incarnation
// forever. No recovery path reads a projected inbox row: block binding and
// recovery actions use the retained submission, terminal and workflow
// receipts, and an expired replay reconciles from the authenticated projected
// cursor. The block itself is left untouched so that reviving such a session
// still requires the same explicit recovery choice.
var agentDeliveryLiveRecoveryBlockOwnerSQL = `EXISTS (
   SELECT 1 FROM task_sessions block_owner
   WHERE block_owner.id = blocked.session_id
    AND block_owner.state NOT IN ('COMPLETED', 'FAILED', 'CANCELLED')
    AND NOT (COALESCE(block_owner.is_primary, 0) = 0 AND EXISTS (
     SELECT 1 FROM task_sessions newer_primary
     WHERE newer_primary.task_id = block_owner.task_id AND newer_primary.id <> block_owner.id
      AND newer_primary.is_primary = 1 AND newer_primary.started_at > block_owner.started_at
    ))
  )`

// Fork guard: an open recovery block of a live session preserves event
// evidence for its session incarnation. Every event payload path must carry
// it, including the independent-output branch that skips the settled-submission proof.
var agentDeliveryOpenRecoveryBlockGuardSQL = `NOT EXISTS (
  SELECT 1 FROM session_recovery_blocks blocked
  WHERE blocked.session_id = source.session_id
   AND blocked.incarnation_id = source.incarnation_id AND blocked.state = 'open'
   AND ` + agentDeliveryLiveRecoveryBlockOwnerSQL + `
 )`

// Known canonical output is redundant after its generic effect commits, even
// while the prompt is active. Prompt and terminal settlement remain separate.
// A scalar identity probe keeps PostgreSQL from hashing lifetime submissions.
var agentDeliveryRetainedSubmissionProofSQL = `((submission_id = '' OR (
 SELECT 1 FROM agent_delivery_submissions settled
 WHERE settled.id = source.submission_id AND settled.session_id = source.session_id
  AND settled.incarnation_id = source.incarnation_id AND settled.harness_generation = source.harness_generation
  AND settled.state IN ('prepared', 'dispatching', 'accepted', 'interrupted_unknown', 'completed', 'failed', 'cancelled')
 LIMIT 1
) IS NOT NULL)
 AND ` + agentDeliveryOpenRecoveryBlockGuardSQL + `)`

func retentionCandidateProofQuery(query string) string {
	query = strings.ReplaceAll(query, "/* unsettled terminal */", "NOT "+agentDeliveryRetainedTerminalProofSQL)
	query = strings.ReplaceAll(query, "/* settled event */", agentDeliveryRetainedSubmissionProofSQL)
	query = strings.ReplaceAll(query, "/* no open recovery block */", agentDeliveryOpenRecoveryBlockGuardSQL)
	return query
}

func retentionEventPayloadSupported(eventType string, terminal bool) bool {
	if terminal {
		return deliveryTerminalOutcome(eventType) != ""
	}
	return agentDeliveryRetentionNonterminalEventType(eventType)
}
