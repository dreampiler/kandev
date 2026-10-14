package handlers

import (
	"context"

	ws "github.com/kandev/kandev/pkg/websocket"
)

// HandlerExtensions groups the optional, product-specific dependencies that
// extend the base Handlers. Keeping them in one value type lets the base
// dependency list stay stable as features are added.
type HandlerExtensions struct {
	stepCompleteRebinder        StepCompleteSessionRebinder
	stepCompleteBindingResolver StepCompleteSessionBindingResolver
	childTaskReorderer          ChildTaskReorderService
	sendOperationStore          SendOperationStore
	sendOperationRetention      func(ctx context.Context)
	officeCrossWorkspace        OfficeCrossWorkspace
}

// SetStepCompleteSessionRebinder wires the orchestrator-owned primary-session
// promotion used to rebind a step-completion signal when the session the task
// currently designates for the step is gone or running the wrong profile.
func (h *Handlers) SetStepCompleteSessionRebinder(rebinder StepCompleteSessionRebinder) {
	h.extensions.stepCompleteRebinder = rebinder
}

// SetStepCompleteSessionBindingResolver wires the orchestrator-owned durable
// step binding the step-completion judgment reads to recognize the session a
// restart or substitution moved onto the current step.
func (h *Handlers) SetStepCompleteSessionBindingResolver(resolver StepCompleteSessionBindingResolver) {
	h.extensions.stepCompleteBindingResolver = resolver
}

// registerAdditionalReadActions registers the read actions that extend the base
// read set.
func (h *Handlers) registerAdditionalReadActions(d mcpActionRegistrar) {
	d.RegisterFunc(ws.ActionMCPGetTaskMessageOperation, h.handleGetTaskMessageOperation)
	d.RegisterFunc(ws.ActionMCPReportToHandoffSource, h.handleReportToHandoffSource)
}

// registerAdditionalMutationActions registers the mutation actions that extend
// the base mutation set.
func (h *Handlers) registerAdditionalMutationActions(d mcpActionRegistrar) {
	d.RegisterFunc(ws.ActionMCPReorderChildTasks, h.handleReorderChildTasks)
}
