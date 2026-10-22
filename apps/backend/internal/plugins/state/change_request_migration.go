package state

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type changeRequestColumn struct {
	CID     int            `db:"cid"`
	Name    string         `db:"name"`
	Type    string         `db:"type"`
	NotNull int            `db:"notnull"`
	Default sql.NullString `db:"dflt_value"`
	PK      int            `db:"pk"`
}

type changeRequestIndex struct {
	Seq     int    `db:"seq"`
	Name    string `db:"name"`
	Unique  int    `db:"unique"`
	Origin  string `db:"origin"`
	Partial int    `db:"partial"`
}

// Only the known SQLite legacy table is rebuilt. Copying the explicit columns
// keeps original IDs and all raw timestamp values, including optional empty text.
func migrateLegacyChangeRequests(tx *sqlx.Tx) error {
	var columns []changeRequestColumn
	if err := tx.Select(&columns, `PRAGMA table_info(plugin_task_change_requests)`); err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}
	legacy, err := legacyChangeRequestKey(tx)
	if err != nil || !legacy {
		return err
	}
	if !knownLegacyChangeRequestColumns(columns) {
		return fmt.Errorf("unsupported legacy task change request columns")
	}
	if err := validateLegacyChangeRequestObjects(tx); err != nil {
		return err
	}
	shadow := "plugin_task_change_requests_native_shadow"
	create := strings.SplitN(changeRequestSchema, ";", 2)[0]
	create = strings.Replace(create, "IF NOT EXISTS plugin_task_change_requests", shadow, 1)
	// Retain TEXT affinity so even malformed historical timestamp text is copied
	// byte-for-byte, rather than coerced by SQLite's DATETIME numeric affinity.
	create = strings.ReplaceAll(create, "DATETIME", "TEXT")
	for _, statement := range []string{
		create,
		"INSERT INTO " + shadow + " (" + changeRequestColumns + ") SELECT " + changeRequestColumns + " FROM plugin_task_change_requests",
		`DROP TABLE plugin_task_change_requests`,
		"ALTER TABLE " + shadow + " RENAME TO plugin_task_change_requests",
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("migrate legacy task change requests: %w", err)
		}
	}
	return nil
}

func legacyChangeRequestKey(tx *sqlx.Tx) (bool, error) {
	var indexes []changeRequestIndex
	if err := tx.Select(&indexes, `PRAGMA index_list(plugin_task_change_requests)`); err != nil {
		return false, err
	}
	legacy := false
	for _, index := range indexes {
		if index.Unique == 0 || index.Origin == "pk" {
			continue
		}
		columns, err := changeRequestIndexColumns(tx, index.Name)
		if err != nil {
			return false, err
		}
		switch strings.Join(columns, ",") {
		case "installation_id,workspace_id,provider_id,provider_host,repository_id,number":
			return false, nil
		case "installation_id,workspace_id,provider_id,repository_id,number":
			legacy = index.Partial == 0 && index.Origin == "u"
		default:
			return false, fmt.Errorf("unsupported task change request unique key")
		}
	}
	if !legacy {
		return false, fmt.Errorf("task change request identity key is unavailable")
	}
	return true, nil
}

func changeRequestIndexColumns(tx *sqlx.Tx, name string) ([]string, error) {
	var rows []struct {
		Seq  int            `db:"seqno"`
		CID  int            `db:"cid"`
		Name sql.NullString `db:"name"`
	}
	if err := tx.Select(&rows, `PRAGMA index_info("`+strings.ReplaceAll(name, `"`, `""`)+`")`); err != nil {
		return nil, err
	}
	columns := make([]string, 0, len(rows))
	for _, row := range rows {
		columns = append(columns, row.Name.String)
	}
	return columns, nil
}

func knownLegacyChangeRequestColumns(columns []changeRequestColumn) bool {
	names := strings.Fields(strings.ReplaceAll(changeRequestColumns, ",", " "))
	if len(columns) != len(names) {
		return false
	}
	defaults := map[string]string{"provider_host": "''", "url": "''", "title": "''", "state": "'open'", "head_branch": "''", "base_branch": "''", "merged_at": "''", "closed_at": "''"}
	for i, column := range columns {
		expectedType, notNull, pk := "TEXT", 1, 0
		if column.Name == "number" {
			expectedType = "INTEGER"
		}
		if column.Name == "id" {
			notNull, pk = 0, 1
		}
		value, hasDefault := defaults[column.Name]
		if column.Name != names[i] || strings.ToUpper(column.Type) != expectedType || column.NotNull != notNull || column.PK != pk || column.Default.Valid != hasDefault || column.Default.String != value {
			return false
		}
	}
	return true
}

func validateLegacyChangeRequestObjects(tx *sqlx.Tx) error {
	var inbound int
	if err := tx.Get(&inbound, `SELECT COUNT(*) FROM sqlite_master m
 JOIN pragma_foreign_key_list(m.name) f ON f."table"='plugin_task_change_requests'
 WHERE m.type='table'`); err != nil {
		return err
	}
	if inbound != 0 {
		return fmt.Errorf("legacy task change requests have inbound foreign keys")
	}
	var triggers int
	if err := tx.Get(&triggers, `SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND tbl_name='plugin_task_change_requests'`); err != nil {
		return err
	}
	var definition string
	if err := tx.Get(&definition, `SELECT sql FROM sqlite_master WHERE type='table' AND name='plugin_task_change_requests'`); err != nil {
		return err
	}
	upper := strings.ToUpper(definition)
	if triggers != 0 || strings.Contains(upper, "CHECK") || strings.Contains(upper, "REFERENCES") {
		return fmt.Errorf("unsupported legacy task change request constraints")
	}
	var indexes []changeRequestIndex
	if err := tx.Select(&indexes, `PRAGMA index_list(plugin_task_change_requests)`); err != nil {
		return err
	}
	expected := map[string]string{
		"idx_plugin_task_change_requests_task":   "installation_id,workspace_id,task_id",
		"idx_plugin_task_change_requests_merged": "workspace_id,state,merged_at",
	}
	for _, index := range indexes {
		if index.Origin != "c" {
			continue
		}
		columns, err := changeRequestIndexColumns(tx, index.Name)
		if err != nil {
			return err
		}
		if index.Unique != 0 || index.Partial != 0 || expected[index.Name] != strings.Join(columns, ",") {
			return fmt.Errorf("unsupported legacy task change request index")
		}
	}
	return nil
}
