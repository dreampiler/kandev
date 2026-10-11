package repoerrors

import "errors"

var (
	// ErrAgentDeliverySubmissionConflict means an existing submission ID was
	// reused with a different immutable payload hash.
	ErrAgentDeliverySubmissionConflict = errors.New("agent delivery submission conflict")
	ErrAgentDeliverySubmissionNotFound = errors.New("agent delivery submission not found")
	ErrAgentDeliveryEventConflict      = errors.New("agent delivery event conflict")
	// ErrAgentDeliveryReplayExpired means the stream sequence is at or below the
	// stream's retired sequence, so its retained payload was removed. The event
	// must be treated as already handled: never re-inserted or re-projected.
	ErrAgentDeliveryReplayExpired          = errors.New("agent delivery replay expired")
	ErrAgentDeliveryEffectConflict         = errors.New("agent delivery effect conflict")
	ErrAgentDeliveryEffectNotFound         = errors.New("agent delivery effect not found")
	ErrAgentDeliveryReconstructionConflict = errors.New("agent delivery reconstruction conflicts with current state")
	ErrAgentDeliveryReconstructionStale    = errors.New("agent delivery reconstruction owner changed")
)
