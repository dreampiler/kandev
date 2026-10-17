package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// This account-aware client leaves the legacy OAuth usage client unchanged.
// Its profile and usage requests use the same resolved token snapshot.
type claudeExecutionUsageClient struct {
	base                    *ClaudeUsageClient
	directToken, profileURL string
	mu                      sync.Mutex
	identityToken           string
	identity                executionAccount
	identityAt              time.Time
}

func (c *claudeExecutionUsageClient) matchesSource(source executionUsageSource) bool {
	return source.provider == "anthropic" && source.path == c.base.credentialsPath && source.token == c.directToken
}

func (c *claudeExecutionUsageClient) token(ctx context.Context) (string, string, error) {
	if c.directToken != "" {
		return c.directToken, "", nil
	}
	creds, err := c.base.readCredentials()
	if err != nil {
		return "", "", err
	}
	if creds.ClaudeAiOauth == nil || creds.ClaudeAiOauth.AccessToken == "" {
		return "", "", fmt.Errorf("execution subscription account is unavailable")
	}
	token, err := c.base.freshAccessToken(ctx, creds.ClaudeAiOauth)
	return token, creds.ClaudeAiOauth.SubscriptionType, err
}

func (c *claudeExecutionUsageClient) resolveIdentity(ctx context.Context, token string) (executionAccount, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identityToken == token && !c.identityAt.IsZero() && time.Since(c.identityAt) <= cacheTTL {
		return c.identity, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.profileURL, nil)
	if err != nil {
		return executionAccount{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := c.base.httpClient.Do(request)
	if err != nil {
		return executionAccount{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return executionAccount{}, fmt.Errorf("execution account profile returned status %d", response.StatusCode)
	}
	var profile struct {
		Account struct {
			UUID string `json:"uuid"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
	}
	if err := json.NewDecoder(response.Body).Decode(&profile); err != nil {
		return executionAccount{}, err
	}
	if profile.Account.UUID == "" || profile.Organization.UUID == "" {
		return executionAccount{}, fmt.Errorf("execution account profile identity is incomplete")
	}
	c.identity = executionAccount{account: profile.Account.UUID, organization: profile.Organization.UUID}
	c.identityToken, c.identityAt = token, time.Now()
	return c.identity, nil
}

func (c *claudeExecutionUsageClient) accountIdentity(ctx context.Context) (executionAccount, error) {
	token, _, err := c.token(ctx)
	if err != nil {
		return executionAccount{}, err
	}
	return c.resolveIdentity(ctx, token)
}

func (c *claudeExecutionUsageClient) FetchUsage(ctx context.Context) (*ProviderUsage, error) {
	token, plan, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	identity, err := c.resolveIdentity(ctx, token)
	if err != nil {
		return nil, err
	}
	body, err := c.base.getUsage(ctx, token)
	if err != nil {
		return nil, err
	}
	var raw claudeUsageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	now := time.Now()
	return &ProviderUsage{AccountRef: identity.account, OrganizationRef: identity.organization,
		Provider: "anthropic", Plan: plan, Windows: claudeExecutionWindows(raw, now), FetchedAt: now}, nil
}

// Strict native readers retain observed utilization even without a reset.
// The legacy client's parser and wire behavior remain unchanged.
func claudeExecutionWindows(raw claudeUsageResponse, now time.Time) []UtilizationWindow {
	if len(raw.Limits) == 0 {
		return claudeWindows(raw, now)
	}
	windows := make([]UtilizationWindow, 0, len(raw.Limits))
	for _, limit := range raw.Limits {
		label := claudeLimitLabel(limit)
		if label == "" {
			continue
		}
		reset, err := time.Parse(time.RFC3339, limit.ResetsAt)
		windows = append(windows, UtilizationWindow{Label: label, Model: claudeLimitModel(limit), UtilizationPct: legacyUtilization(limit.Percent), ResetAt: reset, Observation: &WindowObservation{UtilizationObserved: utilizationObserved(limit.Percent), ResetAtObserved: err == nil}})
	}
	return windows
}
