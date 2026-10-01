package client

import (
	"encoding/json"
	"testing"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	ws "github.com/kandev/kandev/pkg/websocket"
)

func TestUpdateStreamStageBlockedHandlerKeepsReadProgress(t *testing.T) {
	stage := newUpdateStreamStage()
	stage.recordRead()
	before := stage.snapshot()
	if before.ReadCount != 1 || before.Pending != 1 || before.HandledCount != 0 {
		t.Fatalf("blocked worker snapshot = %#v, want one unread handler item", before)
	}
	stage.recordRead()
	stage.recordHandled()
	after := stage.snapshot()
	if after.ReadCount != 2 || after.HandledCount != 1 || after.Pending != 1 {
		t.Fatalf("continuing reader snapshot = %#v, want one pending item", after)
	}
}

func TestUpdateStreamStageHeartbeatIsNotTurnProgress(t *testing.T) {
	stage := newUpdateStreamStage()
	payload, err := json.Marshal(streams.UpdateStreamStageHeartbeat{AdapterSeen: 3, WriterSent: 2})
	if err != nil {
		t.Fatal(err)
	}
	msg := ws.Message{Type: ws.MessageTypeNotification, Action: streams.UpdateStreamStageAction, Payload: payload}
	client := &Client{}
	if !client.consumeUpdateStreamMetadata(stage, msg) {
		t.Fatal("stage heartbeat was not recognized")
	}
	snapshot := stage.snapshot()
	if snapshot.ReadCount != 0 || snapshot.HandledCount != 0 || snapshot.HeartbeatAgeMillis < 0 {
		t.Fatalf("heartbeat changed turn-event progress or was not recorded: %#v", snapshot)
	}
	if snapshot.AgentctlStage.WriterSent != 2 {
		t.Fatalf("agentctl stage = %#v", snapshot.AgentctlStage)
	}
}
