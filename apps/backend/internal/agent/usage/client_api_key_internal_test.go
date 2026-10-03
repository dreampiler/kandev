package usage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeOpenCodeAuth(t *testing.T, entries map[string]string) string {
	t.Helper()
	root := make(map[string]any, len(entries))
	for provider, key := range entries {
		root[provider] = map[string]string{"type": "api", "key": key}
	}
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// serveJSON answers with body only when the expected bearer key is presented.
func serveJSON(t *testing.T, wantKey string, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const openCodeGoUsageBody = `{"usage":{
	"rolling":{"status":"ok","percent":0,"resetsAt":"2026-10-03T17:34:15.000Z"},
	"weekly":{"status":"rate-limited","percent":100,"resetsAt":"2026-10-05T00:00:00.000Z"},
	"monthly":{"status":"ok","percent":50,"resetsAt":"2026-10-30T04:47:16.000Z"}}}`

func TestOpenCodeGoFetchUsageMapsPlanWindows(t *testing.T) {
	srv := serveJSON(t, "go-key", openCodeGoUsageBody)
	client := NewOpenCodeGoUsageClient(writeOpenCodeAuth(t, map[string]string{"opencode-go": "go-key"}))
	client.usageURL = srv.URL

	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if len(usage.Windows) != 3 {
		t.Fatalf("windows = %d, want rolling, weekly and monthly", len(usage.Windows))
	}
	rolling, weekly, monthly := usage.Windows[0], usage.Windows[1], usage.Windows[2]
	if rolling.DurationSeconds != 5*3600 || !rolling.StartAt.Equal(rolling.ResetAt.Add(-5*time.Hour)) {
		t.Fatalf("rolling = %#v, want a five-hour window ending at its reset", rolling)
	}
	if !weekly.LimitReached || weekly.UtilizationPct != 100 {
		t.Fatalf("weekly = %#v, want the provider's rate-limited state kept", weekly)
	}
	wantStart := time.Date(2026, 9, 30, 4, 47, 16, 0, time.UTC)
	if !monthly.StartAt.Equal(wantStart) || monthly.LimitReached {
		t.Fatalf("monthly start = %v, want one calendar month before the reset", monthly.StartAt)
	}
	for _, window := range usage.Windows {
		if window.Scope != WindowScopePaidModels {
			t.Fatalf("window %q scope = %q, want the plan's paid models", window.Label, window.Scope)
		}
	}
}

func TestOpenCodeGoFallsBackToTheZenKeyOfTheSameAccount(t *testing.T) {
	srv := serveJSON(t, "zen-key", openCodeGoUsageBody)
	client := NewOpenCodeGoUsageClient(writeOpenCodeAuth(t, map[string]string{"opencode": "zen-key"}))
	client.usageURL = srv.URL
	if _, err := client.FetchUsage(context.Background()); err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
}

func TestAPIKeyClientsClassifyFailuresWithoutTheBody(t *testing.T) {
	srv := serveJSON(t, "right-key", openCodeGoUsageBody)
	client := NewOpenCodeGoUsageClient(writeOpenCodeAuth(t, map[string]string{"opencode-go": "wrong-key"}))
	client.usageURL = srv.URL
	_, err := client.FetchUsage(context.Background())
	if reason, status := FailureOf(err); reason != FailureUnauthorized || status != http.StatusUnauthorized {
		t.Fatalf("failure = %q/%d, want unauthorized/401", reason, status)
	}

	missing := NewOpenCodeGoUsageClient(filepath.Join(t.TempDir(), "absent.json"))
	_, err = missing.FetchUsage(context.Background())
	if reason, _ := FailureOf(err); reason != FailureCredentialMissing {
		t.Fatalf("failure = %q, want credential_missing", reason)
	}
	var fetchErr *FetchError
	if !errors.As(err, &fetchErr) || fetchErr.Error() != "opencode-go usage: credential_missing" {
		t.Fatalf("error text = %q, want only the provider and reason", err)
	}
}

func TestOpenRouterDailyFreeQuotaIsAnAccountWideUTCDay(t *testing.T) {
	body := `{"data":{"is_free_tier":false,"free_model_daily_requests":{"used":16,"limit":1000,"remaining":984}}}`
	srv := serveJSON(t, "or-key", body)
	client := NewOpenRouterUsageClient(writeOpenCodeAuth(t, map[string]string{"openrouter": "or-key"}), 0)
	client.keyURL = srv.URL
	now := time.Date(2026, 10, 3, 14, 30, 0, 0, time.FixedZone("KST", 9*3600))
	client.now = func() time.Time { return now }

	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if usage.Plan != "credits" || len(usage.Windows) != 1 {
		t.Fatalf("usage = %#v, want one daily window on a credits account", usage)
	}
	window := usage.Windows[0]
	wantStart := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	if !window.StartAt.Equal(wantStart) || !window.ResetAt.Equal(wantStart.Add(24*time.Hour)) {
		t.Fatalf("window = %v..%v, want the current UTC day", window.StartAt, window.ResetAt)
	}
	if window.UtilizationPct != 1.6 || window.Scope != WindowScopeFreeModels {
		t.Fatalf("window = %#v, want 1.6%% of the free-model quota", window)
	}
}

func TestOpenRouterWithoutTheCounterHasNoWindow(t *testing.T) {
	usage := parseOpenRouterUsage(openRouterKeyResponse{}, time.Now(), OpenRouterFreeDailyRequests)
	if len(usage.Windows) != 0 {
		t.Fatalf("windows = %#v, want none rather than a guessed limit", usage.Windows)
	}
}

func TestLLMGatewayDevPlanWindows(t *testing.T) {
	body := `{"data":{"devPlan":"max","devPlanCreditsUsed":"537.00","devPlanCreditsLimit":"537",
		"devPlanCreditsRemaining":"0.00","devPlanPremiumWeeklyLimit":"96.66",
		"devPlanPremiumCreditsUsed":"48.33","devPlanPremiumWeekResetsAt":"2026-10-05T15:05:06.572Z"}}`
	srv := serveJSON(t, "lg-key", body)
	client := NewLLMGatewayUsageClient(writeOpenCodeAuth(t, map[string]string{"llmgateway": "lg-key"}))
	client.keyURL = srv.URL

	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if len(usage.Windows) != 2 {
		t.Fatalf("windows = %#v, want monthly credits and the premium week", usage.Windows)
	}
	monthly, premium := usage.Windows[0], usage.Windows[1]
	if !monthly.LimitReached || monthly.UtilizationPct != 100 || !monthly.ResetAt.IsZero() {
		t.Fatalf("monthly = %#v, want an exhausted window without an invented reset", monthly)
	}
	if monthly.Scope != WindowScopeAllModels {
		t.Fatalf("monthly scope = %q, want every model", monthly.Scope)
	}
	if premium.Scope != WindowScopePremiumModels || premium.UtilizationPct != 50 || premium.DurationSeconds != 7*24*3600 {
		t.Fatalf("premium = %#v, want half of a seven-day premium cap", premium)
	}
}

func TestLLMGatewayWithoutADevPlanHasNoWindow(t *testing.T) {
	var raw llmGatewayKeyResponse
	if err := json.Unmarshal([]byte(`{"data":{"devPlan":"none","devPlanCreditsLimit":0}}`), &raw); err != nil {
		t.Fatal(err)
	}
	if usage := parseLLMGatewayUsage(raw, time.Now()); len(usage.Windows) != 0 {
		t.Fatalf("windows = %#v, want none", usage.Windows)
	}
}

func TestClaudeOAuthTokenClientUsesTheTokenWithoutACredentialsFile(t *testing.T) {
	srv := serveJSON(t, "env-token", `{"five_hour":{"utilization":12,"resets_at":"2026-10-03T18:00:00Z"}}`)
	client := NewClaudeUsageClientWithOAuthToken("env-token")
	client.usageURL = srv.URL
	if !client.HasSubscriptionCredentials() {
		t.Fatal("a configured OAuth token is a subscription credential")
	}
	usage, err := client.FetchUsage(context.Background())
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if len(usage.Windows) != 1 || usage.Windows[0].UtilizationPct != 12 {
		t.Fatalf("windows = %#v, want the five-hour reading", usage.Windows)
	}
}

func TestClaudeScopeRefusalIsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"token lacks scope"}`))
	}))
	defer srv.Close()
	client := NewClaudeUsageClientWithOAuthToken("env-token")
	client.usageURL = srv.URL
	_, err := client.FetchUsage(context.Background())
	if reason, status := FailureOf(err); reason != FailureUnauthorized || status != http.StatusForbidden {
		t.Fatalf("failure = %q/%d, want unauthorized/403", reason, status)
	}
}

func TestOpenRouterConfiguredAllowanceWinsOverTheProviderLimit(t *testing.T) {
	var raw openRouterKeyResponse
	body := `{"data":{"is_free_tier":true,"free_model_daily_requests":{"used":40,"limit":50}}}`
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatal(err)
	}
	usage := parseOpenRouterUsage(raw, time.Now(), OpenRouterFreeDailyRequests)
	if len(usage.Windows) != 1 || usage.Windows[0].UtilizationPct != 4 || usage.Windows[0].LimitReached {
		t.Fatalf("windows = %#v, want 40 of the configured 1000 requests", usage.Windows)
	}
}
