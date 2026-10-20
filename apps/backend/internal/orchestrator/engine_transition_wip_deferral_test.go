package orchestrator

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// prepareExecuteWorkflowJSON mirrors the production shape where an agent's
// completion signal advances "Prepare" into a WIP-limited "Execute" step whose
// on_enter starts the agent.
const prepareExecuteWorkflowJSON = `{
  "version": 1,
  "type": "kandev_workflow",
  "workflows": [
    {
      "name": "PrepareExecute",
      "steps": [
        {
          "name": "Prepare",
          "position": 0,
          "color": "bg-blue-500",
          "events": {
            "on_turn_complete": [{"type": "move_to_step", "config": {"step_position": 1}}]
          },
          "is_start_step": true,
          "allow_manual_move": true
        },
        {
          "name": "Execute",
          "position": 1,
          "color": "bg-yellow-500",
          "prompt": "execute the plan",
          "events": {
            "on_enter": [{"type": "auto_start_agent"}]
          },
          "allow_manual_move": true
        }
      ]
    }
  ]
}`

func observeServiceLogs(t *testing.T, svc *Service, level zapcore.Level) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(level)
	log, err := logger.NewFromZap(zap.New(core))
	if err != nil {
		t.Fatalf("build observed logger: %v", err)
	}
	svc.logger = log
	return logs
}

// An engine transition into a destination step whose WIP limit is already
// reached commits the move but defers the destination's on_enter until queue
// promotion. That deferral must be observable: it names the task, the step and
// the reason, so an operator can tell a queued arrival from a lost on_enter.
func TestEngineTransition_WIPFullDestination_LogsOnEnterDeferral(t *testing.T) {
	ctx := context.Background()
	sg, nameToID := buildWorkflowFromJSON(t, prepareExecuteWorkflowJSON)
	executeID := nameToID["Execute"]
	sg.steps[executeID].WIPLimit = 1

	repo := setupTestRepo(t)
	seedSession(t, repo, "t1", "s1", nameToID["Prepare"])
	setSessionExecID(t, repo, "s1", "exec-1")
	if err := repo.CreateTask(ctx, &models.Task{
		ID: "occupant", WorkspaceID: "ws1", WorkflowID: "wf1", WorkflowStepID: executeID,
		Title: "Occupant", State: v1.TaskStateInProgress, Priority: "medium",
	}); err != nil {
		t.Fatalf("create occupant: %v", err)
	}

	agentMgr := &mockAgentManager{repoForExecutionLookup: repo, isAgentRunning: true}
	svc := createEngineService(t, repo, sg, agentMgr)
	logs := observeServiceLogs(t, svc, zapcore.InfoLevel)
	enteredOnEnter := false
	svc.onProcessOnEnterComplete = func() { enteredOnEnter = true }

	setSessionState(t, ctx, repo, "s1", models.TaskSessionStateRunning)
	session, err := repo.GetTaskSession(ctx, "s1")
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if !svc.processOnTurnCompleteViaEngine(ctx, "t1", session) {
		t.Fatal("expected the Prepare -> Execute transition to commit")
	}

	task, err := repo.GetTask(ctx, "t1")
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if task.WorkflowStepID != executeID || task.WIPAdmitted || task.QueuedForStepID != executeID {
		t.Fatalf("placement step=%q admitted=%t queued=%q, want queued at Execute",
			task.WorkflowStepID, task.WIPAdmitted, task.QueuedForStepID)
	}
	if enteredOnEnter {
		t.Fatal("on_enter must wait for queue promotion")
	}

	var deferrals []map[string]interface{}
	for _, e := range logs.All() {
		if e.Message == wipDeferredOnEnterLogMessage {
			deferrals = append(deferrals, e.ContextMap())
		}
	}
	if len(deferrals) != 1 {
		t.Fatalf("on_enter deferral log count = %d, want 1 (entries: %+v)", len(deferrals), logs.All())
	}
	fields := deferrals[0]
	if fields["task_id"] != "t1" || fields["session_id"] != "s1" || fields["to_step_id"] != executeID {
		t.Fatalf("deferral log identity = %+v", fields)
	}
	if fields["reason"] != wipDeferredOnEnterReason {
		t.Fatalf("deferral reason = %v, want %q", fields["reason"], wipDeferredOnEnterReason)
	}
}
