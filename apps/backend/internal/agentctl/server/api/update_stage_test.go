package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	ws "github.com/kandev/kandev/pkg/websocket"
)

func TestUpdateStreamStageHeartbeatIsControlMetadata(t *testing.T) {
	var frame []byte
	server := &Server{}
	err := server.writeUpdateStreamStageHeartbeat(func(data []byte) error {
		frame = append([]byte(nil), data...)
		return nil
	}, 2, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var msg ws.Message
	if err := json.Unmarshal(frame, &msg); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	if msg.Type != ws.MessageTypeNotification || msg.Action != streams.UpdateStreamStageAction {
		t.Fatalf("heartbeat envelope = %s/%s", msg.Type, msg.Action)
	}
	var stage streams.UpdateStreamStageHeartbeat
	if err := msg.ParsePayload(&stage); err != nil {
		t.Fatal(err)
	}
	if stage.WriterSent != 2 || stage.WriterAgeMillis < 0 {
		t.Fatalf("writer stage = %#v", stage)
	}
}
