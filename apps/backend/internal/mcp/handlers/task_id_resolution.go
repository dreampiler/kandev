package handlers

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"

	mcpscope "github.com/kandev/kandev/internal/mcp/scope"
	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// shortTaskIDPattern matches a hex prefix of a UUID (8 to 35 characters). A
// full 36-character UUID and any non-hex value are left untouched, so existing
// callers that pass full ids keep their exact behavior.
var shortTaskIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8,35}$`)

// taskRefScalarFields are the payload keys that name a task by id and that a
// short-id expansion may rewrite. They are shared across the MCP control
// surface (archive, move, plan read/write, related-task lookup).
var taskRefScalarFields = []string{
	"task_id",
	"caller_task_id",
	"source_task_id",
	"parent_id",
	"depends_on_task_id",
}

// maxTaskIDCandidates bounds the candidate list returned for an ambiguous
// prefix so a rejection stays small.
const maxTaskIDCandidates = 20

// expandShortTaskRefs rewrites any short task-id prefix in a known task-ref
// field to the full id, resolving it against the caller's own workspace. It
// runs before the authorization guards so those guards see the resolved id.
//
// A value that is not an 8-35 character hex prefix is returned unchanged. When
// a prefix matches more than one task in the workspace the request is rejected
// with the candidate list rather than guessing, so a controller learns the full
// ids without the tool silently acting on the wrong task. When no task matches,
// the original value is left in place and the target tool keeps its own
// not-found behavior. Returns (msg, nil) for a pass-through or rewritten
// message, or (nil, errorMsg) to reject the request.
func (h *Handlers) expandShortTaskRefs(ctx context.Context, msg *ws.Message) (*ws.Message, *ws.Message) {
	if h.taskSvc == nil || len(msg.Payload) == 0 {
		return msg, nil
	}
	principal, ok := mcpscope.PrincipalFromContext(ctx)
	if !ok || principal.WorkspaceID == "" {
		return msg, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(msg.Payload, &fields); err != nil || fields == nil {
		return msg, nil
	}

	changed := false
	for _, key := range taskRefScalarFields {
		raw, present := fields[key]
		if !present {
			continue
		}
		resolved, candidates, ok := h.expandShortTaskRefValue(ctx, principal.WorkspaceID, raw)
		if !ok {
			continue
		}
		if candidates != nil {
			errorMsg, _ := shortTaskIDAmbiguous(msg, resolved, candidates)
			return nil, errorMsg
		}
		encoded, err := json.Marshal(resolved)
		if err != nil {
			continue
		}
		fields[key] = encoded
		changed = true
		h.logger.Info("expanded short task id", zap.String("field", key), zap.String("task_id", resolved))
	}
	if !changed {
		return msg, nil
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return msg, nil
	}
	replacement := *msg
	replacement.Payload = payload
	return &replacement, nil
}

// expandShortTaskRefValue resolves one raw field value. It returns
// (fullID, nil, true) when the value is a unique short id, (rawPrefix,
// candidates, true) when it is ambiguous, and ("", nil, false) when the value
// is not a short id, matches no task, or the lookup failed — in which case the
// caller leaves the original value in place.
func (h *Handlers) expandShortTaskRefValue(
	ctx context.Context, workspaceID string, raw json.RawMessage,
) (string, []*models.Task, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", nil, false
	}
	value = strings.TrimSpace(value)
	if !shortTaskIDPattern.MatchString(value) {
		return "", nil, false
	}
	tasks, err := h.taskSvc.FindTasksByIDPrefix(ctx, workspaceID, strings.ToLower(value))
	if err != nil {
		h.logger.Warn("short task id prefix lookup failed",
			zap.String("prefix", value), zap.Error(err))
		return "", nil, false
	}
	switch len(tasks) {
	case 0:
		return "", nil, false
	case 1:
		return tasks[0].ID, nil, true
	default:
		return value, tasks, true
	}
}

// shortTaskIDAmbiguous builds the rejection for a prefix that matches several
// tasks in the caller's workspace, listing the candidates it may have meant.
func shortTaskIDAmbiguous(msg *ws.Message, prefix string, tasks []*models.Task) (*ws.Message, error) {
	candidates := make([]map[string]interface{}, 0, maxTaskIDCandidates)
	for i, task := range tasks {
		if i >= maxTaskIDCandidates {
			break
		}
		candidates = append(candidates, map[string]interface{}{
			"task_id": task.ID,
			"title":   task.Title,
			"state":   string(task.State),
		})
	}
	return ws.NewError(msg.ID, msg.Action, ws.ErrorCodeValidation,
		"task id prefix "+prefix+" matches "+strconv.Itoa(len(tasks))+
			" tasks in this workspace; pass a longer prefix or the full id", map[string]interface{}{
			"reason":     "ambiguous_task_id_prefix",
			"prefix":     prefix,
			"candidates": candidates,
		})
}
