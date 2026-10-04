package handlers

import (
	"context"
	"errors"
	"expvar"
	"fmt"
	"regexp"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/common/constants"
	"github.com/kandev/kandev/internal/task/models"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// Send-operation request parameters, response receipt keys, and the stable
// conflict code. These are wire contracts, not internal names, so they are
// collected here rather than spelled inline at each use.
const (
	sendOperationIDParam    = "operation_id"
	sendOperationIDKey      = "operation_id"
	sendOperationStateKey   = "operation_state"
	sendOperationMessageKey = "message_id"
	sendOperationQueuedKey  = "queued_entry_id"
	sendOperationRetryKey   = "retry_safe"

	// sendOperationErrorConflict is the stable code for a replay whose
	// delivery-determining request fields differ from the ones the identity was
	// claimed with. It never carries stored content: the caller learns only that
	// the identity is spoken for, never what it was used for.
	sendOperationErrorConflict = "operation_id_conflict"
)

// sendOperationIDPattern bounds a caller-supplied identity. It is untrusted
// input that reaches a unique index, a stored row, and a metric-free log line,
// so length and charset are checked before anything is claimed.
var sendOperationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// sendOperationMetric counts claims by closed-set outcome and delivery status.
// Identities, tasks, sessions, and prompts are never labels: logs carry those,
// metrics carry only the shape of the decision. Exposed through stdlib's
// /debug/vars handler in dev mode, like the watcher throttle counters.
var sendOperationMetric = expvar.NewMap("task_message_send_operation_total")

// Closed-set outcome values for sendOperationMetric.
const (
	sendOperationOutcomeClaimed          = "claimed"
	sendOperationOutcomeReplayed         = "replayed"
	sendOperationOutcomeConflictRejected = "conflict_rejected"
)

// SendOperationStore is the durable side of the operation-identity contract:
// claim before the first delivery side effect, read a claimed identity back,
// and settle the delivery outcome once. Implemented by the task repository;
// kept narrow so the handler can be exercised without SQLite.
type SendOperationStore interface {
	ClaimSendOperation(ctx context.Context, operation *models.TaskMessageSendOperation) error
	GetSendOperation(ctx context.Context, senderTaskID, senderSessionID, operationID string) (*models.TaskMessageSendOperation, error)
	SettleSendOperation(ctx context.Context, senderTaskID, senderSessionID, operationID, state, deliveryStatus, messageID, queuedEntryID, failureCode string) (bool, error)
}

// SetSendOperationStore wires the durable operation store used by
// message_task_kandev and get_task_message_operation_kandev. Without it the
// operation-identity parameters are inert: a send carrying an identity behaves
// exactly as an identity-free send rather than half-claiming one.
func (h *Handlers) SetSendOperationStore(store SendOperationStore) {
	h.sendOperationStore = store
}

// SetSendOperationRetention wires the bounded retention pass. The wiring site
// wraps the store's prune call in maintenance admission, so retention can never
// block or fail a live send: a busy lease simply defers the batch.
func (h *Handlers) SetSendOperationRetention(prune func(ctx context.Context)) {
	h.sendOperationRetention = prune
}

// sendOperationClaimRequest is the delivery-determining part of a send, exactly
// as the caller sent it. It is stored verbatim on the claimed row so a replay
// can be compared by direct value equality.
type sendOperationClaimRequest struct {
	SenderTaskID     string
	SenderSessionID  string
	TargetTaskID     string
	RequestedSession string
	Prompt           string
	DeliveryMode     string
	OperationID      string
}

// claimOutcome is what the pre-dispatch seam decided: either this call owns the
// delivery (operation != nil, response == nil), or it must not deliver at all
// and response is already the reply to send (a replay read or a conflict).
type claimOutcome struct {
	operation *models.TaskMessageSendOperation
	response  *ws.Message
}

// claimSendOperation claims operationID before the first delivery side effect.
// Three outcomes, in order:
//
//   - no identity: nothing is claimed and the caller delivers exactly as today.
//   - first claim: the row exists and commits on its own, so a caller that
//     loses the response still has something to read back.
//   - the identity is already held: a replay. An identical request returns the
//     recorded outcome as a pure read (no second message, queue entry, turn, or
//     interrupt); a differing request is rejected with the stable conflict code
//     and delivers nothing.
//
// A claim failure that is not a conflict happens before any delivery, so the
// caller may retry freely.
func (h *Handlers) claimSendOperation(
	ctx context.Context, msg *ws.Message, req sendOperationClaimRequest,
) (claimOutcome, *ws.Message) {
	if req.OperationID == "" {
		return claimOutcome{}, nil
	}
	if !sendOperationIDPattern.MatchString(req.OperationID) {
		return claimOutcome{}, wsError(msg.ID, msg.Action, ws.ErrorCodeValidation,
			"operation_id must be 1-128 characters of letters, digits, '.', '_', ':' or '-' and start with a letter or digit")
	}
	store := h.sendOperationStore
	if store == nil {
		// No store wired: preserving today's send semantics is strictly better
		// than rejecting a send the caller believes is identified.
		h.logger.Warn("message_task_kandev received an operation_id but no send-operation store is wired; delivering unclaimed",
			zap.String("operation_id", req.OperationID))
		return claimOutcome{}, nil
	}

	operation := &models.TaskMessageSendOperation{
		ID:                    newSendOperationID(req),
		OperationID:           req.OperationID,
		SenderTaskID:          req.SenderTaskID,
		SenderSessionID:       req.SenderSessionID,
		TargetTaskID:          req.TargetTaskID,
		TargetSessionID:       req.RequestedSession,
		RequestedPrompt:       req.Prompt,
		RequestedDeliveryMode: req.DeliveryMode,
		RequestedSessionID:    req.RequestedSession,
	}
	if err := store.ClaimSendOperation(ctx, operation); err != nil {
		if !errors.Is(err, models.ErrSendOperationNotClaimed) {
			h.logger.Error("failed to claim send operation",
				zap.String("operation_id", req.OperationID), zap.Error(err))
			return claimOutcome{}, wsError(msg.ID, msg.Action, ws.ErrorCodeInternalError,
				"failed to record the send operation; nothing was delivered, retry freely")
		}
		return h.replaySendOperation(ctx, msg, req, store)
	}

	h.logger.Info("claimed send operation",
		zap.String("operation_id", req.OperationID),
		zap.String("sender_task_id", req.SenderTaskID),
		zap.String("sender_session_id", req.SenderSessionID))
	recordSendOperationMetric(sendOperationOutcomeClaimed, "")
	h.pruneSendOperationsAsync(ctx)
	return claimOutcome{operation: operation}, nil
}

// replaySendOperation resolves an identity another call already owns. It reads
// the recorded outcome; it never delivers.
func (h *Handlers) replaySendOperation(
	ctx context.Context, msg *ws.Message, req sendOperationClaimRequest, store SendOperationStore,
) (claimOutcome, *ws.Message) {
	existing, err := store.GetSendOperation(ctx, req.SenderTaskID, req.SenderSessionID, req.OperationID)
	if err != nil {
		if errors.Is(err, models.ErrSendOperationNotFound) {
			// The identity was released between the failed claim and this read.
			// Nothing is claimed, so the caller may send again.
			return claimOutcome{}, wsError(msg.ID, msg.Action, ws.ErrorCodeNotFound,
				"operation_id is no longer claimed; send again under this identity")
		}
		h.logger.Error("failed to read claimed send operation",
			zap.String("operation_id", req.OperationID), zap.Error(err))
		return claimOutcome{}, wsError(msg.ID, msg.Action, ws.ErrorCodeInternalError,
			"failed to read the recorded send operation; nothing was delivered, retry freely")
	}
	if !existing.MatchesRequest(req.Prompt, req.DeliveryMode, req.RequestedSession) {
		recordSendOperationMetric(sendOperationOutcomeConflictRejected, "")
		h.logger.Warn("rejected send-operation replay whose request differs from the claim",
			zap.String("operation_id", req.OperationID),
			zap.String("claimed_state", existing.State))
		return claimOutcome{}, wsError(msg.ID, msg.Action, ws.ErrorCodeValidation, sendOperationErrorConflict+
			": this operation_id was already used for a different send; nothing was delivered. Use a fresh operation_id.")
	}

	recordSendOperationMetric(sendOperationOutcomeReplayed, existing.DeliveryStatus)
	h.logger.Info("replayed send operation as a read",
		zap.String("operation_id", req.OperationID),
		zap.String("claimed_state", existing.State))
	response, marshalErr := ws.NewResponse(msg.ID, msg.Action, sendOperationReceipt(existing, true))
	if marshalErr != nil {
		h.logger.Error("failed to encode send-operation replay response",
			zap.String("operation_id", req.OperationID), zap.Error(marshalErr))
		return claimOutcome{}, wsError(msg.ID, msg.Action, ws.ErrorCodeInternalError,
			"failed to report the recorded send outcome; nothing was delivered")
	}
	return claimOutcome{}, response
}

// settleSendOperation records the delivery outcome of the claim this call owns.
// It runs on a context detached from the request and bounded by
// StepHistoryWriteTimeout, so a client disconnect cannot drop the settlement of
// a delivery that already committed. A row left pending by a process that dies
// here is reported by readback as still in flight, which is the safe direction:
// it never claims a send failed while it may have succeeded.
func (h *Handlers) settleSendOperation(
	ctx context.Context, operation *models.TaskMessageSendOperation, result taskMessageDispatchResult, dispatchErr error,
) {
	store := h.sendOperationStore
	if store == nil || operation == nil {
		return
	}
	state := models.SendOperationStateCommitted
	failureCode := ""
	if dispatchErr != nil {
		state = models.SendOperationStateFailed
		failureCode = sendOperationFailureDispatchRejected
	}
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.StepHistoryWriteTimeout)
	defer cancel()
	updated, err := store.SettleSendOperation(
		settleCtx,
		operation.SenderTaskID, operation.SenderSessionID, operation.OperationID,
		state, result.status, result.messageID, result.queuedEntryID, failureCode,
	)
	if err != nil {
		h.logger.Error("failed to settle send operation",
			zap.String("operation_id", operation.OperationID),
			zap.String("state", state), zap.Error(err))
		return
	}
	h.logger.Info("settled send operation",
		zap.String("operation_id", operation.OperationID),
		zap.String("state", state),
		zap.String("delivery_status", result.status),
		zap.Bool("updated", updated))
}

// sendOperationFailureDispatchRejected is the stable failure code for a send
// the dispatch path definitively refused. A readback that reports it says
// resend under a fresh identity; it is the only state where retry_safe is true.
const sendOperationFailureDispatchRejected = "dispatch_rejected"

// sendOperationReceipt is the response shape shared by an identified send's
// success path and by a replay read. task_id, session_id, and status keep their
// existing meaning; the operation fields are additions, each omitted when not
// applicable, so an existing caller or parser is unaffected.
func sendOperationReceipt(operation *models.TaskMessageSendOperation, replayed bool) map[string]interface{} {
	receipt := map[string]interface{}{
		"task_id":             operation.TargetTaskID,
		"session_id":          operation.TargetSessionID,
		stopTaskStatusKey:     operation.DeliveryStatus,
		sendOperationIDKey:    operation.OperationID,
		sendOperationStateKey: operation.State,
	}
	if operation.MessageID != "" {
		receipt[sendOperationMessageKey] = operation.MessageID
	}
	if operation.QueuedEntryID != "" {
		receipt[sendOperationQueuedKey] = operation.QueuedEntryID
	}
	if operation.FailureCode != "" {
		receipt["failure_code"] = operation.FailureCode
	}
	if replayed {
		receipt["replayed"] = true
	}
	return receipt
}

// decorateSendOperationReceipt adds the operation fields to the ordinary
// success payload. The delivered artifact's identity is what lets a caller
// correlate the send with what the target actually received.
func decorateSendOperationReceipt(
	payload map[string]interface{}, operation *models.TaskMessageSendOperation, result taskMessageDispatchResult,
) {
	if operation == nil {
		return
	}
	payload[sendOperationIDKey] = operation.OperationID
	payload[sendOperationStateKey] = models.SendOperationStateCommitted
	if result.messageID != "" {
		payload[sendOperationMessageKey] = result.messageID
	}
	if result.queuedEntryID != "" {
		payload[sendOperationQueuedKey] = result.queuedEntryID
	}
}

// messageIDOf reads the identity of a recorded user message for the receipt.
// A nil message means the dispatch recorded nothing, which is reported as an
// absent field rather than an empty id.
func messageIDOf(message *models.Message) string {
	if message == nil {
		return ""
	}
	return message.ID
}

// newSendOperationID mints the row's primary key. It is not a security
// boundary: the identity the caller owns is (sender pair, operation_id), which
// the unique index enforces.
func newSendOperationID(req sendOperationClaimRequest) string {
	return fmt.Sprintf("sendop-%s-%s-%s", req.SenderTaskID, req.SenderSessionID, req.OperationID)
}

// recordSendOperationMetric counts one claim decision. deliveryStatus is empty
// for a decision that never reached a delivery (a first claim, whose status is
// only known after dispatch, and a conflict rejection).
func recordSendOperationMetric(outcome, deliveryStatus string) {
	sendOperationMetric.Add(sendOperationMetricKey(outcome, deliveryStatus), 1)
}

// sendOperationMetricKey builds the "k=v;k=v" label key the expvar-based
// /debug/vars metrics use, so a downstream translator reads this counter with
// the same parser as the watcher and routing counters.
func sendOperationMetricKey(outcome, deliveryStatus string) string {
	if deliveryStatus == "" {
		return "outcome=" + outcome
	}
	return "outcome=" + outcome + ";delivery_status=" + deliveryStatus
}

// pruneSendOperationsAsync runs one bounded retention batch off the send path.
// It is detached, best-effort, and never reports failure to the caller: a full
// retention queue delays a cleanup that no live send depends on.
func (h *Handlers) pruneSendOperationsAsync(ctx context.Context) {
	prune := h.sendOperationRetention
	if prune == nil {
		return
	}
	go func() {
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendOperationRetentionTimeout)
		defer cancel()
		prune(settleCtx)
	}()
}

// sendOperationRetentionTimeout bounds the detached retention batch so a wedged
// prune cannot outlive the process's patience.
const sendOperationRetentionTimeout = 30 * time.Second

// validateSendOperationIDParam reports whether an identity is acceptable
// without claiming anything. Exposed for the readback tool, which takes a
// caller-supplied identity and must reject a malformed one before any read.
func validateSendOperationIDParam(operationID string) error {
	if operationID == "" {
		return errors.New("operation_id is required")
	}
	if !sendOperationIDPattern.MatchString(operationID) {
		return errors.New("operation_id must be 1-128 characters of letters, digits, '.', '_', ':' or '-' and start with a letter or digit")
	}
	return nil
}
