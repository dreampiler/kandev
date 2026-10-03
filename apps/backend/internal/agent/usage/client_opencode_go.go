package usage

import (
	"context"
	"net/http"
	"strings"
	"time"
)

const (
	openCodeGoUsageURL = "https://opencode.ai/zen/go/v1/usage"
	openCodeGoProvider = "opencode-go"

	openCodeGoStatusOK = "ok"
)

// OpenCodeGoUsageClient reads the OpenCode Go subscription's account windows.
// The rolling, weekly and monthly limits are account-wide spend limits on the
// plan's paid models, so every Go model shares the same reading.
type OpenCodeGoUsageClient struct {
	key        OpenCodeKeySource
	usageURL   string
	httpClient *http.Client
}

// NewOpenCodeGoUsageClient creates a client that authenticates with the Go key
// in the OpenCode auth file at authPath.
func NewOpenCodeGoUsageClient(authPath string) *OpenCodeGoUsageClient {
	return &OpenCodeGoUsageClient{
		key:        OpenCodeKeySource{Path: authPath, Providers: []string{"opencode-go", "opencode"}},
		usageURL:   openCodeGoUsageURL,
		httpClient: &http.Client{Timeout: apiKeyUsageHTTPTimeout},
	}
}

type openCodeGoWindow struct {
	Status   string  `json:"status"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt"`
}

type openCodeGoUsageResponse struct {
	Usage struct {
		Rolling *openCodeGoWindow `json:"rolling"`
		Weekly  *openCodeGoWindow `json:"weekly"`
		Monthly *openCodeGoWindow `json:"monthly"`
	} `json:"usage"`
}

// FetchUsage implements ProviderUsageClient.
func (c *OpenCodeGoUsageClient) FetchUsage(ctx context.Context) (*ProviderUsage, error) {
	apiKey, err := c.key.APIKey(openCodeGoProvider)
	if err != nil {
		return nil, err
	}
	var raw openCodeGoUsageResponse
	if err := getBearerJSON(ctx, c.httpClient, openCodeGoProvider, c.usageURL, apiKey, &raw); err != nil {
		return nil, err
	}
	now := time.Now()
	return &ProviderUsage{
		Provider:  openCodeGoProvider,
		Plan:      "go",
		Windows:   openCodeGoWindows(raw),
		FetchedAt: now,
	}, nil
}

// openCodeGoWindows maps the three plan windows. The rolling window is five
// hours and the weekly one seven days; the monthly window follows the
// subscription's billing month, so its start is one calendar month before the
// reported reset rather than a fixed thirty days.
func openCodeGoWindows(raw openCodeGoUsageResponse) []UtilizationWindow {
	windows := make([]UtilizationWindow, 0, 3)
	fixed := []struct {
		label    string
		raw      *openCodeGoWindow
		duration time.Duration
	}{
		{claudeLabel5Hour, raw.Usage.Rolling, 5 * time.Hour},
		{claudeLabel7Day, raw.Usage.Weekly, 7 * 24 * time.Hour},
	}
	for _, entry := range fixed {
		if entry.raw == nil {
			continue
		}
		window := openCodeGoUtilization(entry.label, entry.raw)
		if !window.ResetAt.IsZero() {
			window.StartAt = window.ResetAt.Add(-entry.duration)
			window.DurationSeconds = int64(entry.duration / time.Second)
		}
		windows = append(windows, window)
	}
	if raw.Usage.Monthly != nil {
		window := openCodeGoUtilization("monthly", raw.Usage.Monthly)
		if !window.ResetAt.IsZero() {
			window.StartAt = window.ResetAt.AddDate(0, -1, 0)
			window.DurationSeconds = int64(window.ResetAt.Sub(window.StartAt) / time.Second)
		}
		windows = append(windows, window)
	}
	return windows
}

// openCodeGoUtilization converts one reported window. An unparsable reset
// leaves the window without a start, which keeps it visible but unusable for a
// pace score instead of inventing a reset.
func openCodeGoUtilization(label string, raw *openCodeGoWindow) UtilizationWindow {
	window := UtilizationWindow{
		Label:          label,
		UtilizationPct: raw.Percent,
		Scope:          WindowScopePaidModels,
		LimitReached:   raw.Status != "" && !strings.EqualFold(raw.Status, openCodeGoStatusOK),
	}
	if resetAt, err := time.Parse(time.RFC3339, raw.ResetsAt); err == nil {
		window.ResetAt = resetAt
	}
	return window
}
