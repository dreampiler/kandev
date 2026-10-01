package lifecycle

import (
	client "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"go.uber.org/zap"
)

func tryUpdateStreamStageSnapshot(execution *AgentExecution) (client.UpdateStreamStageSnapshot, bool) {
	if execution == nil || !execution.agentctlLifecycleMu.TryRLock() {
		return client.UpdateStreamStageSnapshot{}, false
	}
	defer execution.agentctlLifecycleMu.RUnlock()
	streamClient := execution.currentAgentCtlClient()
	if streamClient == nil {
		return client.UpdateStreamStageSnapshot{}, false
	}
	return streamClient.UpdateStreamStage()
}

func upstreamStageAge(snapshot client.UpdateStreamStageSnapshot, ageAtHeartbeat int64) int64 {
	if snapshot.HeartbeatAgeMillis < 0 || snapshot.HeartbeatAgeMillis > 30_000 || ageAtHeartbeat < 0 {
		return -1
	}
	return snapshot.HeartbeatAgeMillis + ageAtHeartbeat
}

func (sm *SessionManager) logUpdateStreamStage(execution *AgentExecution, promptGeneration uint64) {
	snapshot, available := tryUpdateStreamStageSnapshot(execution)
	adapterAge, writerAge := int64(-1), int64(-1)
	readAge, handledAge, heartbeatAge := int64(-1), int64(-1), int64(-1)
	if available {
		adapterAge = upstreamStageAge(snapshot, snapshot.AgentctlStage.AdapterAgeMillis)
		writerAge = upstreamStageAge(snapshot, snapshot.AgentctlStage.WriterAgeMillis)
		readAge, handledAge, heartbeatAge = snapshot.LastReadAgeMillis, snapshot.LastHandledAgeMillis, snapshot.HeartbeatAgeMillis
	}
	sm.logger.Warn("agent update stream stage at first stall",
		zap.String("execution_id", execution.ID), zap.Uint64("prompt_generation", promptGeneration),
		zap.Bool("stream_stage_available", available), zap.Bool("stream_connected", available && snapshot.Connected),
		zap.Int64("adapter_event_age_ms", adapterAge), zap.Int64("stream_send_age_ms", writerAge),
		zap.Int64("backend_read_age_ms", readAge), zap.Int64("backend_handler_age_ms", handledAge),
		zap.Int64("heartbeat_age_ms", heartbeatAge),
		zap.Uint64("backend_read_count", snapshot.ReadCount), zap.Uint64("backend_handled_count", snapshot.HandledCount),
		zap.Uint64("handler_pending_count", snapshot.Pending))
}
