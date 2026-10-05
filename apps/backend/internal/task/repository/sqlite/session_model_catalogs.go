package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"

	"github.com/kandev/kandev/internal/db/dialect"
	"github.com/kandev/kandev/internal/task/models"
)

// sessionModelCatalogLockNamespace scopes the PostgreSQL advisory lock so this
// catalog sequence cannot collide with another read-modify-write invariant that
// hashes a different namespace.
const sessionModelCatalogLockNamespace = "session-model-catalog:"

// sessionModelCatalogCacheLimit bounds the in-process catalog cache. One
// provider catalog is tens to hundreds of KiB, so an unbounded map would trade
// the metadata savings this store exists for in memory. Exceeding the bound
// drops the whole cache rather than tracking recency: catalogs are immutable
// once written and cheap to re-read, so a miss only costs one indexed query.
const sessionModelCatalogCacheLimit = 64

// StoreSessionModelCatalog persists one provider catalog under an explicit
// runtime identity and returns the revision to reference it by.
//
// Content decides the revision, not time: an unchanged catalog reuses the
// current revision, and a changed one appends a new revision. An existing
// revision is never rewritten, so a session that already references a revision
// keeps exactly the catalog it observed after the provider changes its list for
// other sessions. An empty key means the caller could not name the runtime
// identity, so the caller must keep the catalog inline instead.
//
// Reading the current revision, assigning the next one, and inserting must be
// one atomic operation: a caller that returned a reference whose stored content
// was some other catalog's would resolve a model list the provider never
// reported. The whole sequence therefore runs on the writer inside a single
// transaction, serialized per catalog key by a process-local lock and, on
// PostgreSQL, by a transaction-scoped advisory lock because its reader pool is
// separate. SQLite's single-writer pool already serializes the transaction.
//
// The stored content is read back and compared before the reference is
// returned, so a revision another writer claimed first can never be reported as
// holding this catalog.
func (r *Repository) StoreSessionModelCatalog(
	ctx context.Context,
	key string,
	catalog models.SessionModelCatalog,
) (models.SessionModelCatalogRef, bool, error) {
	if key == "" {
		return models.SessionModelCatalogRef{}, false, nil
	}
	rendered, err := models.SessionModelCatalogJSON(catalog)
	if err != nil {
		return models.SessionModelCatalogRef{}, false, err
	}
	unlock := r.lockSessionModelCatalogKey(key)
	defer unlock()

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return models.SessionModelCatalogRef{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	if dialect.IsPostgres(r.db.DriverName()) {
		if _, err := tx.ExecContext(ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			sessionModelCatalogLockNamespace+key,
		); err != nil {
			return models.SessionModelCatalogRef{}, false, err
		}
	}
	ref, stored, err := r.storeSessionModelCatalogInTx(ctx, tx, key, rendered)
	if err != nil {
		return models.SessionModelCatalogRef{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return models.SessionModelCatalogRef{}, false, err
	}
	if stored {
		decoded, ok := models.LoadSessionModelCatalog(rendered)
		if ok {
			r.cacheSessionModelCatalog(ref, decoded)
		}
	}
	return ref, stored, nil
}

// storeSessionModelCatalogInTx reuses the current revision when the content is
// unchanged and otherwise appends the next one. It returns the reference and
// whether this call persisted the content, so a caller can tell a reuse from a
// new revision without re-reading.
func (r *Repository) storeSessionModelCatalogInTx(
	ctx context.Context,
	tx *sqlx.Tx,
	key string,
	rendered string,
) (models.SessionModelCatalogRef, bool, error) {
	current, err := latestSessionModelCatalogTx(ctx, tx, r.db.Rebind, key)
	if err != nil {
		return models.SessionModelCatalogRef{}, false, err
	}
	if current != nil {
		var existing string
		err := tx.QueryRowContext(ctx, r.db.Rebind(`
			SELECT catalog_json FROM session_model_catalogs WHERE id = ?
		`), current.ID()).Scan(&existing)
		switch {
		case err == nil && existing == rendered:
			return *current, false, nil
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return models.SessionModelCatalogRef{}, false, err
		}
	}
	ref := models.SessionModelCatalogRef{Key: key, Revision: 1}
	if current != nil {
		ref.Revision = current.Revision + 1
	}
	if _, err := tx.ExecContext(ctx, r.db.Rebind(`
		INSERT INTO session_model_catalogs
			(id, catalog_key, revision, catalog_json, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`), ref.ID(), ref.Key, ref.Revision, rendered, r.nowUTC()); err != nil {
		return models.SessionModelCatalogRef{}, false, err
	}
	var confirmed string
	if err := tx.QueryRowContext(ctx, r.db.Rebind(`
		SELECT catalog_json FROM session_model_catalogs WHERE id = ?
	`), ref.ID()).Scan(&confirmed); err != nil {
		return models.SessionModelCatalogRef{}, false, err
	}
	if confirmed != rendered {
		// Another writer claimed this revision with different content, so it
		// does not hold this catalog. Reporting it would resolve a model list
		// the provider never advertised; the caller keeps the catalog inline.
		return models.SessionModelCatalogRef{}, false, nil
	}
	return ref, true, nil
}

func latestSessionModelCatalogTx(
	ctx context.Context,
	tx *sqlx.Tx,
	rebind func(string) string,
	key string,
) (*models.SessionModelCatalogRef, error) {
	var ref models.SessionModelCatalogRef
	err := tx.QueryRowContext(ctx, rebind(`
		SELECT catalog_key, revision FROM session_model_catalogs
		WHERE catalog_key = ? ORDER BY revision DESC LIMIT 1
	`), key).Scan(&ref.Key, &ref.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

func (r *Repository) lockSessionModelCatalogKey(key string) func() {
	value, _ := r.sessionModelCatalogLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// LoadSessionModelCatalog returns one stored catalog revision.
func (r *Repository) LoadSessionModelCatalog(
	ctx context.Context,
	ref models.SessionModelCatalogRef,
) (models.SessionModelCatalog, bool, error) {
	if ref.Key == "" {
		return models.SessionModelCatalog{}, false, nil
	}
	if catalog, ok := r.cachedSessionModelCatalog(ref); ok {
		return catalog, true, nil
	}
	var rendered string
	if err := r.ro.QueryRowContext(ctx, r.ro.Rebind(`
		SELECT catalog_json FROM session_model_catalogs WHERE id = ?
	`), ref.ID()).Scan(&rendered); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.SessionModelCatalog{}, false, nil
		}
		return models.SessionModelCatalog{}, false, err
	}
	catalog, ok := models.LoadSessionModelCatalog(rendered)
	if !ok {
		return models.SessionModelCatalog{}, false, nil
	}
	r.cacheSessionModelCatalog(ref, catalog)
	return catalog, true, nil
}

// shareSessionModelCatalog replaces the catalog a snapshot writer supplied with
// a reference to a stored revision, so one provider catalog is written once
// instead of once per session.
//
// The inline snapshot is kept whenever the catalog cannot be shared: an
// uncomposable identity, an empty catalog, a store failure, or a revision another
// writer claimed first. A caller must never lose the provider's model list
// because sharing did not work, and the resolved read path treats an inline row
// exactly as it treated one before.
func (r *Repository) shareSessionModelCatalog(
	ctx context.Context,
	sessionID string,
	value interface{},
) (interface{}, error) {
	snapshot, ok := models.LoadSessionModelsSnapshot(value)
	if !ok {
		return value, nil
	}
	if snapshot.CatalogRef != nil {
		// Already references a stored revision; keep the reference and let the
		// scan resolve it, rather than re-storing the catalog the writer read.
		slim, _ := models.SplitSessionModelCatalog(snapshot, snapshot.CatalogRef)
		return slim, nil
	}
	if len(snapshot.Models) == 0 && len(snapshot.ConfigOptions) == 0 {
		return value, nil
	}
	key, err := r.sessionModelCatalogKey(ctx, sessionID)
	if err != nil || key == "" {
		return value, nil
	}
	// SplitSessionModelCatalog strips the per-option selected value on its own
	// copy; stripping here would mutate the caller's slice and take this
	// session's selection with it.
	_, catalog := models.SplitSessionModelCatalog(snapshot, nil)
	ref, _, err := r.StoreSessionModelCatalog(ctx, key, catalog)
	// A non-empty key means a stored revision holds this exact catalog, whether
	// this call appended it or reused the current one; only a conflict or an
	// error leaves the reference empty, and that is the one case that must keep
	// the catalog inline.
	if err != nil || ref.Key == "" {
		return value, nil
	}
	slim, _ := models.SplitSessionModelCatalog(snapshot, &ref)
	return slim, nil
}

// sessionModelCatalogKey composes the catalog identity from the session's own
// row. The profile triple is the identity the runtime already owns; no
// credential secret or fingerprint contributes to it.
func (r *Repository) sessionModelCatalogKey(ctx context.Context, sessionID string) (string, error) {
	var agentProfileID, executionProfileID, executorProfileID string
	err := r.ro.QueryRowContext(ctx, r.ro.Rebind(`
		SELECT agent_profile_id, execution_profile_id, executor_profile_id
		FROM task_sessions WHERE id = ?
	`), sessionID).Scan(&agentProfileID, &executionProfileID, &executorProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return models.SessionModelCatalogKey(agentProfileID, executionProfileID, executorProfileID), nil
}

func (r *Repository) cachedSessionModelCatalog(
	ref models.SessionModelCatalogRef,
) (models.SessionModelCatalog, bool) {
	r.sessionModelCatalogsMu.RLock()
	defer r.sessionModelCatalogsMu.RUnlock()
	catalog, ok := r.sessionModelCatalogs[ref.ID()]
	return catalog, ok
}

func (r *Repository) cacheSessionModelCatalog(
	ref models.SessionModelCatalogRef,
	catalog models.SessionModelCatalog,
) {
	r.sessionModelCatalogsMu.Lock()
	defer r.sessionModelCatalogsMu.Unlock()
	if r.sessionModelCatalogs == nil {
		r.sessionModelCatalogs = make(map[string]models.SessionModelCatalog)
	}
	if len(r.sessionModelCatalogs) >= sessionModelCatalogCacheLimit {
		r.sessionModelCatalogs = make(map[string]models.SessionModelCatalog)
	}
	r.sessionModelCatalogs[ref.ID()] = catalog
}

// resolveSessionModelCatalog expands a referenced catalog in one metadata map
// so consumers keep seeing the resolved snapshot they always have.
//
// A genuinely absent catalog row degrades to the selection-only snapshot, which
// is the same view a session has when its provider never advertised a catalog.
// A read failure is not that: returning a selection-only snapshot would make a
// database error indistinguishable from an empty catalog, so it is reported and
// the scan fails like any other session read error.
func resolveSessionModelCatalog(
	ctx context.Context,
	r *Repository,
	metadata map[string]interface{},
) error {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[models.SessionMetaKeyACPModelState]
	if !ok {
		return nil
	}
	snapshot, ok := models.LoadSessionModelsSnapshot(raw)
	if !ok || snapshot.CatalogRef == nil || len(snapshot.Models) > 0 {
		return nil
	}
	catalog, found, err := r.LoadSessionModelCatalog(ctx, *snapshot.CatalogRef)
	if err != nil {
		return fmt.Errorf("resolve session model catalog %s: %w", snapshot.CatalogRef.ID(), err)
	}
	if !found {
		return nil
	}
	metadata[models.SessionMetaKeyACPModelState] = models.ApplySessionModelCatalog(snapshot, catalog)
	return nil
}

// resolveSessionModelCatalogs is the batched form used by session scans.
func resolveSessionModelCatalogs(
	ctx context.Context,
	r *Repository,
	sessions []*models.TaskSession,
) error {
	for _, session := range sessions {
		if session == nil {
			continue
		}
		if err := resolveSessionModelCatalog(ctx, r, session.Metadata); err != nil {
			return err
		}
	}
	return nil
}
