package usage

import (
	"errors"
	"fmt"
	"net/http"
)

// FetchFailure is a bounded reason a usage read failed. It is safe to log and to
// return to clients because it never carries a credential or a provider body.
type FetchFailure string

const (
	FailureCredentialMissing FetchFailure = "credential_missing"
	FailureUnauthorized      FetchFailure = "unauthorized"
	FailureHTTPStatus        FetchFailure = "http_status"
	FailureNetwork           FetchFailure = "network"
	FailureDecode            FetchFailure = "decode"
	// FailureUnknown covers errors from clients that do not classify their
	// failures.
	FailureUnknown FetchFailure = "fetch_failed"
)

// FetchError is a classified usage read failure. The wrapped error may hold
// detail for debugging, but Error() only exposes the provider, reason and
// status, so logging it cannot leak a response body.
type FetchError struct {
	Provider string
	Reason   FetchFailure
	Status   int
	Err      error
}

func (e *FetchError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("%s usage: %s (status %d)", e.Provider, e.Reason, e.Status)
	}
	return fmt.Sprintf("%s usage: %s", e.Provider, e.Reason)
}

func (e *FetchError) Unwrap() error { return e.Err }

// FailureOf extracts the bounded reason and HTTP status from a usage error.
func FailureOf(err error) (FetchFailure, int) {
	var fetchErr *FetchError
	if errors.As(err, &fetchErr) {
		return fetchErr.Reason, fetchErr.Status
	}
	return FailureUnknown, 0
}

// statusFailure classifies a non-OK provider response. Authentication and
// permission refusals are separated so an operator can tell a missing scope
// from a provider outage.
func statusFailure(provider string, status int) *FetchError {
	reason := FailureHTTPStatus
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		reason = FailureUnauthorized
	}
	return &FetchError{Provider: provider, Reason: reason, Status: status}
}
