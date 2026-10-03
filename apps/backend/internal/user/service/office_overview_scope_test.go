package service

import (
	"context"
	"testing"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/user/dto"
	"github.com/kandev/kandev/internal/user/models"
	"go.uber.org/zap"
)

func TestOfficeOverviewScopePatchValidationAndPublication(t *testing.T) {
	settings := &models.UserSettings{}
	if got := dto.FromUserSettings(settings).OfficeOverviewScope; got != models.OfficeOverviewScopeOffice {
		t.Fatalf("unset scope = %q, want office", got)
	}
	reachable := models.OfficeOverviewScopeReachable
	if err := applyBasicSettings(settings, &UpdateUserSettingsRequest{OfficeOverviewScope: &reachable}); err != nil {
		t.Fatal(err)
	}
	if settings.OfficeOverviewScope != models.OfficeOverviewScopeReachable {
		t.Fatalf("scope = %q, want reachable", settings.OfficeOverviewScope)
	}
	if err := applyBasicSettings(settings, &UpdateUserSettingsRequest{}); err != nil {
		t.Fatal(err)
	}
	if settings.OfficeOverviewScope != models.OfficeOverviewScopeReachable {
		t.Fatal("an absent field must leave the scope unchanged")
	}
	org := "org"
	if err := applyBasicSettings(settings, &UpdateUserSettingsRequest{OfficeOverviewScope: &org}); err == nil {
		t.Fatal("a scope outside office/reachable must be rejected")
	}

	log, err := logger.NewFromZap(zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	bus := &recordingEventBus{}
	svc := NewService(&recordingUserRepository{}, bus, log)
	svc.publishUserSettingsEvent(context.Background(), settings)
	data := bus.publishedEvents[0].Data.(map[string]interface{})
	if got := data["office_overview_scope"]; got != models.OfficeOverviewScopeReachable {
		t.Fatalf("event value = %#v, want reachable", got)
	}
}
