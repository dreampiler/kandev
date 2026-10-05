package acp

import (
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

// claudeRateLimitMetaKey is the Claude agent's rate-limit observation carried in
// a usage update's `_meta`, next to the origin key usageLifecycleEvent reads.
const claudeRateLimitMetaKey = "_claude/rateLimit"

// claudeRateLimitProvider names the provider the observation belongs to. The
// meta carries no provider identity of its own.
const claudeRateLimitProvider = "anthropic"

// claudeRateLimitWindow reads the subscription window a running Claude agent
// reported and returns it in the normalized shape, or nil when the frame carries
// no usable observation.
//
// This is a defensive untyped-map recognizer: every field is validated before it
// becomes a typed value, so a shape change degrades to no observation rather than
// to a plausible-looking wrong number.
func claudeRateLimitWindow(meta map[string]any, now time.Time) *streams.RateLimitWindow {
	info, ok := meta[claudeRateLimitMetaKey].(map[string]any)
	if !ok {
		return nil
	}
	windowType, ok := knownRateLimitWindowType(stringField(info, "rateLimitType"))
	if !ok {
		return nil
	}
	utilization, ok := fractionField(info, "utilization")
	if !ok {
		return nil
	}
	window := &streams.RateLimitWindow{
		Provider:    claudeRateLimitProvider,
		WindowType:  windowType,
		Utilization: utilization,
		ResetsAt:    epochSecondsField(info, "resetsAt"),
		Status:      knownRateLimitStatus(stringField(info, "status")),
		Overage:     boolField(info, "isUsingOverage"),
		ObservedAt:  now,
	}
	return window
}

// knownRateLimitWindowType accepts only the kinds this version of the provider
// documents. An unknown kind has no window length or label, so it is not a
// reading rather than a guessed one.
func knownRateLimitWindowType(raw string) (string, bool) {
	switch raw {
	case streams.RateLimitWindowFiveHour, streams.RateLimitWindowSevenDay,
		streams.RateLimitWindowSevenDayOpus, streams.RateLimitWindowSevenDaySonnet,
		streams.RateLimitWindowSevenDayOverageIncluded, streams.RateLimitWindowOverage:
		return raw, true
	default:
		return "", false
	}
}

// knownRateLimitStatus copies a documented status verbatim and drops anything
// else, so an unrecognized value is not read as an exhausted window.
func knownRateLimitStatus(raw string) string {
	switch raw {
	case streams.RateLimitStatusAllowed, streams.RateLimitStatusAllowedWarning,
		streams.RateLimitStatusRejected:
		return raw
	default:
		return ""
	}
}

// fractionField reads a utilization fraction. The 0–1 range is the provider's
// unit for this field; a value outside it is a different shape, not a percentage
// to be reinterpreted. Booleans and strings are not numbers.
func fractionField(info map[string]any, key string) (float64, bool) {
	value, ok := numberField(info, key)
	if !ok || value < 0 || value > 1 {
		return 0, false
	}
	return value, true
}

// numberField reads a JSON number. Go decodes every JSON number as float64 and
// decodes true as bool, so a bool never reaches the numeric path.
func numberField(info map[string]any, key string) (float64, bool) {
	value, ok := info[key].(float64)
	return value, ok
}

// epochSecondsField reads an epoch-seconds instant. An absent, non-numeric or
// non-positive value yields the zero time, which leaves the window without a
// usable reset rather than with a wrong one.
func epochSecondsField(info map[string]any, key string) time.Time {
	seconds, ok := numberField(info, key)
	if !ok || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0).UTC()
}

func stringField(info map[string]any, key string) string {
	value, _ := info[key].(string)
	return value
}

func boolField(info map[string]any, key string) bool {
	value, _ := info[key].(bool)
	return value
}

// claudeRateLimitEvent wraps a recognized window in its own stream event. It is
// emitted beside the usage lifecycle event rather than through it, so the
// foreground-idle / background-complete ordering a turn already established is
// not reordered by an observation.
func claudeRateLimitEvent(sessionID string, window *streams.RateLimitWindow) *AgentEvent {
	if window == nil {
		return nil
	}
	return &AgentEvent{
		Type:            streams.EventTypeRateLimit,
		SessionID:       sessionID,
		RateLimitWindow: window,
	}
}
