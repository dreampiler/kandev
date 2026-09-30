// Package routingpolicy evaluates the provider-error policy shared by dynamic
// profiles and every execution surface that can invoke one.
package routingpolicy

import (
	"encoding/json"
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
	MinUnclassifiedThreshold  int64 = 2
	MaxUnclassifiedThreshold  int64 = 10
)

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

type RepeatedFailurePolicy struct {
	Enabled   bool  `json:"enabled"`
	Threshold int64 `json:"threshold"`
}

type Policy struct {
	Retry        RetryPolicy     `json:"retry"`
	WaitForReset ResetWaitPolicy `json:"wait_for_reset"`
	OnExhausted  Outcome         `json:"on_exhausted"`
}

type UnclassifiedPolicy struct {
	Enabled                     bool                   `json:"enabled"`
	ConsecutiveFailureThreshold int64                  `json:"consecutive_failure_threshold"`
	OnExhausted                 Outcome                `json:"on_exhausted,omitempty"`
	RepeatedFailure             *RepeatedFailurePolicy `json:"repeated_failure,omitempty"`
}

func (policy *UnclassifiedPolicy) UnmarshalJSON(data []byte) error {
	var wire struct {
		Enabled                     *bool                  `json:"enabled"`
		ConsecutiveFailureThreshold *int64                 `json:"consecutive_failure_threshold"`
		OnExhausted                 Outcome                `json:"on_exhausted"`
		RepeatedFailure             *RepeatedFailurePolicy `json:"repeated_failure"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*policy = UnclassifiedPolicy{OnExhausted: wire.OnExhausted, RepeatedFailure: wire.RepeatedFailure}
	if wire.Enabled != nil {
		policy.Enabled = *wire.Enabled
	}
	if wire.ConsecutiveFailureThreshold != nil {
		policy.ConsecutiveFailureThreshold = *wire.ConsecutiveFailureThreshold
	}
	if wire.RepeatedFailure == nil {
		return nil
	}
	legacy := *wire.RepeatedFailure
	if err := validateUnclassifiedAliasAgreement(wire.Enabled, wire.ConsecutiveFailureThreshold, &legacy); err != nil {
		return err
	}
	if wire.Enabled == nil {
		policy.Enabled = legacy.Enabled
	}
	if wire.ConsecutiveFailureThreshold == nil {
		policy.ConsecutiveFailureThreshold = legacy.Threshold
		if legacy.Enabled && legacy.Threshold == 1 {
			policy.ConsecutiveFailureThreshold = MinUnclassifiedThreshold
			legacy.Threshold = MinUnclassifiedThreshold
		}
	}
	policy.RepeatedFailure = &legacy
	return nil
}

func validateUnclassifiedAliasAgreement(enabled *bool, threshold *int64, legacy *RepeatedFailurePolicy) error {
	if enabled != nil && *enabled != legacy.Enabled {
		return errors.New("legacy repeated_failure.enabled conflicts with canonical enabled")
	}
	if threshold != nil && *threshold != legacy.Threshold {
		return errors.New("legacy repeated_failure.threshold conflicts with canonical threshold")
	}
	return nil
}

type Document struct {
	Version      int64               `json:"version"`
	Transient    Policy              `json:"transient"`
	Hard         Policy              `json:"hard"`
	Unclassified *UnclassifiedPolicy `json:"unclassified,omitempty"`
}

func DefaultPolicy() Policy {
	return Policy{OnExhausted: OutcomeSkip}
}

func DefaultDocument() Document {
	return Document{
		Version:      Version,
		Transient:    DefaultPolicy(),
		Hard:         DefaultPolicy(),
		Unclassified: &UnclassifiedPolicy{OnExhausted: OutcomeStop},
	}
}

func (document Document) PolicyFor(class routingerr.Class) (Policy, bool) {
	switch class {
	case routingerr.ClassTransient:
		return document.Transient, true
	case routingerr.ClassHard:
		return document.Hard, true
	default:
		return Policy{}, false
	}
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
	return validateUnclassifiedPolicy(document.Unclassified)
}

func validateUnclassifiedPolicy(policy *UnclassifiedPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.Enabled && (policy.ConsecutiveFailureThreshold < MinUnclassifiedThreshold || policy.ConsecutiveFailureThreshold > MaxUnclassifiedThreshold) {
		return fmt.Errorf("unclassified.consecutive_failure_threshold must be between %d and %d", MinUnclassifiedThreshold, MaxUnclassifiedThreshold)
	}
	if !policy.Enabled && policy.ConsecutiveFailureThreshold != 0 {
		return errors.New("disabled unclassified threshold must be zero")
	}
	return validateRepeatedFailureCompatibility(policy)
}

func validateRepeatedFailureCompatibility(policy *UnclassifiedPolicy) error {
	legacy := policy.RepeatedFailure
	if legacy == nil {
		return nil
	}
	if legacy.Enabled && (legacy.Threshold < MinUnclassifiedThreshold || legacy.Threshold > MaxUnclassifiedThreshold) {
		return fmt.Errorf("legacy unclassified.repeated_failure.threshold must be between %d and %d", MinUnclassifiedThreshold, MaxUnclassifiedThreshold)
	}
	if !legacy.Enabled && legacy.Threshold != 0 {
		return errors.New("disabled legacy repeated_failure threshold must be zero")
	}
	if legacy.Enabled != policy.Enabled || (legacy.Enabled && legacy.Threshold != policy.ConsecutiveFailureThreshold) {
		return errors.New("legacy repeated_failure fields conflict with the canonical unclassified policy")
	}
	return nil
}

func ValidatePolicy(policy Policy) error {
	if policy.OnExhausted != OutcomeSkip && policy.OnExhausted != OutcomeStop {
		return fmt.Errorf("on_exhausted must be %q or %q", OutcomeSkip, OutcomeStop)
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
	if input.Failure == nil || !input.EffectSafe || !input.Failure.FallbackAllowed {
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
