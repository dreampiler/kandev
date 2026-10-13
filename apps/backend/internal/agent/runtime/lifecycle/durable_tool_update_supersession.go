package lifecycle

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/task/models"
)

// agentDeliverySupersededToolUpdateCompactor is implemented by the backend
// repository. It clears the inbox bodies of earlier in-progress tool updates
// once a later update for the same tool call is projected, and proves every
// candidate under its own locks. Each hint carries the receive-time hash of a
// row observed here as progress of that tool call; the hash binds it.
type agentDeliverySupersededToolUpdateCompactor interface {
	CompactSupersededAgentDeliveryToolUpdates(
		ctx context.Context,
		superseding *models.AgentDeliveryEvent,
		superseded []models.AgentDeliverySupersededToolUpdate,
	) (int, int64, error)
}

type supersededHint = models.AgentDeliverySupersededToolUpdate

const (
	// maxSupersededToolCallHints bounds the in-memory hints and queued jobs.
	// Losing one only leaves that body for the normal payload grace.
	maxSupersededToolCallHints = 256
	// maxSupersededSequencesPerToolCall bounds the hints kept per tool call
	// for a queued job or for retry after a compaction that did not commit.
	maxSupersededSequencesPerToolCall = 16
)

type toolUpdateSupersessionKey struct {
	streamID   string
	toolCallID string
}

type supersededToolUpdateJob struct {
	compactor   agentDeliverySupersededToolUpdateCompactor
	superseding models.AgentDeliveryEvent
	hints       []supersededHint
}

// toolUpdateSupersession remembers, per stream and tool call, the projected
// in-progress tool_update sequences whose inbox bodies are still retained, and
// queues their compaction once a later update is projected. Compaction runs
// on one tracked background goroutine so the delivery path never waits for it.
// Everything here is a best-effort hint: restarts, eviction and failures fall
// back to normal retention, and the repository re-proves every candidate.
type toolUpdateSupersession struct {
	mu      sync.Mutex
	pending map[toolUpdateSupersessionKey][]supersededHint
	jobs    map[toolUpdateSupersessionKey]*supersededToolUpdateJob
	queue   []toolUpdateSupersessionKey
	running bool
}

// advance returns the hints that the projected update superseding supersedes
// and, when retain is true, records superseding itself as the next hint.
func (s *toolUpdateSupersession) advance(key toolUpdateSupersessionKey, current supersededHint, retain bool) []supersededHint {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.pending[key]
	delete(s.pending, key)
	if retain {
		s.pending = storeSupersessionHint(s.pending, key, []supersededHint{current})
	}
	return previous
}

// restore keeps hints whose compaction did not commit so the next update for
// the same tool call can retry them.
func (s *toolUpdateSupersession) restore(key toolUpdateSupersessionKey, hints []supersededHint) {
	if len(hints) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = storeSupersessionHint(s.pending, key, boundedSupersededHints(hints, s.pending[key]))
}

// schedule merges hints into the queued job for key. The newest projected
// update becomes the job's superseding event; every queued sequence is older.
// It reports whether the caller must start the drain goroutine.
func (s *toolUpdateSupersession) schedule(
	key toolUpdateSupersessionKey,
	compactor agentDeliverySupersededToolUpdateCompactor,
	superseding models.AgentDeliveryEvent,
	hints []supersededHint,
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[key]
	if job == nil {
		if len(s.jobs) >= maxSupersededToolCallHints {
			return false
		}
		if s.jobs == nil {
			s.jobs = make(map[toolUpdateSupersessionKey]*supersededToolUpdateJob)
		}
		job = &supersededToolUpdateJob{}
		s.jobs[key] = job
		s.queue = append(s.queue, key)
	}
	job.compactor = compactor
	job.superseding = superseding
	job.hints = boundedSupersededHints(job.hints, hints)
	if s.running {
		return false
	}
	s.running = true
	return true
}

// next pops the oldest queued job, or marks the drain idle when none is left.
func (s *toolUpdateSupersession) next() (toolUpdateSupersessionKey, *supersededToolUpdateJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) > 0 {
		key := s.queue[0]
		s.queue = s.queue[1:]
		if job := s.jobs[key]; job != nil {
			delete(s.jobs, key)
			return key, job, true
		}
	}
	s.queue = nil
	s.running = false
	return toolUpdateSupersessionKey{}, nil, false
}

func (s *toolUpdateSupersession) abandonDrain() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = nil
	s.queue = nil
	s.running = false
}

func storeSupersessionHint(
	pending map[toolUpdateSupersessionKey][]supersededHint,
	key toolUpdateSupersessionKey,
	hints []supersededHint,
) map[toolUpdateSupersessionKey][]supersededHint {
	if pending == nil {
		pending = make(map[toolUpdateSupersessionKey][]supersededHint)
	}
	if _, exists := pending[key]; !exists && len(pending) >= maxSupersededToolCallHints {
		// Tool calls that never finish would otherwise accumulate. Dropping
		// all hints is safe: those bodies keep the normal grace.
		pending = make(map[toolUpdateSupersessionKey][]supersededHint)
	}
	pending[key] = hints
	return pending
}

func boundedSupersededHints(older, newer []supersededHint) []supersededHint {
	merged := append(append([]supersededHint(nil), older...), newer...)
	if len(merged) > maxSupersededSequencesPerToolCall {
		merged = merged[len(merged)-maxSupersededSequencesPerToolCall:]
	}
	return merged
}

// toolUpdateSupersededByLater reports a progress-only status whose body a
// later update replaces. Final statuses are never hinted, so their bodies keep
// the normal grace.
func toolUpdateSupersededByLater(status string) bool {
	return toolIsExecuting(status) || status == "pending"
}

// compactSupersededToolUpdates runs after a nonterminal tool_update is
// projected. Superseded in-progress bodies carry cumulative tool output, so a
// long command would otherwise keep O(n^2) bytes in the inbox for the payload
// grace. It only queues work; failure never affects delivery, and the bodies
// stay for normal retention.
func (sm *StreamManager) compactSupersededToolUpdates(delivery AgentDeliveryRepository, prepared preparedAgentEvent) {
	durable := prepared.durableEvent
	if durable == nil || durable.Terminal || durable.EventType != streams.EventTypeToolUpdate ||
		durable.StreamID == "" || durable.Sequence <= 0 || durable.ContentHash == "" ||
		prepared.event.ToolCallID == "" {
		return
	}
	compactor, ok := delivery.(agentDeliverySupersededToolUpdateCompactor)
	if !ok {
		return
	}
	key := toolUpdateSupersessionKey{streamID: durable.StreamID, toolCallID: prepared.event.ToolCallID}
	previous := sm.toolUpdateSupersession.advance(
		key, supersededHint{Sequence: durable.Sequence, ContentHash: durable.ContentHash},
		toolUpdateSupersededByLater(prepared.event.ToolStatus),
	)
	if len(previous) == 0 {
		return
	}
	superseding := *durable
	superseding.Payload = nil // the repository proves it by hash; do not pin the body
	if sm.toolUpdateSupersession.schedule(key, compactor, superseding, previous) &&
		!sm.start(sm.drainSupersededToolUpdates) {
		sm.toolUpdateSupersession.abandonDrain()
	}
}

func (sm *StreamManager) drainSupersededToolUpdates() {
	ctx := sm.reconciliationContext
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		key, job, ok := sm.toolUpdateSupersession.next()
		if !ok {
			return
		}
		if ctx.Err() != nil {
			continue
		}
		compacted, cleared, err := job.compactor.CompactSupersededAgentDeliveryToolUpdates(
			ctx, &job.superseding, job.hints,
		)
		if err != nil {
			if ctx.Err() == nil {
				sm.toolUpdateSupersession.restore(key, job.hints)
			}
			sm.logger.Debug("superseded tool update compaction deferred to retention",
				zap.String("stream_id", job.superseding.StreamID),
				zap.Int64("sequence", job.superseding.Sequence),
				zap.Int("hinted", len(job.hints)),
				zap.Error(err))
			continue
		}
		if compacted > 0 {
			sm.logger.Debug("superseded tool update payloads compacted",
				zap.String("stream_id", job.superseding.StreamID),
				zap.Int64("sequence", job.superseding.Sequence),
				zap.Int("rows", compacted),
				zap.Int64("bytes", cleared))
		}
	}
}
