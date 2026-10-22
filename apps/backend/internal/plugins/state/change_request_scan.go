package state

import (
	"fmt"
	"time"
)

// Scan timestamps independently of the column declaration: existing Native
// databases store RFC3339 text, while new schemas and Postgres return time.Time.
func scanChangeRequest(scanner interface{ Scan(...any) error }) (ChangeRequestRecord, error) {
	var row ChangeRequestRecord
	var created, merged, closed, updated any
	err := scanner.Scan(&row.ID, &row.InstallationID, &row.WorkspaceID, &row.TaskID,
		&row.ProviderID, &row.ProviderHost, &row.RepositoryID, &row.Number,
		&row.URL, &row.Title, &row.State, &row.HeadBranch, &row.BaseBranch,
		&created, &merged, &closed, &updated)
	if err != nil {
		return row, err
	}
	row.CreatedAt, err = requiredChangeRequestTimestamp(created)
	if err != nil {
		return row, err
	}
	row.UpdatedAt, err = requiredChangeRequestTimestamp(updated)
	if err != nil {
		return row, err
	}
	row.MergedAt, err = optionalChangeRequestTimestamp(merged)
	if err != nil {
		return row, err
	}
	row.ClosedAt, err = optionalChangeRequestTimestamp(closed)
	return row, err
}

func requiredChangeRequestTimestamp(value any) (time.Time, error) {
	parsed, err := optionalChangeRequestTimestamp(value)
	if err != nil {
		return time.Time{}, err
	}
	if parsed == nil {
		return time.Time{}, fmt.Errorf("missing task change request timestamp")
	}
	return *parsed, nil
}

func optionalChangeRequestTimestamp(value any) (*time.Time, error) {
	switch value := value.(type) {
	case nil:
		return nil, nil
	case time.Time:
		if value.IsZero() {
			return nil, fmt.Errorf("invalid task change request timestamp")
		}
		utc := value.UTC()
		return &utc, nil
	case []byte:
		return optionalChangeRequestTimestamp(string(value))
	case string:
		if value == "" {
			return nil, nil
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return optionalChangeRequestTimestamp(parsed)
			}
		}
	}
	return nil, fmt.Errorf("invalid task change request timestamp")
}
