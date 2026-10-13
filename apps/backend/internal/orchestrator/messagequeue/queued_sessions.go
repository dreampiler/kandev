package messagequeue

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// QueuedSessionRef names one session that still holds visible queue rows.
type QueuedSessionRef struct {
	TaskID    string
	SessionID string
}

// queuedSessionLister is implemented by repositories that can enumerate the
// sessions owning queue rows without loading every row's payload.
type queuedSessionLister interface {
	ListQueuedSessions(ctx context.Context) ([]QueuedSessionRef, error)
}

// ListQueuedSessions returns every session that still holds queue rows. Startup
// uses it to give queued work a delivery trigger after a restart: the ordinary
// triggers (turn end, boot ready, enqueue) never fire for a session that was
// idle when the backend stopped.
func (s *Service) ListQueuedSessions(ctx context.Context) ([]QueuedSessionRef, error) {
	lister, ok := s.repo.(queuedSessionLister)
	if !ok {
		return nil, errors.New("queued session listing unavailable")
	}
	return lister.ListQueuedSessions(ctx)
}

// ListQueuedSessions returns the distinct sessions owning queue rows.
func (r *sqliteRepository) ListQueuedSessions(ctx context.Context) ([]QueuedSessionRef, error) {
	rows, err := r.ro.QueryxContext(ctx, r.ro.Rebind(`
		SELECT DISTINCT session_id, task_id
		FROM queued_messages
		ORDER BY session_id ASC
	`))
	if err != nil {
		return nil, fmt.Errorf("list queued sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []QueuedSessionRef
	for rows.Next() {
		var ref QueuedSessionRef
		if scanErr := rows.Scan(&ref.SessionID, &ref.TaskID); scanErr != nil {
			return nil, fmt.Errorf("scan queued session: %w", scanErr)
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list queued session rows: %w", err)
	}
	return out, nil
}

// ListQueuedSessions returns the distinct sessions owning queue rows.
func (r *memoryRepository) ListQueuedSessions(_ context.Context) ([]QueuedSessionRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]QueuedSessionRef, 0, len(r.entries))
	for sessionID, list := range r.entries {
		if len(list) == 0 {
			continue
		}
		out = append(out, QueuedSessionRef{TaskID: list[0].TaskID, SessionID: sessionID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out, nil
}
