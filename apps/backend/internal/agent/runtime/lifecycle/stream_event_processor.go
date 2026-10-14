package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	agentctl "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"go.uber.org/zap"
)

const (
	streamProjectionBatchBytes = 64 << 10
	streamProjectionBatchWait  = 20 * time.Millisecond
	streamProjectionQueueSize  = 4096

	// streamAdmissionRetryAttempts bounds in-place retries of a durable
	// admission that failed with a retryable deadline, so transient writer
	// contention does not tear down the shared transport mid-handshake.
	streamAdmissionRetryAttempts = 3
	// streamAdmissionRetryBackoff is the drain-aware pause between attempts.
	streamAdmissionRetryBackoff = 200 * time.Millisecond
)

var errStreamEventProcessorQueueFull = fmt.Errorf(
	"%w: durable agent event processor queue is full",
	ErrUncertainPromptDelivery,
)

// streamEventProcessor keeps the transport reader responsive while preserving
// ordered lifecycle delivery. Compatible canonical chunks can accumulate until
// the byte or timer bound and commit through one repository transaction.
type streamEventProcessor struct {
	manager           *StreamManager
	ctx               context.Context
	cancel            context.CancelFunc
	execution         *AgentExecution
	client            *agentctl.Client
	delivery          AgentDeliveryRepository
	startupGeneration uint64
	events            chan agentctl.AgentEvent
	done              chan struct{}
	// stopTransport detaches the updates stream this processor serves. It is
	// set before the stream delivers its first event.
	stopTransport func()
	// detached closes when the transport is gone so a successor generation
	// wait does not hold the disconnect path until its bound.
	detached   chan struct{}
	detachOnce sync.Once

	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

type streamEventBatch struct {
	items   []preparedAgentEvent
	bytes   int
	timer   *time.Timer
	timerCh <-chan time.Time
}

func newStreamEventProcessor(
	manager *StreamManager,
	parent context.Context,
	execution *AgentExecution,
	client *agentctl.Client,
	delivery AgentDeliveryRepository,
	startupGeneration uint64,
) *streamEventProcessor {
	ctx, cancel := context.WithCancel(parent)
	processor := &streamEventProcessor{
		manager:           manager,
		ctx:               ctx,
		cancel:            cancel,
		execution:         execution,
		client:            client,
		delivery:          delivery,
		startupGeneration: startupGeneration,
		events:            make(chan agentctl.AgentEvent, streamProjectionQueueSize),
		done:              make(chan struct{}),
		detached:          make(chan struct{}),
	}
	go processor.run()
	return processor
}

func (p *streamEventProcessor) enqueue(event agentctl.AgentEvent) bool {
	select {
	case p.events <- event:
		return true
	case <-p.ctx.Done():
		return false
	}
}

func (p *streamEventProcessor) close() error {
	p.detachOnce.Do(func() { close(p.detached) })
	p.closeOnce.Do(func() {
		close(p.events)
	})
	<-p.done
	p.cancel()
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

func (p *streamEventProcessor) fail(err error) {
	if err == nil {
		return
	}
	p.errMu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.errMu.Unlock()
	p.cancel()
}

// stop records a processing failure and detaches the transport. A processor
// that only cancelled itself would leave the reader attached: every later event
// is then dropped as "processor unavailable" and the disconnect path that
// reports the failure and recovers the execution never runs.
func (p *streamEventProcessor) stop(err error) {
	p.fail(err)
	if p.stopTransport != nil {
		p.stopTransport()
	}
}

func (p *streamEventProcessor) run() {
	defer close(p.done)
	defer p.cancel()

	batch := &streamEventBatch{}
	for {
		select {
		case event, ok := <-p.events:
			if !ok {
				p.fail(p.flushBatch(batch))
				return
			}
			if err := p.handleEvent(batch, event); err != nil {
				p.stop(err)
				return
			}
		case <-batch.timerCh:
			if err := p.flushBatch(batch); err != nil {
				p.stop(err)
				return
			}
		case <-p.ctx.Done():
			return
		}
	}
}

func (p *streamEventProcessor) handleEvent(batch *streamEventBatch, event agentctl.AgentEvent) error {
	admissionCtx := withDeliveryDetachSignal(p.ctx, p.detached)
	var prepared preparedAgentEvent
	err := retryStreamAdmission(p.ctx, p.manager.sleepOrStop, func(attempt int) {
		p.manager.logger.Warn("retrying durable agent event admission after deadline",
			zap.String("instance_id", p.execution.ID),
			zap.Int("attempt", attempt))
	}, func() error {
		var prepareErr error
		prepared, prepareErr = p.manager.prepareAgentEvent(
			admissionCtx, p.execution, p.client, p.delivery, event,
		)
		return prepareErr
	})
	if err != nil {
		return err
	}
	if prepared.duplicate {
		p.manager.scheduleDurableDeliveryAck(p.execution, p.client, prepared.event)
		return nil
	}
	if !p.batchable(prepared) {
		if err := p.flushBatch(batch); err != nil {
			return err
		}
		return p.manager.processPreparedAgentEvent(
			p.ctx, p.execution, p.client, p.delivery, prepared, p.startupGeneration,
		)
	}
	if p.mustFlushBefore(batch, prepared) {
		if err := p.flushBatch(batch); err != nil {
			return err
		}
	}
	batch.items = append(batch.items, prepared)
	batch.bytes += len(prepared.durableEvent.Payload)
	if len(batch.items) == 1 {
		batch.timer = time.NewTimer(streamProjectionBatchWait)
		batch.timerCh = batch.timer.C
	}
	if batch.bytes >= streamProjectionBatchBytes {
		return p.flushBatch(batch)
	}
	return nil
}

// streamAdmissionRetryable reports whether a durable admission failure is a
// transient deadline worth retrying in place. A cancelled processor or parent
// context is never retried: shutdown keeps its existing stop path, and any
// permanent cause (identity mismatch, corruption, missing checkpoint) still
// fails closed immediately.
func streamAdmissionRetryable(ctx context.Context, err error) bool {
	return ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded)
}

// retryStreamAdmission runs prepare, retrying in place while it returns a
// retryable deadline and the processor is live, up to a bounded number of
// attempts. The same event is re-prepared, so later events are never overtaken
// and delivery order is preserved; sleep is drain-aware and aborts the retry on
// a stop signal. The final prepare error is returned unchanged, so the stop
// path and its log wording are the same as before.
func retryStreamAdmission(
	ctx context.Context,
	sleep func(time.Duration) bool,
	logRetry func(attempt int),
	prepare func() error,
) error {
	err := prepare()
	for attempt := 1; err != nil && attempt < streamAdmissionRetryAttempts; attempt++ {
		if !streamAdmissionRetryable(ctx, err) {
			return err
		}
		if !sleep(streamAdmissionRetryBackoff) {
			return err
		}
		logRetry(attempt)
		err = prepare()
	}
	return err
}

func (p *streamEventProcessor) mustFlushBefore(batch *streamEventBatch, prepared preparedAgentEvent) bool {
	if len(batch.items) == 0 {
		return false
	}
	previous := batch.items[len(batch.items)-1]
	return !compatibleCanonicalBatch(previous, prepared, p.execution) ||
		batch.bytes+len(prepared.durableEvent.Payload) > streamProjectionBatchBytes
}

func (p *streamEventProcessor) flushBatch(batch *streamEventBatch) error {
	if len(batch.items) == 0 {
		return nil
	}
	err := p.manager.projectCanonicalAgentEvents(
		p.ctx, p.execution, batch.items, p.delivery, p.client, p.startupGeneration,
	)
	batch.items = nil
	batch.bytes = 0
	if batch.timer != nil && !batch.timer.Stop() {
		select {
		case <-batch.timer.C:
		default:
		}
	}
	batch.timer = nil
	batch.timerCh = nil
	return err
}

func (p *streamEventProcessor) batchable(prepared preparedAgentEvent) bool {
	if _, ok := p.delivery.(canonicalAgentDeliveryBatchProjector); !ok {
		return false
	}
	return prepared.durableEvent != nil && canonicalAgentDeliveryEvent(prepared.event)
}

func compatibleCanonicalBatch(previous, next preparedAgentEvent, execution *AgentExecution) bool {
	if previous.durableEvent == nil || next.durableEvent == nil {
		return false
	}
	if previous.event.Type != next.event.Type {
		return false
	}
	return canonicalAgentMessageID(execution, previous.event) == canonicalAgentMessageID(execution, next.event)
}

func (sm *StreamManager) processPreparedAgentEvent(
	ctx context.Context,
	execution *AgentExecution,
	client *agentctl.Client,
	delivery AgentDeliveryRepository,
	prepared preparedAgentEvent,
	startupGeneration uint64,
) error {
	if prepared.duplicate {
		if !prepared.skipAcknowledgement {
			sm.scheduleDurableDeliveryAck(execution, client, prepared.event)
		}
		return nil
	}
	if prepared.durableEvent != nil && prepared.durableEvent.Terminal {
		if err := sm.projectDurableAgentDeliveryEventWithEffect(
			ctx, prepared.durableEvent, delivery, prepared.deliveryEffect,
		); err != nil {
			return fmt.Errorf("project durable terminal agent event: %w", err)
		}
		if !prepared.skipAcknowledgement {
			sm.scheduleDurableDeliveryAck(execution, client, prepared.event)
		}
		if !prepared.skipCallback {
			sm.notifyAgentEvent(execution, prepared.event, startupGeneration)
		}
		return nil
	}
	event, canonicalProjected, err := sm.projectCanonicalAgentEvent(
		ctx, execution, prepared.event, prepared.durableEvent, delivery, client,
		!prepared.skipAcknowledgement,
	)
	if err != nil {
		return fmt.Errorf("canonical agent event projection: %w", err)
	}
	if !prepared.skipCallback {
		sm.notifyAgentEvent(execution, event, startupGeneration)
	}
	if prepared.durableEvent == nil || canonicalProjected {
		return nil
	}
	if err := sm.projectDurableAgentDeliveryEventWithEffect(
		ctx, prepared.durableEvent, delivery, prepared.deliveryEffect,
	); err != nil {
		return fmt.Errorf("project durable agent event: %w", err)
	}
	if !prepared.skipAcknowledgement {
		sm.scheduleDurableDeliveryAck(execution, client, event)
	}
	sm.compactSupersededToolUpdates(delivery, prepared)
	return nil
}
