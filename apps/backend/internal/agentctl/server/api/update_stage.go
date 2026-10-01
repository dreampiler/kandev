package api

import (
	"encoding/json"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	ws "github.com/kandev/kandev/pkg/websocket"
)

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
