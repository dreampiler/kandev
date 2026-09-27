// Package routingpolicy evaluates the provider-error policy shared by dynamic
// profiles and every execution surface that can invoke one.
package routingpolicy

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

const (
	Version int64 = 1

	MinRetries                int64 = 1
	MaxRetries                int64 = 10
	MinInitialIntervalSeconds int64 = 1
	MaxInitialIntervalSeconds int64 = 3600
	MaxRetryDelay                   = 24 * time.Hour
	MinResetWaitSeconds       int64 = 1
	MaxResetWaitSeconds       int64 = 7 * 24 * 60 * 60

	// MinRepeatedFailureThreshold is the smallest number of consecutive
	// same-profile failures that may trigger a try-next fallback. A threshold
	// of 1 is valid and means "fall back on the first classified failure".
	MinRepeatedFailureThreshold int64 = 1
	MaxRepeatedFailureThreshold int64 = 10
)

// RepeatedFailurePolicy allows an otherwise non-fallbackable failure to try
// the next candidate once the same concrete execution profile has produced
// the same class of failure repeatedly. It exists so an environment-specific
// defect that always classifies as unclassified cannot pin a dynamic profile
// to one candidate forever; it never overrides the effect-safety invariant.
type RepeatedFailurePolicy struct {
	Enabled   bool  `json:"enabled"`
	Threshold int64 `json:"threshold"`
}

type Outcome string

const (
	OutcomeSkip Outcome = "skip"
	OutcomeStop Outcome = "stop"
)

type RetryPolicy struct {
	Enabled                bool  `json:"enabled"`
	MaxRetries             int64 `json:"max_retries"`
	InitialIntervalSeconds int64 `json:"initial_interval_seconds"`
}

type ResetWaitPolicy struct {
	Enabled        bool  `json:"enabled"`
	MaxWaitSeconds int64 `json:"max_wait_seconds"`
}

type Policy struct {
	Retry        RetryPolicy     `json:"retry"`
	WaitForReset ResetWaitPolicy `json:"wait_for_reset"`
	OnExhausted  Outcome         `json:"on_exhausted"`
	// RepeatedFailure is only consulted for the unclassified class. It is a
	// deliberate, opt-in override of the classifier's FallbackAllowed=false
	// invariant for a concrete profile that keeps failing the same way.
	RepeatedFailure RepeatedFailurePolicy `json:"repeated_failure"`
}

type Document struct {
	Version   int64  `json:"version"`
	Transient Policy `json:"transient"`
	Hard      Policy `json:"hard"`
	// Unclassified is intentionally separate from the two provider-recovery
	// classes. A missing or disabled value keeps the historical fail-closed
	// behavior: an unclassified failure stops the route.
	Unclassified Policy `json:"unclassified"`
}

func DefaultPolicy() Policy {
	return Policy{OnExhausted: OutcomeSkip}
}

func DefaultDocument() Document {
	return Document{
		Version: Version, Transient: DefaultPolicy(), Hard: DefaultPolicy(),
		Unclassified: Policy{OnExhausted: OutcomeStop},
	}
}

func (document Document) PolicyFor(class routingerr.Class) (Policy, bool) {
	switch class {
	case routingerr.ClassTransient:
		return document.Transient, true
	case routingerr.ClassHard:
		return document.Hard, true
	case routingerr.ClassUnclassified:
		if !document.Unclassified.RepeatedFailure.Enabled {
			return Policy{}, false
		}
		return document.Unclassified, true
	default:
		return Policy{}, false
	}
}

// RepeatedFailureEnabled reports whether the document opts into the
// repeated-failure override for the class. It is false for every class whose
// policy is absent, so fail-closed callers can gate on it directly.
func (document Document) RepeatedFailureEnabled(class routingerr.Class) bool {
	policy, ok := document.PolicyFor(class)
	return ok && policy.RepeatedFailure.Enabled
}

func ValidateDocument(document Document) error {
	if document.Version != Version {
		return fmt.Errorf("unsupported routing policy version %d", document.Version)
	}
	if err := ValidatePolicy(document.Transient); err != nil {
		return fmt.Errorf("transient policy: %w", err)
	}
	if err := ValidatePolicy(document.Hard); err != nil {
		return fmt.Errorf("hard policy: %w", err)
	}
	// The unclassified section is optional and fail-closed. Validate it only
	// when it opts in, so a persisted document written before the section
	// existed keeps loading unchanged.
	if document.Unclassified.RepeatedFailure.Enabled {
		if err := ValidatePolicy(document.Unclassified); err != nil {
			return fmt.Errorf("unclassified policy: %w", err)
		}
	}
	return nil
}

func ValidatePolicy(policy Policy) error {
	if policy.OnExhausted != OutcomeSkip && policy.OnExhausted != OutcomeStop {
		return fmt.Errorf("on_exhausted must be %q or %q", OutcomeSkip, OutcomeStop)
	}
	if policy.RepeatedFailure.Enabled {
		if policy.RepeatedFailure.Threshold < MinRepeatedFailureThreshold || policy.RepeatedFailure.Threshold > MaxRepeatedFailureThreshold {
			return fmt.Errorf("repeated_failure.threshold must be between %d and %d", MinRepeatedFailureThreshold, MaxRepeatedFailureThreshold)
		}
	} else if policy.RepeatedFailure.Threshold != 0 {
		return errors.New("disabled repeated_failure threshold must be zero")
	}
	if policy.Retry.Enabled {
		if policy.Retry.MaxRetries < MinRetries || policy.Retry.MaxRetries > MaxRetries {
			return fmt.Errorf("retry.max_retries must be between %d and %d", MinRetries, MaxRetries)
		}
		if policy.Retry.InitialIntervalSeconds < MinInitialIntervalSeconds || policy.Retry.InitialIntervalSeconds > MaxInitialIntervalSeconds {
			return fmt.Errorf("retry.initial_interval_seconds must be between %d and %d", MinInitialIntervalSeconds, MaxInitialIntervalSeconds)
		}
	} else if policy.Retry.MaxRetries != 0 || policy.Retry.InitialIntervalSeconds != 0 {
		return errors.New("disabled retry values must be zero")
	}
	if policy.WaitForReset.Enabled {
		if policy.WaitForReset.MaxWaitSeconds < MinResetWaitSeconds || policy.WaitForReset.MaxWaitSeconds > MaxResetWaitSeconds {
			return fmt.Errorf("wait_for_reset.max_wait_seconds must be between %d and %d", MinResetWaitSeconds, MaxResetWaitSeconds)
		}
	} else if policy.WaitForReset.MaxWaitSeconds != 0 {
		return errors.New("disabled wait_for_reset value must be zero")
	}
	return nil
}

type DecisionKind string

const (
	DecisionWaitForReset DecisionKind = "waiting_for_reset"
	DecisionRetry        DecisionKind = "retry_wait"
	DecisionSkip         DecisionKind = "skip"
	DecisionStop         DecisionKind = "stop"
)

type EvaluationInput struct {
	Failure       *routingerr.Error
	Now           time.Time
	RetryOrdinal  int64
	ResetWaitUsed bool
	EffectSafe    bool
	// ConsecutiveFailures is the number of consecutive failures the same
	// concrete execution profile has produced for this route. It is only
	// consulted for the unclassified class.
	ConsecutiveFailures int64
}

type Evaluation struct {
	Kind             DecisionKind
	Class            routingerr.Class
	Code             routingerr.Code
	CatalogueVersion string
	RetryOrdinal     int64
	Deadline         time.Time
	PendingOutcome   Outcome
}

func Evaluate(document Document, input EvaluationInput) Evaluation {
	result := Evaluation{Kind: DecisionStop, PendingOutcome: OutcomeStop}
	if input.Failure == nil || !input.EffectSafe {
		return result
	}
	class := failureClass(input.Failure)
	policy, ok := document.PolicyFor(class)
	if !ok || ValidateDocument(document) != nil {
		return result
	}
	result.Class = class
	result.Code = input.Failure.Code
	result.CatalogueVersion = input.Failure.CatalogueVersion
	if result.CatalogueVersion == "" {
		result.CatalogueVersion = routingerr.CatalogueVersion
	}
	result.PendingOutcome = policy.OnExhausted

	if !input.Failure.FallbackAllowed {
		// The classifier forbids falling back for this failure. The only
		// override is the repeated-failure policy: the same concrete profile
		// has failed the same way often enough that trying a different
		// candidate is worth more than stopping, and the failed turn was
		// proven not to have produced a side effect.
		if !repeatedFailureOverride(class, policy, input.ConsecutiveFailures) {
			return result
		}
		result.Kind = DecisionSkip
		result.PendingOutcome = OutcomeSkip
		return result
	}

	if resetAt, ok := resetWaitDeadline(policy, input); ok {
		result.Kind = DecisionWaitForReset
		result.Deadline = resetAt
		return result
	}
	if retryAt, retryOrdinal, ok := retryDeadline(policy, input); ok {
		result.Kind = DecisionRetry
		result.RetryOrdinal = retryOrdinal
		result.Deadline = retryAt
		return result
	}
	if policy.OnExhausted == OutcomeSkip {
		result.Kind = DecisionSkip
	}
	return result
}

// repeatedFailureOverride reports whether an otherwise non-fallbackable
// failure may try the next candidate because the same concrete profile has
// repeatedly produced the same unclassified failure. It is deliberately
// narrow: only the unclassified class, only when the profile opted in, and
// only once the threshold is reached.
func repeatedFailureOverride(class routingerr.Class, policy Policy, consecutive int64) bool {
	if class != routingerr.ClassUnclassified || !policy.RepeatedFailure.Enabled {
		return false
	}
	return consecutive >= policy.RepeatedFailure.Threshold
}

func failureClass(failure *routingerr.Error) routingerr.Class {
	if failure.Class != "" {
		return failure.Class
	}
	return routingerr.ClassForCode(failure.Code)
}

func resetWaitDeadline(policy Policy, input EvaluationInput) (time.Time, bool) {
	if !policy.WaitForReset.Enabled || input.ResetWaitUsed || input.Failure.ResetHint == nil {
		return time.Time{}, false
	}
	resetAt := input.Failure.ResetHint.UTC()
	maxWait := time.Duration(policy.WaitForReset.MaxWaitSeconds) * time.Second
	if !resetAt.After(input.Now) || resetAt.After(input.Now.Add(maxWait)) {
		return time.Time{}, false
	}
	return resetAt, true
}

func retryDeadline(policy Policy, input EvaluationInput) (time.Time, int64, bool) {
	if !policy.Retry.Enabled || input.RetryOrdinal >= policy.Retry.MaxRetries {
		return time.Time{}, 0, false
	}
	delay, err := NextRetryDelay(policy.Retry.InitialIntervalSeconds, input.RetryOrdinal)
	if err != nil {
		return time.Time{}, 0, false
	}
	return input.Now.Add(delay), input.RetryOrdinal + 1, true
}

func NextRetryDelay(initialIntervalSeconds, retryOrdinal int64) (time.Duration, error) {
	if initialIntervalSeconds < MinInitialIntervalSeconds || retryOrdinal < 0 {
		return 0, errors.New("invalid retry interval or ordinal")
	}
	if initialIntervalSeconds > MaxInitialIntervalSeconds {
		return 0, errors.New("retry interval exceeds limit")
	}
	if retryOrdinal >= 63 {
		return MaxRetryDelay, nil
	}
	seconds := uint64(initialIntervalSeconds)
	if retryOrdinal > 0 {
		if retryOrdinal >= 64 || seconds > math.MaxUint64/(uint64(1)<<retryOrdinal) {
			return MaxRetryDelay, nil
		}
		seconds *= uint64(1) << retryOrdinal
	}
	if seconds >= uint64(MaxRetryDelay/time.Second) {
		return MaxRetryDelay, nil
	}
	return time.Duration(seconds) * time.Second, nil
}
