package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/settings/controller"
	"github.com/kandev/kandev/internal/common/httpmw"
)

type fakeProviderLimitService struct {
	updates []controller.UpdateProviderLimitRequest
}

func (s *fakeProviderLimitService) ListProviderLimits(context.Context) ([]controller.ProviderLimitDTO, error) {
	return []controller.ProviderLimitDTO{{Provider: "opencode-go", ProfileCount: 8, ModelScoped: true}}, nil
}

func (s *fakeProviderLimitService) UpdateProviderLimit(
	_ context.Context,
	provider string,
	request controller.UpdateProviderLimitRequest,
) (*controller.ProviderLimitDTO, error) {
	if provider == "bad provider" {
		return nil, fmt.Errorf("%w: provider", controller.ErrInvalidProviderLimit)
	}
	s.updates = append(s.updates, request)
	return &controller.ProviderLimitDTO{Provider: provider, BlockUntil: request.BlockUntil}, nil
}

func TestProviderLimitRoutesListAndUpdate(t *testing.T) {
	router, ctrl, _ := newSettingsHarness(t, newFakeSettingsRepo(), nil)
	service := &fakeProviderLimitService{}
	ctrl.SetProviderLimitService(service)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/provider-limits", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"provider":"opencode-go"`) {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}

	body := `{"monthly_reset_at":null,"monthly_reset_timezone":"","block_until":"2026-10-07T14:20:00Z"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/provider-limits/llmgateway", strings.NewReader(body))
	request.Header.Set(httpmw.InterimSettingsInterlockHeader, "test-interlock")
	update := httptest.NewRecorder()
	router.ServeHTTP(update, request)
	if update.Code != http.StatusOK {
		t.Fatalf("update = %d %s", update.Code, update.Body.String())
	}
	var saved controller.ProviderLimitDTO
	if err := json.Unmarshal(update.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	want := time.Date(2026, 10, 7, 14, 20, 0, 0, time.UTC)
	if saved.Provider != "llmgateway" || saved.BlockUntil == nil || !saved.BlockUntil.Equal(want) {
		t.Fatalf("saved = %#v", saved)
	}
	if len(service.updates) != 1 || service.updates[0].MonthlyResetAt != nil {
		t.Fatalf("updates = %#v", service.updates)
	}
}

func TestProviderLimitUpdateRequiresInterlockAndRejectsInvalidInput(t *testing.T) {
	router, ctrl, _ := newSettingsHarness(t, newFakeSettingsRepo(), nil)
	service := &fakeProviderLimitService{}
	ctrl.SetProviderLimitService(service)

	locked := httptest.NewRecorder()
	router.ServeHTTP(locked, httptest.NewRequest(http.MethodPut, "/api/v1/provider-limits/opencode-go",
		strings.NewReader(`{}`)))
	if locked.Code != http.StatusForbidden || len(service.updates) != 0 {
		t.Fatalf("update without interlock = %d, updates %d", locked.Code, len(service.updates))
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/provider-limits/bad%20provider", strings.NewReader(`{}`))
	request.Header.Set(httpmw.InterimSettingsInterlockHeader, "test-interlock")
	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid update = %d %s", invalid.Code, invalid.Body.String())
	}
}

func TestProviderLimitRoutesReportUnwiredService(t *testing.T) {
	router, _, _ := newSettingsHarness(t, newFakeSettingsRepo(), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/provider-limits", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired list = %d", response.Code)
	}
}
