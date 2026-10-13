package sqlite

import (
	"strings"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

// The retention classifier is explicit: an event type that is not listed here
// is unknown, stays intact and stops prefix retirement for its stream.
//
// Output events carry agent content that the consumer applies when the event
// is projected (canonical messages, tool calls, plans).
var agentDeliveryRetentionOutputEventTypes = []string{
	streams.EventTypeMessageChunk,
	streams.EventTypeReasoning,
	streams.EventTypeToolCall,
	streams.EventTypeToolUpdate,
	streams.EventTypePlan,
	streams.EventTypeAgentPlan,
}

// Session-state events (capabilities, modes, models, MCP attachment evidence,
// context windows, permission notices, turn markers, usage, background work)
// are applied by the same lifecycle callback before their generic
// agent_delivery.event receipt commits. After projection the inbox row is
// never read back: resume and continue_from_history read only unprojected
// rows, a replayed duplicate skips the callback, and an expired replay is
// reconciled from the authenticated projected cursor. Their projected rows are
// therefore redundant transport evidence exactly like projected output, and
// they follow the same grace, replay window and contiguous retirement rules.
//
// Every stream starts with agent_capabilities, so excluding these types pinned
// every retirement checkpoint at zero and let inbox and receipt rows grow at
// the full inflow rate.
var agentDeliveryRetentionStateEventTypes = []string{
	streams.EventTypePermissionRequest,
	streams.EventTypePermissionCancelled,
	streams.EventTypeSessionStatus,
	streams.EventTypeContextWindow,
	streams.EventTypeForegroundIdle,
	streams.EventTypeBackgroundComplete,
	streams.EventTypeAvailableCommands,
	streams.EventTypeSessionMode,
	streams.EventTypeRateLimit,
	streams.EventTypeAgentCapabilities,
	streams.EventTypeSessionModels,
	streams.EventTypeSessionModelFallback,
	streams.EventTypeSessionModelSelectionWarning,
	streams.EventTypeSessionInfo,
	streams.EventTypeResponseAttemptReset,
	streams.EventTypeAuthRequired,
	streams.EventTypeMCPAttachment,
	streams.EventTypeTurnStarted,
	streams.EventTypeUsageObservation,
	streams.EventTypeBackgroundWorkUpdated,
	streams.EventTypeBackgroundWorkOutput,
}

// Terminal events need the full settlement proof before they compact or retire.
var agentDeliveryRetentionTerminalEventTypes = []string{
	streams.EventTypeComplete,
	streams.EventTypeError,
	"cancelled",
	"canceled",
}

// agentDeliveryRetentionNonterminalTypesSQL lists the nonterminal types whose
// projected rows compact after their generic receipt commits.
var agentDeliveryRetentionNonterminalTypesSQL = agentDeliverySQLStringList(
	append(append([]string{}, agentDeliveryRetentionOutputEventTypes...), agentDeliveryRetentionStateEventTypes...),
)

// agentDeliveryRetentionKnownTypesSQL lists every type the proof can classify.
var agentDeliveryRetentionKnownTypesSQL = agentDeliverySQLStringList(
	append(append(append([]string{}, agentDeliveryRetentionOutputEventTypes...),
		agentDeliveryRetentionStateEventTypes...), agentDeliveryRetentionTerminalEventTypes...),
)

func agentDeliveryRetentionNonterminalEventType(eventType string) bool {
	for _, known := range agentDeliveryRetentionOutputEventTypes {
		if eventType == known {
			return true
		}
	}
	for _, known := range agentDeliveryRetentionStateEventTypes {
		if eventType == known {
			return true
		}
	}
	return false
}

func agentDeliverySQLStringList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+strings.ReplaceAll(value, "'", "''")+"'")
	}
	return strings.Join(quoted, ", ")
}
