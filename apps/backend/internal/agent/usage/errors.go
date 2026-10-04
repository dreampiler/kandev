package usage

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRetryAfter bounds provider-supplied Retry-After hints so an absurd or
// hostile header cannot park an account indefinitely.
const maxRetryAfter = 15 * time.Minute

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
// detail for debugging, but Error() only exposes the provider, reason,
// status, and rounded retry hint, so logging it cannot leak a response body.
type FetchError struct {
	Provider   string
	Reason     FetchFailure
	Status     int
	RetryAfter time.Duration
	Err        error
}

func (e *FetchError) Error() string {
	if e.RetryAfter > 0 {
		if e.Status != 0 {
			return fmt.Sprintf("%s usage: %s (status %d, retry after %s)", e.Provider, e.Reason, e.Status, e.RetryAfter.Round(time.Second))
		}
		return fmt.Sprintf("%s usage: %s (retry after %s)", e.Provider, e.Reason, e.RetryAfter.Round(time.Second))
	}
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

// ParseRetryAfter parses a Retry-After header in either delta-seconds or
// HTTP-date format, clamped between zero and maxRetryAfter.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	var delay time.Duration
	if seconds, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		delay = time.Duration(seconds) * time.Second
	} else if parsed, parseErr := http.ParseTime(trimmed); parseErr == nil {
		if !parsed.After(now) {
			return 0
		}
		delay = parsed.Sub(now)
	} else {
		return 0
	}
	if delay > maxRetryAfter {
		return maxRetryAfter
	}
	return delay
}

// statusFailure classifies a non-OK provider response. Authentication and
// permission refusals are separated so an operator can tell a missing scope
// from a provider outage. When header is provided, Retry-After is parsed.
func statusFailure(provider string, status int, header ...http.Header) *FetchError {
	reason := FailureHTTPStatus
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		reason = FailureUnauthorized
	}
	var retryAfter time.Duration
	if len(header) > 0 && header[0] != nil {
		retryAfter = ParseRetryAfter(header[0].Get("Retry-After"), time.Now())
	}
	return &FetchError{Provider: provider, Reason: reason, Status: status, RetryAfter: retryAfter}
}
