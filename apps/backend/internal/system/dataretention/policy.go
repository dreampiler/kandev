package dataretention

import (
	"errors"
	"time"
)

// Age is a retention window expressed in whole days. Both approved targets are
// day-granular: 30 days of archived history and 7 days of finished cleanup.
type Age struct {
	Days int `json:"days"`
}

const maxAgeDays = 3650

func (a Age) Validate() error {
	if a.Days < 1 || a.Days > maxAgeDays {
		return errors.New("invalid_age")
	}
	return nil
}

// Cutoff returns the oldest timestamp a pass may touch. Equal timestamps are
// retained, so a row exactly at the boundary survives this pass.
func (a Age) Cutoff(now time.Time) time.Time {
	return now.UTC().AddDate(0, 0, -a.Days)
}

const (
	defaultArchivedDays = 30
	defaultCleanupDays  = 7
)

// Policy is the administrator's saved choice for both approved targets. The two
// ages are independent so one can be shortened without touching the other, and
// Revision makes a stale concurrent save a conflict rather than an overwrite.
type Policy struct {
	Enabled     bool  `json:"enabled"`
	ArchivedAge Age   `json:"archived_age"`
	CleanupAge  Age   `json:"cleanup_age"`
	Revision    int64 `json:"revision"`
}

func DefaultPolicy() Policy {
	return Policy{
		ArchivedAge: Age{Days: defaultArchivedDays},
		CleanupAge:  Age{Days: defaultCleanupDays},
	}
}
