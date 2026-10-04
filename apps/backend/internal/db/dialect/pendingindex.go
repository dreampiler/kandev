package dialect

import "fmt"

// PendingIDLookupIndexDDL returns the partial expression index used by
// pending-ID-only clarification reads and claims. The pending ID expression
// is the leading key so the database can isolate one bundle before applying
// its deterministic creation ordering.
func PendingIDLookupIndexDDL(driver, indexName, table string) string {
	pendingID := JSONExtract(driver, "metadata", "pending_id")
	return fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s((%s), created_at, id) WHERE %s IS NOT NULL",
		indexName, table, pendingID, pendingID,
	)
}

// ClarificationSessionIndexDDL returns the partial expression index used by
// the clarification bundle reads (the needs-you Inbox page, its hidden count,
// and the Office overview's answerable-question read). Those queries group
// messages by pending ID under `type = 'clarification_request'`; without this
// index the planner satisfies the session restriction through an index that
// carries no type, so it walks every message of every addressed session and
// discards all but the handful of clarification rows. The partial predicate
// keeps the index to clarification rows only and leaves every other message
// query's plan untouched. messageType is the caller's message-type
// discriminator, so this package carries no domain vocabulary of its own.
func ClarificationSessionIndexDDL(driver, indexName, table, messageType string) string {
	pendingID := JSONExtract(driver, "metadata", "pending_id")
	return fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s(task_session_id, (%s)) WHERE type = '%s'",
		indexName, table, pendingID, messageType,
	)
}
