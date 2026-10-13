package orchestrator

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/review"
	"github.com/kandev/kandev/internal/steptelemetry"
	taskmodels "github.com/kandev/kandev/internal/task/models"
	"github.com/kandev/kandev/internal/workflow/engine"
	wfmodels "github.com/kandev/kandev/internal/workflow/models"
)

// ReviewRunner launches native code-review passes. Satisfied by
// *review.Runner; declared here so the orchestrator does not depend on the
// concrete type and tests can substitute a fake.
type ReviewRunner interface {
	Launch(ctx context.Context, req review.RunRequest) (*taskmodels.TaskReviewRun, error)
}

// buildWorkflowCallbacks creates the callback registry for the workflow engine.
// Each callback wraps an existing orchestrator Service method, keeping side-effect
// logic in the orchestrator while letting the engine drive evaluation.
//
// Phase 2 (ADR-0004) callbacks — queue_run, clear_decisions,
// queue_run_for_each_participant — are registered conditionally based on
// the orchestrator's wired adapters. If adapters are missing, triggers
// without an operation id retain the legacy no-op behavior. Operation-bearing
// triggers fail before execution so late wiring can retry them safely.
func buildWorkflowCallbacks(svc *Service) engine.MapRegistry {
	r := engine.MapRegistry{
		engine.ActionEnablePlanMode:    &enablePlanModeCallback{svc: svc},
		engine.ActionDisablePlanMode:   &disablePlanModeCallback{svc: svc},
		engine.ActionResetAgentContext: &resetAgentContextCallback{svc: svc},
		engine.ActionAutoStartAgent:    &autoStartAgentCallback{svc: svc},
		engine.ActionSetWorkflowData:   &setWorkflowDataCallback{},
		engine.ActionSetSessionMode:    &setSessionModeCallback{svc: svc},
	}
	if svc.engineRunQueue != nil {
		r[engine.ActionQueueRun] = engine.QueueRunCallback{
			Adapter:      svc.engineRunQueue,
			Participants: svc.engineParticipants,
			CEOResolver:  svc.engineCEOResolver,
			Primary:      svc.enginePrimary,
			TaskSteps:    workflowTargetStepResolver(svc.enginePrimary, svc.engineParticipants),
			Logger:       svc.logger,
		}
		if svc.engineParticipants != nil {
			r[engine.ActionQueueRunForEachParticipant] = engine.QueueRunForEachParticipantCallback{
				Adapter:      svc.engineRunQueue,
				Participants: svc.engineParticipants,
				Decisions:    svc.engineDecisions,
				Logger:       svc.logger,
			}
		}
	}
	if svc.engineDecisions != nil {
		r[engine.ActionClearDecisions] = engine.ClearDecisionsCallback{Decisions: svc.engineDecisions}
	}
	if svc.reviewRunner != nil {
		r[engine.ActionRunCodeReview] = &runCodeReviewCallback{svc: svc}
	}
	if svc.engineTaskCreator != nil {
		r[engine.ActionCreateChildTask] = engine.CreateChildTaskCallback{Creator: svc.engineTaskCreator}
	}
	if svc.engineWorkflowSwitcher != nil {
		r[engine.ActionSwitchWorkflow] = engine.SwitchWorkflowCallback{
			Switcher: svc.engineWorkflowSwitcher,
			Dispatch: switchWorkflowDispatcher(svc),
		}
	}
	if svc.engineParticipantSeatWriter != nil {
		r[engine.ActionEnsureParticipantSeat] = engine.EnsureParticipantSeatCallback{
			Writer: svc.engineParticipantSeatWriter,
			Caster: svc.engineParticipantSeatCaster,
			Logger: svc.logger,
		}
	}
	return r
}

// workflowTargetStepResolver picks the primary adapter first because it is the
// queue_run primary resolver, then falls back to participants. Nil means
// cross-task queue_run targets will surface ErrActionNotYetWired.
func workflowTargetStepResolver(
	primary engine.PrimaryAgentResolver,
	participants engine.ParticipantStore,
) engine.TargetTaskStepResolver {
	if resolver, ok := primary.(engine.TargetTaskStepResolver); ok {
		return resolver
	}
	if resolver, ok := participants.(engine.TargetTaskStepResolver); ok {
		return resolver
	}
	return nil
}

// switchWorkflowDispatcher returns the closure SwitchWorkflowCallback uses
// to fire on_exit / on_enter. It reads svc.workflowEngine lazily — at
// registration time the engine may not yet be initialised, but it is
// guaranteed by the time the closure runs (callbacks only execute after
// HandleTrigger).
func switchWorkflowDispatcher(svc *Service) engine.DispatchTriggerFn {
	return func(ctx context.Context, taskID, sessionID string, trigger engine.Trigger, operationID, sourceStepID string) error {
		eng := svc.workflowEngine
		if eng == nil {
			return nil // engine not initialised; treat as no-op
		}
		// Direct on_enter preflight can create/promote sessions and retire the
		// initiating session, so it must not run for a replayed operation. The
		// engine performs the same check before executing actions, but that is
		// necessarily after this dispatcher-side preparation.
		if operationID != "" && svc.workflowStore != nil {
			applied, err := svc.workflowStore.IsOperationApplied(ctx, operationID)
			if err != nil {
				return err
			}
			if applied {
				return nil
			}
		}
		ctx = withWorkflowMetaCache(ctx)
		var preloadedState *engine.MachineState
		if trigger == engine.TriggerOnEnter {
			var err error
			var sourceStep *wfmodels.WorkflowStep
			if sourceStepID != "" {
				sourceStep, err = svc.loadWorkflowStepForLifecycle(ctx, sourceStepID, "workflow switch source")
				if err != nil {
					return err
				}
			}
			sessionID, preloadedState, err = svc.prepareDirectWorkflowStepEntry(ctx, taskID, sessionID, sourceStep)
			if err != nil {
				return err
			}
		}
		input := engine.HandleInput{
			TaskID:         taskID,
			SessionID:      sessionID,
			Trigger:        trigger,
			OperationID:    operationID,
			PreloadedState: preloadedState,
		}
		// on_enter: the ledger-driven DispatchStepEntry path (fired by the
		// step-transition writer this switch's AddTaskToWorkflow call
		// commits through) now owns the session-independent half of the
		// entry sequence. Running the full HandleTrigger here would execute
		// those actions a second time. Session-shaped actions have no other
		// production path on this route, so they still run through the
		// engine's callback registry.
		// on_exit is unaffected — it is a different trigger, entirely out of
		// this requirement's scope.
		var err error
		if trigger == engine.TriggerOnEnter {
			_, err = eng.HandleTriggerSessionShapedOnly(ctx, input)
		} else {
			_, err = eng.HandleTrigger(ctx, input)
		}
		return err
	}
}

func (s *Service) prepareDirectWorkflowStepEntry(
	ctx context.Context, taskID, sessionID string,
	sourceStep *wfmodels.WorkflowStep,
) (string, *engine.MachineState, error) {
	ctx = withWorkflowMetaCache(ctx)
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return "", nil, fmt.Errorf("load task for workflow step entry: %w", err)
	}
	if task == nil {
		return "", nil, fmt.Errorf("task %s not found for workflow step entry", taskID)
	}
	if s.workflowStepGetter == nil || task.WorkflowStepID == "" {
		return "", nil, fmt.Errorf("workflow step is not available for task %s", taskID)
	}
	step, err := s.workflowStepGetter.GetStep(ctx, task.WorkflowStepID)
	if err != nil {
		return "", nil, fmt.Errorf("load destination workflow step: %w", err)
	}
	if step == nil {
		return "", nil, fmt.Errorf("destination workflow step %s not found", task.WorkflowStepID)
	}
	session, err := s.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return "", nil, fmt.Errorf("load session for workflow step entry: %w", err)
	}
	effectiveSession, _, err := s.prepareWorkflowStepSession(ctx, taskID, session, step, sourceStep)
	if err != nil {
		return "", nil, fmt.Errorf("prepare session for workflow step entry: %w", err)
	}
	state := s.buildMachineState(ctx, task, effectiveSession)
	return effectiveSession.ID, &state, nil
}

// enablePlanModeCallback enables plan mode on the session.
type enablePlanModeCallback struct {
	svc *Service
}

func (c *enablePlanModeCallback) Execute(ctx context.Context, in engine.ActionInput) (engine.ActionResult, error) {
	if in.State.IsPassthrough {
		return engine.ActionResult{}, nil
	}
	session, err := c.svc.repo.GetTaskSession(ctx, in.State.SessionID)
	if err != nil {
		return engine.ActionResult{}, fmt.Errorf("load session for enable plan mode: %w", err)
	}
	c.svc.setSessionPlanMode(ctx, session, true)
	return engine.ActionResult{}, nil
}

// disablePlanModeCallback disables plan mode on the session.
type disablePlanModeCallback struct {
	svc *Service
}

func (c *disablePlanModeCallback) Execute(ctx context.Context, in engine.ActionInput) (engine.ActionResult, error) {
	if in.State.IsPassthrough {
		return engine.ActionResult{}, nil
	}
	session, err := c.svc.repo.GetTaskSession(ctx, in.State.SessionID)
	if err != nil {
		return engine.ActionResult{}, fmt.Errorf("load session for disable plan mode: %w", err)
	}
	c.svc.clearSessionPlanMode(ctx, session)
	return engine.ActionResult{}, nil
}

// setSessionModeCallback applies a workflow-declared session permission mode
// (e.g. "acceptEdits") when entering a step. See issue #1183.
type setSessionModeCallback struct {
	svc *Service
}

func (c *setSessionModeCallback) Execute(ctx context.Context, in engine.ActionInput) (engine.ActionResult, error) {
	// Skip before any DB lookup: passthrough sessions manage their own mode in
	// the CLI, and an action with no mode is a no-op. Guarding here keeps a
	// skipped action from failing on a session-load error, and mirrors the
	// enable/disable plan-mode callbacks.
	if in.Action.SetSessionMode == nil || in.State.IsPassthrough {
		return engine.ActionResult{}, nil
	}
	session, err := c.svc.repo.GetTaskSession(ctx, in.State.SessionID)
	if err != nil {
		return engine.ActionResult{}, fmt.Errorf("load session for set session mode: %w", err)
	}
	// Passthrough is already excluded above, so pass false explicitly; the
	// isPassthrough parameter exists for the legacy processOnEnter call site.
	c.svc.applyStepSessionMode(ctx, session, in.Action.SetSessionMode.Mode, false)
	return engine.ActionResult{}, nil
}

// resetAgentContextCallback restarts the agent subprocess with a fresh ACP session.
type resetAgentContextCallback struct {
	svc *Service
}

func (c *resetAgentContextCallback) Execute(ctx context.Context, in engine.ActionInput) (engine.ActionResult, error) {
	session, err := c.svc.repo.GetTaskSession(ctx, in.State.SessionID)
	if err != nil {
		return engine.ActionResult{}, fmt.Errorf("load session for reset agent context: %w", err)
	}
	ok := c.svc.resetAgentContext(ctx, in.State.TaskID, session, in.Step.Name)
	if !ok {
		return engine.ActionResult{}, fmt.Errorf("failed to reset agent context for session %s", in.State.SessionID)
	}
	return engine.ActionResult{}, nil
}

// autoStartAgentCallback sends the auto-start prompt for a workflow step.
type autoStartAgentCallback struct {
	svc *Service
}

func (c *autoStartAgentCallback) Execute(ctx context.Context, in engine.ActionInput) (engine.ActionResult, error) {
	if in.State.IsPassthrough {
		return engine.ActionResult{}, nil
	}

	_, err := c.svc.LaunchSession(ctx, &LaunchSessionRequest{
		TaskID:         in.State.TaskID,
		Intent:         IntentWorkflowStep,
		SessionID:      in.State.SessionID,
		WorkflowStepID: in.Step.ID,
	})
	if err != nil {
		return engine.ActionResult{}, fmt.Errorf("auto-start via LaunchSession failed: %w", err)
	}
	return engine.ActionResult{}, nil
}

// runCodeReviewCallback starts a native code-review pass over the task's changed
// files when it enters the step.
//
// A review failure never blocks the transition: the run row records the reason
// and the Review panel surfaces it, but a task must not get stuck in a step
// because no reviewer was configured or the provider was down. That is why this
// logs and returns nil instead of propagating the error.
type runCodeReviewCallback struct {
	svc *Service
}

func (c *runCodeReviewCallback) Execute(ctx context.Context, in engine.ActionInput) (engine.ActionResult, error) {
	profileID := ""
	if in.Action.RunCodeReview != nil {
		profileID = in.Action.RunCodeReview.AgentProfileID
	}
	sessionID := in.State.SessionID
	writer := c.svc.reviewWriterSession(ctx, in.State.TaskID, sessionID)
	if writer != nil {
		sessionID = writer.ID
	}
	_, err := c.svc.reviewRunner.Launch(ctx, review.RunRequest{
		TaskID:         in.State.TaskID,
		SessionID:      sessionID,
		AgentProfileID: profileID,
		Trigger:        taskmodels.ReviewTriggerWorkflowStep,
		WorkflowStepID: in.Step.ID,
		EntryID:        in.EntryID,
		Origin:         c.svc.reviewOriginIdentity(writer),
		OriginRequired: true,
	})
	if err != nil {
		c.svc.logger.Warn("workflow step code review did not start",
			zap.String("task_id", in.State.TaskID),
			zap.String("workflow_step_id", in.Step.ID),
			zap.Error(err))
	}
	return engine.ActionResult{}, nil
}

// reviewOriginIdentity reads the writer session's concrete execution identity
// so the reviewer is never the writer's own profile or model. A nil result means
// the writer could not be identified; because the caller marks OriginRequired,
// resolution then fails closed (review_agent_unavailable) instead of reviewing
// with a model it cannot prove distinct. Step entry itself is unaffected.
//
// run_code_review is ledger-owned: it runs from Engine.DispatchStepEntry, whose
// MachineState carries no session, so stateSessionID is empty on every real step
// entry. The writer is therefore taken from the committed arrival itself — see
// reviewWriterSession.
func (s *Service) reviewOriginIdentity(session *taskmodels.TaskSession) *review.OriginIdentity {
	if session == nil {
		return nil
	}
	if session.AgentProfileID == "" && session.ExecutionProfileID == "" {
		s.logger.Warn("review origin identity unavailable: writer session has no profile",
			zap.String("task_id", session.TaskID), zap.String("session_id", session.ID))
		return nil
	}
	// A dynamic profile session stores its logical profile as AgentProfileID and
	// the concrete candidate it actually ran as ExecutionProfileID; both are
	// carried so the resolver excludes the logical profile and the real model.
	return &review.OriginIdentity{
		ProfileID:          session.AgentProfileID,
		ExecutionProfileID: session.ExecutionProfileID,
	}
}

// reviewWriterSession identifies the session that wrote the change a step-entry
// review judges, in order:
//
//  1. the session the engine state names, when a caller supplies one;
//  2. the session that initiated the committed step change — the same
//     attribution the task_step_transitions ledger row for this entry records
//     (the session whose turn completed, or the task's own agent moving
//     itself). A session of another task (a coordinator moving the card) did
//     not write this task's change and is passed over;
//  3. the task's primary session, for an arrival no session of the task
//     initiated (a manual move, a WIP pull, a coordinator's move).
//
// A named session that cannot be read stops the search and returns nil: the
// writer is then unknown and the review fails closed rather than guessing.
func (s *Service) reviewWriterSession(ctx context.Context, taskID, stateSessionID string) *taskmodels.TaskSession {
	if s.repo == nil || taskID == "" {
		return nil
	}
	for _, sessionID := range []string{stateSessionID, steptelemetry.FromContext(ctx).SessionID} {
		if sessionID == "" {
			continue
		}
		session, err := s.repo.GetTaskSession(ctx, sessionID)
		if err != nil || session == nil {
			s.logger.Warn("review origin identity unavailable",
				zap.String("task_id", taskID), zap.String("session_id", sessionID), zap.Error(err))
			return nil
		}
		if session.TaskID == taskID {
			return session
		}
	}
	sessions, err := s.repo.ListTaskSessions(ctx, taskID)
	if err != nil {
		s.logger.Warn("review origin identity unavailable: list task sessions",
			zap.String("task_id", taskID), zap.Error(err))
		return nil
	}
	for _, session := range sessions {
		if session != nil && session.IsPrimary {
			return session
		}
	}
	s.logger.Warn("review origin identity unavailable: no session identifies the writer",
		zap.String("task_id", taskID))
	return nil
}

// setWorkflowDataCallback writes key/value data into the workflow data bag.
type setWorkflowDataCallback struct{}

func (c *setWorkflowDataCallback) Execute(_ context.Context, in engine.ActionInput) (engine.ActionResult, error) {
	if in.Action.SetWorkflowData == nil {
		return engine.ActionResult{}, nil
	}
	return engine.ActionResult{
		DataPatch: map[string]any{
			in.Action.SetWorkflowData.Key: in.Action.SetWorkflowData.Value,
		},
	}, nil
}
