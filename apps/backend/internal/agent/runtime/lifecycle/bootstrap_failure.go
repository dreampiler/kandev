package lifecycle

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/task/models"
)

// StopReasonAgentBootstrapFailed lets the lifecycle manager reclaim a fresh
// failed launch while preserving an already-retained Kubernetes runtime.
const StopReasonAgentBootstrapFailed = "agent bootstrap failed"

// BootstrapFailure carries safe, operation-boundary evidence for a failure
// before the agent becomes ready. Cause remains available to backend logging
// and errors.Is/errors.As callers; user-facing projections must use Code and
// Detail instead of Error().
type BootstrapFailure struct {
	Operation      string
	Code           string
	Reason         string
	Detail         string
	RequestedModel string
	EffectiveModel string
	AttemptedModel string
	RequestedMode  string
	EffectiveMode  string
	PromptNotSent  *bool
	Cause          error
}

func (e *BootstrapFailure) Error() string {
	if e == nil {
		return "agent bootstrap failed"
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	if e.Detail != "" {
		return e.Detail
	}
	return "agent bootstrap failed"
}

func (e *BootstrapFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *BootstrapFailure) safeCode() string {
	if e == nil || !isKnownBootstrapCauseCode(e.Code) {
		return models.AgentErrorCauseCodeUnknown
	}
	return e.Code
}

// SafeCode returns the allowlisted operation-boundary reason for user-facing
// recovery projections. It never exposes the wrapped provider or transport
// error.
func (e *BootstrapFailure) SafeCode() string {
	return e.safeCode()
}

func (e *BootstrapFailure) safeDetail() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" || isSelectionBootstrapCause(e.safeCode()) || e.safeCode() == models.AgentErrorCauseCodeRuntimeCleanupFailed {
		return bootstrapFailureDetail(e.safeCode())
	}
	return routingerr.Sanitize(e.Detail)
}

// SafeDetail returns the operation-boundary detail. Callers must use this
// field instead of Error when building durable or user-facing records.
func (e *BootstrapFailure) SafeDetail() string {
	return e.safeDetail()
}

// SafeAgentErrorCause returns the bounded structured projection used by
// persistence and transport. Selector values are rejected when the routing
// sanitizer changes them; arbitrary provider errors remain only in Cause.
func (e *BootstrapFailure) SafeAgentErrorCause(operation string) (models.AgentErrorCause, bool) {
	if e == nil {
		return models.AgentErrorCause{}, false
	}
	if operation == "" {
		operation = e.Operation
	}
	cause := models.AgentErrorCause{
		Operation:      operation,
		Code:           e.SafeCode(),
		Detail:         e.SafeDetail(),
		Reason:         e.Reason,
		RequestedModel: safeBootstrapSelector(e.RequestedModel),
		EffectiveModel: safeBootstrapSelector(e.EffectiveModel),
		AttemptedModel: safeBootstrapSelector(e.AttemptedModel),
		RequestedMode:  safeBootstrapSelector(e.RequestedMode),
		EffectiveMode:  safeBootstrapSelector(e.EffectiveMode),
	}
	if e.PromptNotSent != nil {
		value := *e.PromptNotSent
		cause.PromptNotSent = &value
	}
	normalized := models.NormalizeAgentErrorCauses([]models.AgentErrorCause{cause})
	if len(normalized) == 0 {
		return models.AgentErrorCause{}, false
	}
	return normalized[0], true
}

func safeBootstrapSelector(value string) string {
	if len(value) > 256 || strings.TrimSpace(value) != value {
		return ""
	}
	if routingerr.SanitizeFullUnbounded(value) != value {
		return ""
	}
	return value
}

func isSelectionBootstrapCause(code string) bool {
	switch code {
	case models.AgentErrorCauseCodeModelUnavailable,
		models.AgentErrorCauseCodeModelSelectionFailed,
		models.AgentErrorCauseCodePermissionModeFailed,
		models.AgentErrorCauseCodePermissionModeUnconfirmed,
		models.AgentErrorCauseCodePermissionModeMismatch:
		return true
	default:
		return false
	}
}

func isKnownBootstrapCauseCode(code string) bool {
	switch code {
	case models.AgentErrorCauseCodeAuthenticationRequired,
		models.AgentErrorCauseCodePermissionDenied,
		models.AgentErrorCauseCodeDestinationInvalid,
		models.AgentErrorCauseCodeSourceBranchMissing,
		models.AgentErrorCauseCodeTransportUnavailable,
		models.AgentErrorCauseCodeRuntimeCleanupFailed,
		models.AgentErrorCauseCodeTimeout,
		models.AgentErrorCauseCodeModelUnavailable,
		models.AgentErrorCauseCodeModelSelectionFailed,
		models.AgentErrorCauseCodePermissionModeFailed,
		models.AgentErrorCauseCodePermissionModeUnconfirmed,
		models.AgentErrorCauseCodePermissionModeMismatch,
		models.AgentErrorCauseCodeUnknown:
		return true
	default:
		return false
	}
}

func bootstrapOperation(execution *AgentExecution) string {
	if execution != nil && execution.isResumedSession {
		return models.AgentErrorCauseOperationResume
	}
	return models.AgentErrorCauseOperationStart
}

func bootstrapFailureFor(execution *AgentExecution, err error) *BootstrapFailure {
	if err == nil {
		return nil
	}
	var existing *BootstrapFailure
	if errors.As(err, &existing) {
		if existing != nil && existing.Operation == "" {
			copy := *existing
			copy.Operation = bootstrapOperation(execution)
			return &copy
		}
		return existing
	}
	code := models.AgentErrorCauseCodeUnknown
	detail := bootstrapFailureDetail(code)
	var startupFailure *routingerr.AgentStartupFailure
	var managedFailure *routingerr.ManagedRuntimeStartupError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		// A deadline stays a timeout even when a startup type wraps it, so
		// timeout-based recovery keeps its signal.
		code = models.AgentErrorCauseCodeTimeout
	case errors.As(err, &startupFailure) && startupFailure != nil:
		code = models.AgentErrorCauseCodeTransportUnavailable
		detail = agentStartupFailureDetail(startupFailure)
	case errors.As(err, &managedFailure) && managedFailure != nil:
		code = models.AgentErrorCauseCodeTransportUnavailable
		detail = managedRuntimeStartupFailureDetail(managedFailure)
	}
	return &BootstrapFailure{
		Operation: bootstrapOperation(execution),
		Code:      code,
		Detail:    detail,
		Cause:     err,
	}
}

// maxBootstrapCauseDetailBytes keeps the projected cause well under the durable
// cause-detail budget (maxAgentErrorCauseDetailBytes = 1024) so the phase,
// agent id, and cause excerpt all survive normalization.
const maxBootstrapCauseDetailBytes = 512

// agentStartupFailureDetail turns trusted process/ACP-init evidence into a
// bounded detail. The raw diagnostic stays only in Cause for logs; the durable
// projection carries the phase, a safe agent identifier, and a sanitized cause.
func agentStartupFailureDetail(failure *routingerr.AgentStartupFailure) string {
	parts := []string{"agent startup failed", "phase=" + string(failure.Phase)}
	if agent := safeBootstrapSelector(failure.ProviderID); agent != "" {
		parts = append(parts, "agent="+agent)
	}
	if cause := boundedBootstrapCause(failure.Diagnostic); cause != "" {
		parts = append(parts, cause)
	}
	return strings.Join(parts, "; ")
}

func managedRuntimeStartupFailureDetail(failure *routingerr.ManagedRuntimeStartupError) string {
	parts := []string{"managed runtime startup failed", "code=" + string(failure.Code)}
	if details := boundedBootstrapCause(failure.Details); details != "" {
		parts = append(parts, details)
	}
	return strings.Join(parts, "; ")
}

func boundedBootstrapCause(value string) string {
	return truncateBootstrapDetail(routingerr.Sanitize(value), maxBootstrapCauseDetailBytes)
}

func truncateBootstrapDetail(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func bootstrapFailureDetail(code string) string {
	switch code {
	case models.AgentErrorCauseCodeAuthenticationRequired:
		return "Agent authentication is required."
	case models.AgentErrorCauseCodePermissionDenied:
		return "The required contribution access was denied."
	case models.AgentErrorCauseCodeDestinationInvalid:
		return "The contribution destination is not valid."
	case models.AgentErrorCauseCodeSourceBranchMissing:
		return "The contribution source branch is not available."
	case models.AgentErrorCauseCodeRuntimeCleanupFailed:
		return "The previous runtime could not be stopped. Check executor connectivity, then retry Resume."
	case models.AgentErrorCauseCodeTransportUnavailable:
		return "The contribution service could not be reached."
	case models.AgentErrorCauseCodeTimeout:
		return "The bootstrap operation timed out."
	case models.AgentErrorCauseCodeModelUnavailable:
		return "The requested model is unavailable."
	case models.AgentErrorCauseCodeModelSelectionFailed:
		return "The requested model could not be selected."
	case models.AgentErrorCauseCodePermissionModeFailed:
		return "The requested permission mode could not be applied."
	case models.AgentErrorCauseCodePermissionModeUnconfirmed:
		return "The requested permission mode was not confirmed."
	case models.AgentErrorCauseCodePermissionModeMismatch:
		return "The applied permission mode did not match the request."
	default:
		return "The bootstrap operation could not be completed."
	}
}

func wrapBootstrapFailure(execution *AgentExecution, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrSessionTerminal) {
		return err
	}
	failure := bootstrapFailureFor(execution, err)
	if failure == nil {
		return err
	}
	return failure
}
