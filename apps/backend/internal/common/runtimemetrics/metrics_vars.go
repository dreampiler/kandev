// Package runtimemetrics publishes the agent-runtime footprint gauges. It is a
// neutral package so the lifecycle tier that measures a footprint and the
// orchestrator tier that reacts to one record against the same variables
// instead of each keeping a private registry of the same names.
//
// Every published variable is a bare gauge or a counter over a closed reason
// set. No task, session, execution, user, or agent identifier is ever a label;
// per-session values belong on the bounded diagnostic projection, not here.
package runtimemetrics

import "expvar"

var (
	liveRuntimes     = expvar.NewInt("agent_live_runtimes")
	runtimeProcesses = expvar.NewInt("agent_runtime_processes")
	committedBytes   = expvar.NewInt("agent_runtime_committed_bytes")
	residentBytes    = expvar.NewInt("agent_runtime_resident_bytes")
	unreadableRuns   = expvar.NewInt("agent_runtime_unreadable_runtimes")
	footprintReads   = expvar.NewInt("agent_runtime_footprint_reads_total")
	footprintErrors  = expvar.NewInt("agent_runtime_footprint_errors_total")
	reclamationTotal = expvar.NewMap("runtime_reclamation_total")
)

// Footprint describes one observation of the live agent runtimes' cost. The
// byte totals are a lower bound whenever UnreadableRuntimes is nonzero.
type Footprint struct {
	LiveRuntimes       int
	ProcessCount       int
	CommittedBytes     uint64
	ResidentBytes      uint64
	UnreadableRuntimes int
	// Complete is false when the observation failed. A failed observation is
	// recorded as an error and zeroes the gauges rather than leaving a previous
	// total readable as if it were still current.
	Complete bool
}

// PublishFootprint records one footprint observation.
func PublishFootprint(observation Footprint) {
	footprintReads.Add(1)
	if !observation.Complete {
		footprintErrors.Add(1)
		liveRuntimes.Set(0)
		runtimeProcesses.Set(0)
		committedBytes.Set(0)
		residentBytes.Set(0)
		unreadableRuns.Set(0)
		return
	}
	liveRuntimes.Set(int64(observation.LiveRuntimes))
	runtimeProcesses.Set(int64(observation.ProcessCount))
	committedBytes.Set(int64(observation.CommittedBytes))
	residentBytes.Set(int64(observation.ResidentBytes))
	unreadableRuns.Set(int64(observation.UnreadableRuntimes))
}

// Reclamation reasons. The set is closed: a new call site adds a constant here
// rather than passing a free-form string.
const (
	// ReasonTerminal marks a session that can no longer receive a message.
	ReasonTerminal = "terminal"
	// ReasonWorkspacePolicy marks the per-workspace opt-in idle suspension.
	ReasonWorkspacePolicy = "workspace_policy"
	// ReasonDeadRow marks a row whose process is already gone.
	ReasonDeadRow = "dead_row"
)

// Reclamation results.
const (
	// ResultReleased means the runtime was stopped and its row settled.
	ResultReleased = "released"
	// ResultSkipped means a guard failed closed, so nothing was forced.
	ResultSkipped = "skipped"
	// ResultFailed means the stop itself did not complete; ownership is retained
	// and the candidate is retried.
	ResultFailed = "failed"
)

// RecordReclamation counts one reclamation decision.
func RecordReclamation(reason, result string) {
	reclamationTotal.Add(reason+";"+result, 1)
}
