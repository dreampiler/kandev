package crossworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/events/bus"
	"github.com/kandev/kandev/internal/office/models"
	taskmodels "github.com/kandev/kandev/internal/task/models"
	v1 "github.com/kandev/kandev/pkg/api/v1"
)

// CommentSource marks an Office comment that arrived from another task
// through this package, so UIs and prompts can tell it from a person's
// comment.
const CommentSource = "cross_workspace"

// handoff_source metadata keys written by the Office handoff action.
const (
	handoffSourceTaskIDKey      = "source_task_id"
	handoffSourceWorkspaceIDKey = "source_workspace_id"
)

// OfficeDelivery identifies the comment a message or report became.
type OfficeDelivery struct {
	TaskID    string `json:"task_id"`
	CommentID string `json:"comment_id"`
}

// DeliverToOfficeTask turns a task-to-task message whose target is an Office
// task into a comment on that task, which wakes its assignee through the
// normal task_comment run. It applies only when the target's Office
// workspace has cross-workspace access on and can reach the sender's
// workspace. handled=false means the caller keeps its normal delivery path,
// which is the behavior with the setting off.
func (s *Service) DeliverToOfficeTask(
	ctx context.Context, sender, target *taskmodels.Task, body string,
) (delivery *OfficeDelivery, handled bool, err error) {
	if s == nil || sender == nil || target == nil || !target.IsFromOffice {
		return nil, false, nil
	}
	if !s.Enabled(ctx, target.WorkspaceID) {
		return nil, false, nil
	}
	if err := s.Reachable(ctx, target.WorkspaceID, sender.WorkspaceID); err != nil {
		if errors.Is(err, ErrDisabled) || errors.Is(err, ErrNotReachable) {
			return nil, false, nil
		}
		return nil, false, err
	}
	header := fmt.Sprintf("Message from task %q (%s)%s.\n%s",
		sender.Title, sender.ID, s.workspaceSuffix(ctx, sender.WorkspaceID), replyHint(sender.ID))
	delivery, err = s.postComment(ctx, target.ID, header+"\n\n"+body)
	if err != nil {
		return nil, false, err
	}
	return delivery, true, nil
}

// ReportToHandoffSource posts body on the Office task that handed off
// deliveryTaskID, waking that task's assignee. callerWorkspaceID, when set,
// must be the delivery task's workspace: a coordinator reports on its own
// workspace's work only.
func (s *Service) ReportToHandoffSource(
	ctx context.Context, callerWorkspaceID, deliveryTaskID, body string,
) (*OfficeDelivery, error) {
	if strings.TrimSpace(body) == "" {
		return nil, newValidationError("body is required")
	}
	delivery, err := s.tasks.GetTask(ctx, deliveryTaskID)
	if err != nil || delivery == nil {
		return nil, newValidationError("task %q was not found", deliveryTaskID)
	}
	if callerWorkspaceID != "" && delivery.WorkspaceID != callerWorkspaceID {
		return nil, ErrNotReachable
	}
	sourceTask, err := s.handoffSourceTask(ctx, delivery)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("Report on handed-off task %q (%s)%s.\n%s",
		delivery.Title, delivery.ID, s.workspaceSuffix(ctx, delivery.WorkspaceID), replyHint(delivery.ID))
	return s.postComment(ctx, sourceTask.ID, header+"\n\n"+body)
}

// handoffSourceTask resolves the Office task recorded in delivery's
// handoff_source provenance and checks that its workspace still has
// cross-workspace access on and reaches the delivery task's workspace.
func (s *Service) handoffSourceTask(ctx context.Context, delivery *taskmodels.Task) (*taskmodels.Task, error) {
	sourceTaskID, sourceWorkspaceID := handoffSource(delivery)
	if sourceTaskID == "" || sourceWorkspaceID == "" {
		return nil, newValidationError("task %q has no handoff source", delivery.ID)
	}
	ownerCtx, err := s.reach(ctx, sourceWorkspaceID, delivery.WorkspaceID)
	if err != nil {
		return nil, err
	}
	source, err := s.tasks.GetTask(ownerCtx, sourceTaskID)
	if err != nil || source == nil || source.WorkspaceID != sourceWorkspaceID || !source.IsFromOffice {
		return nil, newValidationError("handoff source task %q is no longer available", sourceTaskID)
	}
	return source, nil
}

func handoffSource(task *taskmodels.Task) (taskID, workspaceID string) {
	if task == nil || task.Metadata == nil {
		return "", ""
	}
	src, ok := task.Metadata[taskmodels.MetaKeyHandoffSource].(map[string]interface{})
	if !ok {
		return "", ""
	}
	taskID, _ = src[handoffSourceTaskIDKey].(string)
	workspaceID, _ = src[handoffSourceWorkspaceIDKey].(string)
	return strings.TrimSpace(taskID), strings.TrimSpace(workspaceID)
}

// SubscribeHandoffCompletion reports a handed-off task's terminal state back
// to the Office task that created it. It only acts when that Office
// workspace has cross-workspace access on.
func (s *Service) SubscribeHandoffCompletion(eb bus.EventBus) error {
	if s == nil || eb == nil {
		return nil
	}
	_, err := eb.Subscribe(events.TaskStateChanged, func(_ context.Context, event *bus.Event) error {
		taskID, newState := taskStateChange(event)
		if taskID == "" {
			return nil
		}
		// The memory bus runs handlers inside Publish; reporting must not
		// hold up the state change that triggered it.
		go s.HandleTaskStateChanged(context.Background(), taskID, newState)
		return nil
	})
	return err
}

func taskStateChange(event *bus.Event) (taskID string, newState v1.TaskState) {
	if event == nil {
		return "", ""
	}
	data, ok := event.Data.(map[string]interface{})
	if !ok {
		return "", ""
	}
	taskID, _ = data["task_id"].(string)
	state, _ := data["new_state"].(string)
	return taskID, v1.TaskState(state)
}

// HandleTaskStateChanged posts the completion report for one state change.
// Errors are logged, never returned: a missed report leaves the Office task
// unchanged and the coordinator can still report by hand.
func (s *Service) HandleTaskStateChanged(ctx context.Context, taskID string, newState v1.TaskState) {
	if s == nil || taskID == "" || !taskmodels.IsTerminalTaskState(newState) {
		return
	}
	task, err := s.tasks.GetTask(ctx, taskID)
	if err != nil || task == nil {
		return
	}
	sourceTaskID, sourceWorkspaceID := handoffSource(task)
	if sourceTaskID == "" || !s.Enabled(ctx, sourceWorkspaceID) {
		return
	}
	source, err := s.handoffSourceTask(ctx, task)
	if err != nil {
		s.logger.Debug("handoff completion report skipped",
			zap.String("task_id", taskID), zap.Error(err))
		return
	}
	body := fmt.Sprintf("Handed-off task %q (%s)%s reached state %s.\n"+
		"Read its result with `$KANDEV_CLI kandev tasks transcript --id %s`, or ask its workspace coordinator.",
		task.Title, task.ID, s.workspaceSuffix(ctx, task.WorkspaceID), newState, task.ID)
	if _, err := s.postComment(ctx, source.ID, body); err != nil {
		s.logger.Warn("handoff completion report failed",
			zap.String("task_id", taskID), zap.String("source_task_id", source.ID), zap.Error(err))
	}
}

func (s *Service) postComment(ctx context.Context, taskID, body string) (*OfficeDelivery, error) {
	if s.comments == nil {
		return nil, errors.New("office comments are not available")
	}
	comment := &models.TaskComment{
		ID:         uuid.New().String(),
		TaskID:     taskID,
		AuthorType: "agent",
		Body:       body,
		Source:     CommentSource,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.comments.CreateComment(ctx, comment); err != nil {
		return nil, fmt.Errorf("create office comment: %w", err)
	}
	return &OfficeDelivery{TaskID: taskID, CommentID: comment.ID}, nil
}

// workspaceSuffix renders ` in workspace "<name>"` for message headers, or
// "" when the name cannot be read.
func (s *Service) workspaceSuffix(ctx context.Context, workspaceID string) string {
	ws, err := s.tasks.GetWorkspace(ctx, workspaceID)
	if err != nil || ws == nil {
		return ""
	}
	return fmt.Sprintf(" in workspace %q", ws.Name)
}

func replyHint(taskID string) string {
	return fmt.Sprintf("Reply with `$KANDEV_CLI kandev tasks send --id %s --prompt ...` "+
		"or the message_task_kandev MCP tool (task_id=%s).", taskID, taskID)
}
