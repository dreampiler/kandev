package usage

import (
	"context"
	"net/http"
	"time"
)

const (
	llmGatewayKeyURL    = "https://api.llmgateway.io/v1/key"
	llmGatewayModelsURL = "https://api.llmgateway.io/v1/models"
	llmGatewayProvider  = "llmgateway"

	llmGatewayMonthlyLabel = "monthly"
	llmGatewayPremiumLabel = "7-day premium"
	llmGatewayPremiumSpan  = 7 * 24 * time.Hour
)

// LLMGatewayUsageClient reads an LLM Gateway dev plan (DevPass) allowance: the
// plan's credits for the billing month, and the weekly cap on premium models.
type LLMGatewayUsageClient struct {
	key        OpenCodeKeySource
	keyURL     string
	httpClient *http.Client
	now        func() time.Time
}

// NewLLMGatewayUsageClient creates a client that authenticates with the LLM
// Gateway key in the OpenCode auth file at authPath.
func NewLLMGatewayUsageClient(authPath string) *LLMGatewayUsageClient {
	return &LLMGatewayUsageClient{
		key:        OpenCodeKeySource{Path: authPath, Providers: []string{"llmgateway"}},
		keyURL:     llmGatewayKeyURL,
		httpClient: &http.Client{Timeout: apiKeyUsageHTTPTimeout},
		now:        time.Now,
	}
}

type llmGatewayKeyResponse struct {
	Data struct {
		DevPlan                    string      `json:"devPlan"`
		DevPlanCreditsUsed         flexDecimal `json:"devPlanCreditsUsed"`
		DevPlanCreditsLimit        flexDecimal `json:"devPlanCreditsLimit"`
		DevPlanCreditsRemaining    flexDecimal `json:"devPlanCreditsRemaining"`
		DevPlanPremiumWeeklyLimit  flexDecimal `json:"devPlanPremiumWeeklyLimit"`
		DevPlanPremiumCreditsUsed  flexDecimal `json:"devPlanPremiumCreditsUsed"`
		DevPlanPremiumWeekResetsAt string      `json:"devPlanPremiumWeekResetsAt"`
	} `json:"data"`
}

// FetchUsage implements ProviderUsageClient.
func (c *LLMGatewayUsageClient) FetchUsage(ctx context.Context) (*ProviderUsage, error) {
	apiKey, err := c.key.APIKey(llmGatewayProvider)
	if err != nil {
		return nil, err
	}
	var raw llmGatewayKeyResponse
	if err := getBearerJSON(ctx, c.httpClient, llmGatewayProvider, c.keyURL, apiKey, &raw); err != nil {
		return nil, err
	}
	return parseLLMGatewayUsage(raw, c.now()), nil
}

// parseLLMGatewayUsage maps the dev plan fields. The provider reports the
// premium week's reset but not the billing month's, so the monthly window keeps
// its percentage and exhaustion without a start or reset: it is visible, and an
// exhausted plan is known, but its pace is never computed from an invented month.
func parseLLMGatewayUsage(raw llmGatewayKeyResponse, now time.Time) *ProviderUsage {
	data := raw.Data
	usage := &ProviderUsage{
		Provider: llmGatewayProvider, Plan: data.DevPlan, Windows: []UtilizationWindow{}, FetchedAt: now,
	}
	if data.DevPlan == "" || data.DevPlan == "none" {
		return usage
	}
	if data.DevPlanCreditsLimit.Set && data.DevPlanCreditsLimit.Value > 0 {
		exhausted := data.DevPlanCreditsRemaining.Set && data.DevPlanCreditsRemaining.Value <= 0
		usage.Windows = append(usage.Windows, UtilizationWindow{
			Label:          llmGatewayMonthlyLabel,
			UtilizationPct: 100 * data.DevPlanCreditsUsed.Value / data.DevPlanCreditsLimit.Value,
			LimitReached:   exhausted,
		})
	}
	if data.DevPlanPremiumWeeklyLimit.Set && data.DevPlanPremiumWeeklyLimit.Value > 0 {
		window := UtilizationWindow{
			Label:          llmGatewayPremiumLabel,
			UtilizationPct: 100 * data.DevPlanPremiumCreditsUsed.Value / data.DevPlanPremiumWeeklyLimit.Value,
			Scope:          WindowScopePremiumModels,
		}
		// The premium week opens at the first premium request, so before that
		// request there is no reset and the window has no usage to report.
		if resetAt, err := time.Parse(time.RFC3339, data.DevPlanPremiumWeekResetsAt); err == nil {
			window.ResetAt = resetAt
			window.StartAt = resetAt.Add(-llmGatewayPremiumSpan)
			window.DurationSeconds = int64(llmGatewayPremiumSpan / time.Second)
		}
		window.LimitReached = window.UtilizationPct >= 100
		usage.Windows = append(usage.Windows, window)
	}
	return usage
}
