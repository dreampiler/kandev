package usage

import (
	"context"
	"net/http"
	"time"
)

const (
	openRouterKeyURL    = "https://openrouter.ai/api/v1/key"
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	openRouterProvider  = "openrouter"

	// openRouterDailyLabel names the account's free-model request quota.
	openRouterDailyLabel = "1-day"
)

// OpenRouterUsageClient reads the account's free-model daily request quota.
// OpenRouter counts free-model requests across the whole account, and reports
// the count and the limit that applies to it (the limit rises once credits are
// purchased), so the provider's own figure is used rather than a local count.
type OpenRouterUsageClient struct {
	key        OpenCodeKeySource
	keyURL     string
	httpClient *http.Client
	now        func() time.Time
}

// NewOpenRouterUsageClient creates a client that authenticates with the
// OpenRouter key in the OpenCode auth file at authPath.
func NewOpenRouterUsageClient(authPath string) *OpenRouterUsageClient {
	return &OpenRouterUsageClient{
		key:        OpenCodeKeySource{Path: authPath, Providers: []string{"openrouter"}},
		keyURL:     openRouterKeyURL,
		httpClient: &http.Client{Timeout: apiKeyUsageHTTPTimeout},
		now:        time.Now,
	}
}

type openRouterKeyResponse struct {
	Data struct {
		IsFreeTier             bool `json:"is_free_tier"`
		FreeModelDailyRequests *struct {
			Used  float64 `json:"used"`
			Limit float64 `json:"limit"`
		} `json:"free_model_daily_requests"`
	} `json:"data"`
}

// FetchUsage implements ProviderUsageClient.
func (c *OpenRouterUsageClient) FetchUsage(ctx context.Context) (*ProviderUsage, error) {
	apiKey, err := c.key.APIKey(openRouterProvider)
	if err != nil {
		return nil, err
	}
	var raw openRouterKeyResponse
	if err := getBearerJSON(ctx, c.httpClient, openRouterProvider, c.keyURL, apiKey, &raw); err != nil {
		return nil, err
	}
	return parseOpenRouterUsage(raw, c.now()), nil
}

// parseOpenRouterUsage turns the free-request counter into a daily window. The
// quota resets at midnight UTC, so the window spans the current UTC day. A
// response without the counter yields no window rather than a guessed limit.
func parseOpenRouterUsage(raw openRouterKeyResponse, now time.Time) *ProviderUsage {
	plan := "credits"
	if raw.Data.IsFreeTier {
		plan = "free"
	}
	usage := &ProviderUsage{Provider: openRouterProvider, Plan: plan, Windows: []UtilizationWindow{}, FetchedAt: now}
	counter := raw.Data.FreeModelDailyRequests
	if counter == nil || counter.Limit <= 0 {
		return usage
	}
	start := now.UTC().Truncate(24 * time.Hour)
	reset := start.Add(24 * time.Hour)
	usage.Windows = append(usage.Windows, UtilizationWindow{
		Label:           openRouterDailyLabel,
		UtilizationPct:  100 * counter.Used / counter.Limit,
		ResetAt:         reset,
		StartAt:         start,
		DurationSeconds: int64(24 * time.Hour / time.Second),
		Scope:           WindowScopeFreeModels,
		LimitReached:    counter.Used >= counter.Limit,
	})
	return usage
}
