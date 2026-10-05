package statussummary

import (
	"encoding/json"

	dynamicruntime "github.com/kandev/kandev/internal/agent/runtime/dynamic"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// QuotaWaitSummaryFromRouteState maps one session's durable routing state to the
// bounded task-list projection, or nil when the session is not parked on a
// provider limit.
//
// Only a deadline-backed wait qualifies: a route that is waiting without a
// deadline has no instant to lift at, and a route waiting for any reason other
// than an exhausted quota belongs to the routing-owned presentation rather than
// to a limit wait. The policy body, the failure rule and the provider payload
// behind the decision stay in the routing tables.
func QuotaWaitSummaryFromRouteState(state *dynamicruntime.RouteState) *QuotaWaitSummary {
	if state == nil || state.SessionID == "" {
		return nil
	}
	policy, ok := quotaWaitPolicy(state)
	if !ok {
		return nil
	}
	if policy.FailureCode != routingerr.CodeQuotaLimited || policy.Deadline == nil {
		return nil
	}
	deadline := policy.Deadline.UTC()
	if deadline.IsZero() {
		return nil
	}
	return &QuotaWaitSummary{
		SessionID: state.SessionID,
		Reason:    QuotaWaitReasonQuotaLimited,
		Deadline:  deadline,
	}
}

// quotaWaitPolicy reports whether the durable status is one a fresh selection
// is retried from automatically, and returns its policy snapshot. A waiting
// route qualifies only while it waits for suspended resources, because only
// that wait carries the deadline the projection reports.
func quotaWaitPolicy(state *dynamicruntime.RouteState) (dynamicruntime.PolicyState, bool) {
	var policy dynamicruntime.PolicyState
	switch state.Status {
	case "retry_wait", "waiting_for_reset":
	case "waiting":
		if !policyHasResourceWait(state.PolicyStateJSON) {
			return policy, false
		}
	default:
		return policy, false
	}
	if err := json.Unmarshal([]byte(state.PolicyStateJSON), &policy); err != nil {
		return policy, false
	}
	if policy.ResourceWait && policy.Deadline == nil {
		return policy, false
	}
	return policy, true
}

// policyHasResourceWait reads the wait marker without trusting the rest of the
// document, so a policy body this build cannot parse still answers whether the
// route is a resource wait.
func policyHasResourceWait(policyStateJSON string) bool {
	var probe struct {
		ResourceWait bool `json:"resource_wait"`
	}
	if err := json.Unmarshal([]byte(policyStateJSON), &probe); err != nil {
		return false
	}
	return probe.ResourceWait
}
