package state

import (
	"context"
	"errors"

	"github.com/jmoiron/sqlx"
)

// ReportChangeRequestWithReceipt commits the exact command receipt and the
// existing host-qualified ledger row in one transaction.
func (s *ChangeRequestStore) ReportChangeRequestWithReceipt(
	ctx context.Context, record ExactChangeRequestRecord, commands *CommandStore,
	operationID, resultStatus string,
) (ExactChangeRequestRecord, CommandRecord, error) {
	if commands == nil || commands.db != s.db {
		return ExactChangeRequestRecord{}, CommandRecord{}, errors.New("plugin task change request: command store is unavailable")
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return ExactChangeRequestRecord{}, CommandRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	stored, _, err := s.reportExactChangeRequestTx(ctx, tx, record)
	if err != nil {
		return ExactChangeRequestRecord{}, CommandRecord{}, err
	}
	completed, err := completeCommandInTx(ctx, tx, operationID, resultStatus, "", stored.ID, ChangeRequestResourceVersion(stored), "")
	if err != nil {
		return ExactChangeRequestRecord{}, CommandRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExactChangeRequestRecord{}, CommandRecord{}, err
	}
	return stored, completed, nil
}

func validateChangeRequestIdentity(installationID, workspaceID, providerID, repositoryID string, number int64) error {
	if installationID == "" || workspaceID == "" || providerID == "" || repositoryID == "" || number <= 0 {
		return errors.New("plugin task change request: required identity fields are missing")
	}
	return nil
}

// removeExactChangeRequestTx resolves the public host-less identity only when
// it names one native object. A second host is an ambiguity, never permission
// to delete both objects or guess which row the caller meant.
func removeExactChangeRequestTx(ctx context.Context, tx *sqlx.Tx, installationID, workspaceID, providerID, repositoryID string, number int64) error {
	if err := validateChangeRequestIdentity(installationID, workspaceID, providerID, repositoryID, number); err != nil {
		return err
	}
	rows, err := tx.QueryxContext(ctx, tx.Rebind(`SELECT id FROM plugin_task_change_requests
		WHERE installation_id = ? AND workspace_id = ? AND provider_id = ? AND repository_id = ? AND number = ?
		ORDER BY id LIMIT 2`), installationID, workspaceID, providerID, repositoryID, number)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	readErr := rows.Err()
	_ = rows.Close()
	if readErr != nil {
		return readErr
	}
	if len(ids) > 1 {
		return ErrChangeRequestAmbiguous
	}
	if len(ids) == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, tx.Rebind(`DELETE FROM plugin_task_change_requests
		WHERE id = ? AND installation_id = ? AND workspace_id = ?`), ids[0], installationID, workspaceID)
	return err
}

func (s *ChangeRequestStore) RemoveExactChangeRequest(ctx context.Context, installationID, workspaceID, providerID, repositoryID string, number int64) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := removeExactChangeRequestTx(ctx, tx, installationID, workspaceID, providerID, repositoryID, number); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveChangeRequestWithReceipt completes the command with the one-object
// removal; rollback preserves both the ledger and receipt on any failure.
func (s *ChangeRequestStore) RemoveChangeRequestWithReceipt(
	ctx context.Context, installationID, workspaceID, providerID, repositoryID string,
	number int64, commands *CommandStore, operationID, resultStatus string,
) (CommandRecord, error) {
	if commands == nil || commands.db != s.db {
		return CommandRecord{}, errors.New("plugin task change request: command store is unavailable")
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return CommandRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := removeExactChangeRequestTx(ctx, tx, installationID, workspaceID, providerID, repositoryID, number); err != nil {
		return CommandRecord{}, err
	}
	completed, err := completeCommandInTx(ctx, tx, operationID, resultStatus, "", "", "", "")
	if err != nil {
		return CommandRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommandRecord{}, err
	}
	return completed, nil
}
