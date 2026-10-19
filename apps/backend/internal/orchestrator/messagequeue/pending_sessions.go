package messagequeue

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// errPendingSessionListingUnsupported is returned by repositories that cannot
// enumerate sessions with queued rows.
var errPendingSessionListingUnsupported = errors.New("pending session listing is not supported")

type pendingSessionLister interface {
	ListSessionsWithPending(ctx context.Context) ([]string, error)
}

// ListSessionsWithPending returns the sessions that hold at least one queued
// row that is not reserved by an in-flight dispatch.
func (s *Service) ListSessionsWithPending(ctx context.Context) ([]string, error) {
	lister, ok := s.repo.(pendingSessionLister)
	if !ok {
		return nil, errPendingSessionListingUnsupported
	}
	return lister.ListSessionsWithPending(ctx)
}

func (r *memoryRepository) ListSessionsWithPending(_ context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for sessionID, list := range r.entries {
		for _, msg := range list {
			if !msg.IsReservedInFlight() {
				out = append(out, sessionID)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r *sqliteRepository) ListSessionsWithPending(ctx context.Context) ([]string, error) {
	rows, err := r.ro.QueryxContext(ctx, r.ro.Rebind(`
		SELECT q.id, q.session_id, q.task_id, q.position, q.content, q.model, q.plan_mode,
		       q.attachments_json, q.metadata_json, q.queued_at, q.queued_by
		FROM queued_messages q
		INNER JOIN task_sessions s ON s.id = q.session_id
		ORDER BY q.session_id ASC, q.position ASC
	`))
	if err != nil {
		return nil, fmt.Errorf("list sessions with pending: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		msg, scanErr := scanQueuedRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list sessions with pending scan: %w", scanErr)
		}
		if msg.IsReservedInFlight() || (len(out) > 0 && out[len(out)-1] == msg.SessionID) {
			continue
		}
		out = append(out, msg.SessionID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions with pending rows: %w", err)
	}
	return out, nil
}
