package models

// AgentDeliverySupersededToolUpdate identifies one projected in-progress
// tool_update inbox row that a later update for the same tool call replaces.
// ContentHash is the versioned hash computed when the row was received; it
// binds the caller's tool call and status observation to the persisted row.
type AgentDeliverySupersededToolUpdate struct {
	Sequence    int64
	ContentHash string
}
