package api

import (
	"encoding/json"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/adapter"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
	ws "github.com/kandev/kandev/pkg/websocket"
)

type updateStreamWriteStage struct {
	sent     uint64
	lastSent time.Time
}

// Only the successful agent-event write path calls this wrapper. MCP replies
// and control metadata neither advance the durable cursor nor these counters.
func (s *updateStreamWriteStage) wrap(write func([]byte) error) func([]byte) error {
	return func(data []byte) error {
		err := write(data)
		if err == nil {
			s.sent++
			s.lastSent = time.Now()
		}
		return err
	}
}

func (w *durableAgentStreamWriter) writeNotification(notification adapter.AgentEvent, replay bool) bool {
	if w.stage == nil {
		w.stage = &updateStreamWriteStage{}
	}
	return w.server.writeAgentStreamNotification(notification, w.stage.wrap(w.writeMessage), replay)
}

func (w *durableAgentStreamWriter) writeHeartbeat() bool {
	if w.stage == nil {
		w.stage = &updateStreamWriteStage{}
	}
	return w.server.writeUpdateStreamStageHeartbeat(w.writeMessage, w.stage.sent, w.stage.lastSent) == nil
}

func (w *durableAgentStreamWriter) serviceHeartbeat() bool {
	select {
	case <-w.heartbeat:
		return w.writeHeartbeat()
	default:
		return true
	}
}

func (s *Server) writeUpdateStreamStageHeartbeat(writeMessage func([]byte) error, sent uint64, lastSent time.Time) error {
	stage := streams.UpdateStreamStageHeartbeat{AdapterAgeMillis: -1, WriterAgeMillis: -1}
	if s.procMgr != nil {
		stage = s.procMgr.UpdateStreamStage()
	}
	stage.WriterSent = sent
	if !lastSent.IsZero() {
		stage.WriterAgeMillis = max(0, time.Since(lastSent).Milliseconds())
	}
	msg, err := ws.NewNotification(streams.UpdateStreamStageAction, stage)
	if err != nil {
		return err
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return writeMessage(data)
}
