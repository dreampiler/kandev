package mcp

import (
	"fmt"
	"reflect"
)

// toolArgumentAliases maps an accepted spelling variant to the canonical
// argument name, per tool.
//
// The compiled tool schemas set additionalProperties=false, so an argument a
// model spelled its own way is rejected before the handler ever runs, with an
// error that names the invented key and not the one it should have sent. That
// rejection costs a full round trip through the agent for what is a rename, and
// the plan-write tools were the worst case: a model that writes old_string or
// oldText for old_text never reaches the edit at all.
//
// Renaming here, ahead of validation, is the only place that can help: the
// schema stays closed (see applyToolArgumentAliases for what it does not
// absorb), the handler sees the canonical name, and the value itself is never
// inspected, so uniqueness, size, and version guards are untouched.
//
// Deliberately absent: any alias that would change a value or drop a required
// argument. "content"/"plan" style aliases are not listed because they invite
// a substitution the tool cannot verify.
var toolArgumentAliases = map[string]map[string]string{
	"edit_task_plan_kandev": {
		"old_string":      "old_text",
		"oldString":       "old_text",
		"oldText":         "old_text",
		"new_string":      "new_text",
		"newString":       "new_text",
		"expectedVersion": "expected_version",
		"allowTruncation": "allow_truncation",
		"taskId":          mcpKeyTaskID,
	},
	"create_task_plan_kandev": {
		"expectedVersion": "expected_version",
		"taskId":          mcpKeyTaskID,
	},
	"update_task_plan_kandev": {
		"expectedVersion": "expected_version",
		"taskId":          mcpKeyTaskID,
	},
	"get_task_plan_kandev": {
		"expectedVersion": "expected_version",
		"taskId":          mcpKeyTaskID,
	},
	"list_workflows_kandev": {
		"workspaceId": "workspace_id",
	},
	"list_workflow_steps_kandev": {
		"workflowId": "workflow_id",
	},
	"list_tasks_kandev": {
		"workflowId": "workflow_id",
	},
	"get_task_conversation_kandev": {
		"taskId":    mcpKeyTaskID,
		"sessionId": "session_id",
	},
	"list_task_sessions_kandev": {
		"taskId":    mcpKeyTaskID,
		"sessionId": "session_id",
	},
	"list_related_tasks_kandev": {
		"taskId": mcpKeyTaskID,
	},
	"message_task_kandev": {
		"taskId":    mcpKeyTaskID,
		"sessionId": "session_id",
	},
}

// applyToolArgumentAliases rewrites the recognized variants of args to their
// canonical names and returns the rewritten copy. A key this table does not
// list is left untouched, so the schema still rejects it and the existing
// unknown-argument diagnostic still names it.
//
// A canonical argument already present alongside an alias is the one case that
// must not silently pick a winner: the two values disagree, so neither can be
// assumed to be the intended one. Equal values collapse quietly; anything else
// is an error naming both keys.
func applyToolArgumentAliases(toolName string, args map[string]any) (map[string]any, error) {
	aliases := toolArgumentAliases[toolName]
	normalized := make(map[string]any, len(args))
	for key, value := range args {
		normalized[key] = value
	}
	for alias, canonical := range aliases {
		value, present := normalized[alias]
		if !present {
			continue
		}
		delete(normalized, alias)
		existing, present := normalized[canonical]
		if !present {
			normalized[canonical] = value
			continue
		}
		if !reflect.DeepEqual(existing, value) {
			return nil, fmt.Errorf(
				"invalid arguments for %s: %q and %q were both provided with different values; send only %q",
				toolName, alias, canonical, canonical)
		}
	}
	return normalized, nil
}
