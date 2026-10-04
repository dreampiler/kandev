package models

import (
	"encoding/json"
	"time"
)

// Turn metadata keys owned by the child-turn stalled signal. The source turn
// row is the durable candidate record; these keys stay compact.
const (
	// TurnMetaKeyChildStallStart is written in the turn-insert transaction for
	// a task that has a parent. It captures the parent, workspace, and workflow
	// entry the turn started in.
	TurnMetaKeyChildStallStart = "child_stall_start"
	// TurnMetaKeyChildStallSettlement records a non-live settlement
	// (abandoned or cancelled). Absent means a live settlement.
	TurnMetaKeyChildStallSettlement = "child_stall_settlement"
	// TurnMetaKeyChildStall holds the producer's classification and delivery
	// state for the turn.
	TurnMetaKeyChildStall = "child_stall"
	// TurnMetaKeyChildStallResolved marks a turn the producer no longer scans.
	TurnMetaKeyChildStallResolved = "child_stall_resolved"
)

// Child-turn settlement kinds stored under TurnMetaKeyChildStallSettlement.
const (
	ChildStallSettlementAbandoned = "abandoned"
	ChildStallSettlementCancelled = "cancelled"
)

// ChildStallStart is the start snapshot stored under TurnMetaKeyChildStallStart.
// A zero TransitionID means the workflow entry was unknown at turn start.
type ChildStallStart struct {
	ParentTaskID string `json:"parent_task_id"`
	WorkspaceID  string `json:"workspace_id"`
	TransitionID int64  `json:"transition_id,omitempty"`
}

// ToMap returns the JSON-compatible metadata value.
func (s ChildStallStart) ToMap() map[string]interface{} {
	value := map[string]interface{}{
		"parent_task_id": s.ParentTaskID,
		"workspace_id":   s.WorkspaceID,
	}
	if s.TransitionID != 0 {
		value["transition_id"] = s.TransitionID
	}
	return value
}

// ChildStallState is the producer state stored under TurnMetaKeyChildStall.
type ChildStallState struct {
	State            string     `json:"state"`
	Cause            string     `json:"cause,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	QuestionID       string     `json:"question_id,omitempty"`
	QueueID          string     `json:"queue_id,omitempty"`
	ParentSessionID  string     `json:"parent_session_id,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	Attempts         int        `json:"attempts,omitempty"`
	NextAttemptAt    *time.Time `json:"next_attempt_at,omitempty"`
	OperatorNotified bool       `json:"operator_notified,omitempty"`
	FirstSeenAt      *time.Time `json:"first_seen_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// ToMap returns the JSON-compatible metadata value.
func (s ChildStallState) ToMap() map[string]interface{} {
	encoded, err := json.Marshal(s)
	if err != nil {
		return map[string]interface{}{"state": s.State}
	}
	var value map[string]interface{}
	if err := json.Unmarshal(encoded, &value); err != nil {
		return map[string]interface{}{"state": s.State}
	}
	return value
}

// LoadChildStallStart decodes the start snapshot from turn metadata.
func LoadChildStallStart(metadata map[string]interface{}) (ChildStallStart, bool) {
	var start ChildStallStart
	if !decodeTurnMetadataValue(metadata, TurnMetaKeyChildStallStart, &start) || start.ParentTaskID == "" {
		return ChildStallStart{}, false
	}
	return start, true
}

// LoadChildStallState decodes the producer state from turn metadata.
func LoadChildStallState(metadata map[string]interface{}) (ChildStallState, bool) {
	var state ChildStallState
	if !decodeTurnMetadataValue(metadata, TurnMetaKeyChildStall, &state) {
		return ChildStallState{}, false
	}
	return state, true
}

// ChildStallSettlement returns the recorded non-live settlement kind, or "".
func ChildStallSettlement(metadata map[string]interface{}) string {
	value, _ := metadata[TurnMetaKeyChildStallSettlement].(string)
	return value
}

func decodeTurnMetadataValue(metadata map[string]interface{}, key string, target interface{}) bool {
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	return json.Unmarshal(encoded, target) == nil
}
