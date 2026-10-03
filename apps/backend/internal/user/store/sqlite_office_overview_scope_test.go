package store

import (
	"testing"

	"github.com/kandev/kandev/internal/user/models"
)

// TestOfficeOverviewScopeStoredValues pins the default, the round trip, and
// that anything other than "reachable" reads back as the narrower Office
// scope, so a hand-edited row can never widen the overview.
func TestOfficeOverviewScopeStoredValues(t *testing.T) {
	if got := defaultUserSettings(DefaultUserID).OfficeOverviewScope; got != models.OfficeOverviewScopeOffice {
		t.Fatalf("default scope = %q, want office", got)
	}
	raw, err := marshalUserSettingsPayload(&models.UserSettings{OfficeOverviewScope: models.OfficeOverviewScopeReachable})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	settings, err := scanUserSettings(settingsScanner{raw: string(raw)}, DefaultUserID)
	if err != nil || settings.OfficeOverviewScope != models.OfficeOverviewScopeReachable {
		t.Fatalf("round trip scope = %q err = %v, want reachable", settings.OfficeOverviewScope, err)
	}
	for _, stored := range []string{`{}`, `{"office_overview_scope":"org"}`, `{"office_overview_scope":7}`, `{"office_overview_scope":null}`} {
		settings, err := scanUserSettings(settingsScanner{raw: stored}, DefaultUserID)
		if err != nil || settings.OfficeOverviewScope != models.OfficeOverviewScopeOffice {
			t.Fatalf("stored %s: scope = %q err = %v, want office", stored, settings.OfficeOverviewScope, err)
		}
	}
}
