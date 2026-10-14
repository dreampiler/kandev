package models

import (
	"encoding/json"
	"fmt"
	"sort"
)

const MetaKeyWorkflowInitialRecipientBinding = "workflow_initial_recipient_binding"

// WorkflowInitialRecipientBinding selects a recovery successor without changing
// the historical initial conversation or an entry's recorded route.
type WorkflowInitialRecipientBinding struct {
	InitialSessionID string `json:"initial_session_id"`
	SourceSessionID  string `json:"source_session_id"`
	SessionID        string `json:"session_id"`
	AgentProfileID   string `json:"agent_profile_id"`
}

// RecordedWorkflowInitialSessionID reads historical identity without electing a
// legacy session or constructing configuration that was never recorded.
func RecordedWorkflowInitialSessionID(metadata map[string]interface{}) (string, error) {
	if snapshot, ok := LoadWorkflowInitialSessionSnapshot(metadata); ok {
		return snapshot.SessionID, nil
	}
	binding, err := LoadWorkflowInitialRecipientBinding(metadata)
	if err != nil || binding == nil {
		return "", err
	}
	return binding.InitialSessionID, nil
}

// MarkedOriginalTaskSession distinguishes an absent marker from ambiguity.
func MarkedOriginalTaskSession(sessions []*TaskSession) (*TaskSession, bool) {
	var original *TaskSession
	for _, session := range sessions {
		if session == nil || !IsOriginalTaskSession(session.Metadata) {
			continue
		}
		if original != nil {
			return nil, true
		}
		original = session
	}
	return original, original != nil
}

func LegacyOriginalTaskSession(sessions []*TaskSession) *TaskSession {
	legacy := make([]*TaskSession, 0, len(sessions))
	for _, session := range sessions {
		if session != nil && session.Metadata[SessionMetaKeyCreatedBy] != SessionCreatedByWorkflowSwitch {
			legacy = append(legacy, session)
		}
	}
	sort.SliceStable(legacy, func(i, j int) bool {
		left, right := legacy[i].StartedAt, legacy[j].StartedAt
		if left.Equal(right) {
			return legacy[i].ID < legacy[j].ID
		}
		if left.IsZero() {
			return false
		}
		return right.IsZero() || left.Before(right)
	})
	if len(legacy) == 0 || (len(legacy) > 1 && legacy[0].StartedAt.Equal(legacy[1].StartedAt)) {
		return nil
	}
	return legacy[0]
}

func LoadWorkflowInitialRecipientBinding(metadata map[string]interface{}) (*WorkflowInitialRecipientBinding, error) {
	value, present := metadata[MetaKeyWorkflowInitialRecipientBinding]
	if !present {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode initial recipient binding: %w", err)
	}
	var binding WorkflowInitialRecipientBinding
	if err := json.Unmarshal(data, &binding); err != nil {
		return nil, fmt.Errorf("decode initial recipient binding: %w", err)
	}
	if binding.InitialSessionID == "" || binding.SourceSessionID == "" || binding.SessionID == "" ||
		binding.AgentProfileID == "" || binding.SourceSessionID == binding.SessionID {
		return nil, fmt.Errorf("invalid initial recipient binding")
	}
	return &binding, nil
}

// ValidateInitialRecipientRoute fences a new entry's preflight selection. A
// recorded entry retains its accepted selection through later recovery.
func ValidateInitialRecipientRoute(metadata map[string]interface{}, route *WorkflowSessionRoute) error {
	if route == nil || route.TargetKind != "initial" {
		return nil
	}
	if saved, ok := LoadWorkflowSessionRoute(metadata); ok && saved.OperationID == route.OperationID {
		return nil
	}
	current, err := LoadWorkflowInitialRecipientBinding(metadata)
	if err != nil {
		return err
	}
	expected := route.InitialRecipientBinding
	if (current == nil) != (expected == nil) || (current != nil && *current != *expected) {
		return fmt.Errorf("initial recipient changed after workflow preflight")
	}
	return nil
}
