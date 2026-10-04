package models

// AdmittedSessionRef is one session in the instance-wide admitted population
// together with the agent profile it runs.
//
// It lives in models rather than in either consumer because the admission
// controller and the session repository must agree on this shape, and the
// controller must classify the lane itself: the profile travels as durable data
// so no repository, caller or transport can decide a session's class.
type AdmittedSessionRef struct {
	ID        string
	ProfileID string
}
