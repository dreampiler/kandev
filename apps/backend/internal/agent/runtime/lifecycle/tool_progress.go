package lifecycle

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

func (e *AgentExecution) stallThreshold() time.Duration {
	e.activeToolMu.RLock()
	defer e.activeToolMu.RUnlock()
	for _, tool := range e.openTools {
		if toolIsExecuting(tool.Status) && !tool.ForegroundExited {
			return toolStallEscalationThreshold
		}
	}
	return stallEscalationThreshold
}

func toolIsExecuting(status string) bool {
	switch status {
	case "in_progress", "running", "started":
		return true
	default:
		return false
	}
}

func (e *AgentExecution) toolProbeSnapshot() ([]string, uint64) {
	e.activeToolMu.RLock()
	defer e.activeToolMu.RUnlock()
	var ids []string
	for id, tool := range e.openTools {
		if toolIsExecuting(tool.Status) && !tool.ForegroundExited {
			ids = append(ids, id)
		}
	}
	return ids, e.toolRevision
}

func (e *AgentExecution) probeToolProgress(ctx context.Context, promptGeneration, startupGeneration uint64) {
	ids, revision := e.toolProbeSnapshot()
	if len(ids) == 0 || len(ids) > 64 || e.ACPSessionID == "" {
		return
	}
	client, release := e.AcquireAgentCtlClient()
	if client == nil {
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	result, err := client.ProbeToolProgress(requestCtx, streams.ToolProgressRequest{SessionID: e.ACPSessionID, ToolCallIDs: ids})
	cancel()
	release()
	if err != nil {
		return
	}
	e.startupCallbackMu.RLock()
	defer e.startupCallbackMu.RUnlock()
	if e.startupAttemptSnapshot() != startupGeneration {
		return
	}
	e.promptLifecycleMu.Lock()
	defer e.promptLifecycleMu.Unlock()
	if e.promptGeneration != promptGeneration || e.promptCompletionGeneration == promptGeneration {
		return
	}
	e.withAgentCtlClient(client, func() { e.applyToolProgress(revision, result) })
}

func (e *AgentExecution) applyToolProgress(revision uint64, result streams.ToolProgressResponse) {
	e.activeToolMu.Lock()
	defer e.activeToolMu.Unlock()
	if e.toolRevision != revision {
		return
	}
	seen := make(map[string]bool)
	for _, sample := range result.Tools {
		if seen[sample.ToolCallID] {
			return
		}
		seen[sample.ToolCallID] = true
	}
	progress, changed := false, false
	for _, sample := range result.Tools {
		tool, exists := e.openTools[sample.ToolCallID]
		if !exists || !toolIsExecuting(tool.Status) || tool.ForegroundExited {
			continue
		}
		switch sample.ForegroundState {
		case "exited":
			tool.ForegroundExited = true
			e.openTools[sample.ToolCallID] = tool
			changed = true
		case "running":
			progress = progress || sample.CPUProgress
		}
	}
	if changed {
		e.toolRevision++
	}
	if progress || changed {
		e.lastActivityAtMu.Lock()
		if progress {
			e.lastToolProgressAt = time.Now()
		}
		e.promptActivityEpoch++
		e.lastActivityAtMu.Unlock()
	}
}

func (e *AgentExecution) promptStallSnapshot() (time.Time, bool, uint64, uint64) {
	e.activeToolMu.RLock()
	defer e.activeToolMu.RUnlock()
	e.lastActivityAtMu.Lock()
	defer e.lastActivityAtMu.Unlock()
	activity := e.lastActivityAt
	if e.lastToolProgressAt.After(activity) {
		activity = e.lastToolProgressAt
	}
	return activity, e.agentEventSincePrompt, e.promptActivityEpoch, e.toolRevision
}

func (e *AgentExecution) stallSnapshotCurrent(epoch, revision uint64) bool {
	e.activeToolMu.RLock()
	defer e.activeToolMu.RUnlock()
	e.lastActivityAtMu.Lock()
	defer e.lastActivityAtMu.Unlock()
	return e.toolRevision == revision && e.promptActivityEpoch == epoch
}

// A sample may be invalidated while the bounded terminal publish is in flight.
func (e *AgentExecution) signalStallCompletion(startup, epoch, revision uint64, signal PromptCompletionSignal) bool {
	e.startupCallbackMu.RLock()
	defer e.startupCallbackMu.RUnlock()
	e.promptLifecycleMu.Lock()
	defer e.promptLifecycleMu.Unlock()
	if e.promptGeneration != 0 && (e.promptGeneration != signal.PromptGeneration || e.promptCompletionGeneration == signal.PromptGeneration) {
		return false
	}
	e.activeToolMu.RLock()
	defer e.activeToolMu.RUnlock()
	e.lastActivityAtMu.Lock()
	defer e.lastActivityAtMu.Unlock()
	if e.toolRevision != revision || e.promptActivityEpoch != epoch {
		return false
	}
	return e.signalPromptCompletionForStartupGenerationLeased(startup, signal)
}
