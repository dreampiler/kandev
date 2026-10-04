package streams

type ToolProgressRequest struct {
	SessionID   string   `json:"session_id"`
	ToolCallIDs []string `json:"tool_call_ids"`
}

type ToolProgressResponse struct {
	Tools []ToolProgressObservation `json:"tools"`
}

// ForegroundState concerns the invoking command, not surviving background children.
type ToolProgressObservation struct {
	ToolCallID      string `json:"tool_call_id"`
	ForegroundState string `json:"foreground_state"`
	CPUProgress     bool   `json:"cpu_progress"`
}
