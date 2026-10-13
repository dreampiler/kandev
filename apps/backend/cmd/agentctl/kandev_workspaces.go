package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Cross-workspace access (docs/specs/office/requirements/
// office-cross-workspace-access.md). Every command here calls a signed Office
// runtime route that decides reach from the run token's workspace and that
// workspace's cross-workspace access setting; with the setting off, only the
// run's own workspace is reachable and everything else answers 403/404.

// subcmdTranscript names the cross-workspace conversation read.
const subcmdTranscript = "transcript"

// runWorkspacesCmd dispatches `agentctl kandev workspaces <subcmd>`.
func runWorkspacesCmd(args []string) int {
	if len(args) == 0 {
		cliError("usage: agentctl kandev workspaces <list|coordinator|tasks> [flags]")
		return 1
	}
	switch args[0] {
	case subcmdList:
		return workspacesList(args[1:])
	case "coordinator":
		return workspacesCoordinator(args[1:])
	case "tasks":
		return workspacesTasks(args[1:])
	default:
		cliError("unknown workspaces subcommand: %s", args[0])
		return 1
	}
}

// workspacesList prints the run's own workspace plus every workspace it can
// reach, each with the coordinator task to address there.
func workspacesList(args []string) int {
	fs := flag.NewFlagSet("workspaces list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		cliError("parse flags: %v", err)
		return 1
	}
	return getWithQuery("/api/v1/office/runtime/workspaces", url.Values{})
}

func workspacesCoordinator(args []string) int {
	fs := flag.NewFlagSet("workspaces coordinator", flag.ContinueOnError)
	id := fs.String("id", "", "Workspace ID (required)")
	if err := fs.Parse(args); err != nil {
		cliError("parse flags: %v", err)
		return 1
	}
	wsID := strings.TrimSpace(*id)
	if wsID == "" {
		cliError("--id is required")
		return 1
	}
	return getWithQuery("/api/v1/office/runtime/workspaces/"+url.PathEscape(wsID)+"/coordinator", url.Values{})
}

func workspacesTasks(args []string) int {
	fs := flag.NewFlagSet("workspaces tasks", flag.ContinueOnError)
	id := fs.String("id", "", "Workspace ID (required)")
	workflow := fs.String("workflow", "", "Limit to one workflow ID")
	if err := fs.Parse(args); err != nil {
		cliError("parse flags: %v", err)
		return 1
	}
	return listWorkspaceTasks(*id, *workflow)
}

func listWorkspaceTasks(workspaceID, workflowID string) int {
	wsID := strings.TrimSpace(workspaceID)
	if wsID == "" {
		cliError("--id is required")
		return 1
	}
	values := url.Values{}
	setQueryIfNonEmpty(values, "workflow_id", strings.TrimSpace(workflowID))
	return getWithQuery("/api/v1/office/runtime/workspaces/"+url.PathEscape(wsID)+"/tasks", values)
}

// tasksCrossRead handles `tasks sessions|transcript|plan`: reads of any task
// the run can reach, including tasks in other workspaces.
func tasksCrossRead(name, suffix string, args []string) int {
	fs := flag.NewFlagSet("tasks "+name, flag.ContinueOnError)
	id := fs.String("id", "", "Task ID (required; defaults to $KANDEV_TASK_ID)")
	var session, limit, before, after, sortOrder *string
	if name == subcmdTranscript {
		session = fs.String("session", "", "Session ID (defaults to the primary session)")
		limit = fs.String("limit", "", "Page size")
		before = fs.String("before", "", "Return messages before this message ID")
		after = fs.String("after", "", "Return messages after this message ID")
		sortOrder = fs.String("sort", "", "asc | desc")
	}
	if err := fs.Parse(args); err != nil {
		cliError("parse flags: %v", err)
		return 1
	}
	client, err := newKandevClient()
	if err != nil {
		cliError("%v", err)
		return 1
	}
	taskID := strings.TrimSpace(resolveTaskID(*id, client.taskID))
	if taskID == "" {
		cliError("--id (or KANDEV_TASK_ID) required")
		return 1
	}
	values := url.Values{}
	if name == subcmdTranscript {
		setQueryIfNonEmpty(values, "session_id", *session)
		setQueryIfNonEmpty(values, "limit", *limit)
		setQueryIfNonEmpty(values, "before", *before)
		setQueryIfNonEmpty(values, "after", *after)
		setQueryIfNonEmpty(values, "sort", *sortOrder)
	}
	q := ""
	if encoded := values.Encode(); encoded != "" {
		q = "?" + encoded
	}
	body, status, err := client.do(http.MethodGet,
		fmt.Sprintf("/api/v1/office/runtime/tasks/%s/%s%s", url.PathEscape(taskID), suffix, q), nil)
	return handleResponse(body, status, err)
}

// tasksSend sends a message to a task's agent session, in this workspace or
// a reachable one, as the run's own task. Use it to ask another workspace's
// coordinator for something; the reply comes back as a comment on this task.
func tasksSend(args []string) int {
	fs := flag.NewFlagSet("tasks send", flag.ContinueOnError)
	id := fs.String("id", "", "Target task ID (required)")
	prompt := fs.String("prompt", "", "Message (required; use \"-\" for stdin)")
	operationID := fs.String("operation-id", "", "Optional idempotency key; reuse it when retrying the same send")
	if err := fs.Parse(args); err != nil {
		cliError("parse flags: %v", err)
		return 1
	}
	taskID := strings.TrimSpace(*id)
	message, err := resolveBody(*prompt)
	if err != nil {
		cliError("read prompt: %v", err)
		return 1
	}
	if taskID == "" || strings.TrimSpace(message) == "" {
		cliError("--id and --prompt are required")
		return 1
	}
	client, err := newKandevClient()
	if err != nil {
		cliError("%v", err)
		return 1
	}
	payload := map[string]string{"prompt": message}
	if op := strings.TrimSpace(*operationID); op != "" {
		payload["operation_id"] = op
	}
	body, status, err := client.do(http.MethodPost,
		fmt.Sprintf("/api/v1/office/runtime/tasks/%s/messages", url.PathEscape(taskID)), payload)
	return handleResponse(body, status, err)
}
