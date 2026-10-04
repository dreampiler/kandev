package lifecycle

import (
	"strings"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
)

func (e *AgentExecution) observeToolUpdate(event agentctl.AgentEvent) bool {
	if event.ParentToolCallID != "" {
		return true
	}
	e.activeToolMu.Lock()
	defer e.activeToolMu.Unlock()
	tool, exists := e.openTools[event.ToolCallID]
	if !exists {
		if !toolIsExecuting(event.ToolStatus) {
			return isTerminalToolUpdate(event)
		}
		tool = activeTopLevelTool{ToolCallID: event.ToolCallID, Name: event.ToolName, Title: event.ToolTitle}
		if e.openTools == nil {
			e.openTools = make(map[string]activeTopLevelTool)
		}
	}
	progress := event.ToolStatus != "" && event.ToolStatus != tool.Status
	if tool.ForegroundExited && !isTerminalToolUpdate(event) {
		return false
	}
	if event.ToolStatus != "" {
		tool.Status = event.ToolStatus
	}
	outputProgress := updateToolOutput(&tool, event)
	progress = progress || outputProgress
	e.openTools[event.ToolCallID] = tool
	if e.activeTool != nil && e.activeTool.ToolCallID == tool.ToolCallID {
		copy := tool
		e.activeTool = &copy
	}
	if progress {
		e.toolRevision++
	}
	return progress || isTerminalToolUpdate(event)
}

func updateToolOutput(tool *activeTopLevelTool, event agentctl.AgentEvent) bool {
	if event.ToolOutputBytes != nil {
		progress := *event.ToolOutputBytes > tool.OutputBytes
		tool.OutputBytes = max(tool.OutputBytes, *event.ToolOutputBytes)
		return progress
	}
	output := toolUpdateText(event)
	if output == "" || output == tool.OutputText {
		return false
	}
	tool.OutputText = output
	return true
}

func toolUpdateText(event agentctl.AgentEvent) string {
	var text strings.Builder
	text.WriteString(event.Text)
	if event.NormalizedPayload != nil && event.NormalizedPayload.ShellExec() != nil {
		if output := event.NormalizedPayload.ShellExec().Output; output != nil {
			text.WriteString(output.Stdout)
			text.WriteString(output.Stderr)
		}
	}
	for _, item := range event.ToolCallContents {
		if item.Content != nil && item.Content.Type == "text" {
			text.WriteString(item.Content.Text)
		}
	}
	return text.String()
}

func (e *AgentExecution) observeToolPermission(event agentctl.AgentEvent) {
	if event.Type != "permission_request" || event.ToolCallID == "" {
		return
	}
	e.activeToolMu.Lock()
	defer e.activeToolMu.Unlock()
	if tool, exists := e.openTools[event.ToolCallID]; exists {
		tool.Status = "awaiting_permission"
		e.openTools[event.ToolCallID] = tool
		e.toolRevision++
	}
}
