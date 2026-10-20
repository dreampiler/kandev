package orchestrator

// A committed transition into a step whose WIP limit is reached leaves the
// task queued at that step; its on_enter actions run on queue promotion.
const (
	wipDeferredOnEnterLogMessage = "workflow on_enter deferred until destination step admits the task"
	wipDeferredOnEnterReason     = "wip_limit_reached"
)
