package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	claudeUsageURL   = "https://api.anthropic.com/api/oauth/usage"
	claudeRefreshURL = "https://platform.claude.com/v1/oauth/token"
	claudeBetaHeader = "oauth-2025-04-20"

	claudeLabel5Hour = "5-hour"
	claudeLabel7Day  = "7-day"
)

// ClaudeOAuthTokenEnv is the environment variable Claude Code reads a
// long-lived OAuth token from (`claude setup-token`). A host-run Claude agent
// inherits it from the Kandev process, so it is that agent's account.
const ClaudeOAuthTokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

const claudeProvider = "anthropic"

// TokenResolver supplies the OAuth token a usage read must authenticate with.
// It is called once per read, so a rotated credential is picked up without a
// restart, and its result is used only as the request bearer.
type TokenResolver func(ctx context.Context) (string, error)

// ClaudeUsageClient fetches utilization from the Anthropic OAuth usage API.
// It authenticates with a static OAuth token, which it never refreshes or
// persists, with a token resolved per read, or with the CLI credentials file.
type ClaudeUsageClient struct {
	credentialsPath string
	staticToken     string
	tokenResolver   TokenResolver
	usageURL        string
	refreshURL      string
	httpClient      *http.Client
}

// NewClaudeUsageClientWithPath creates a client with an explicit credentials path (for tests).
func NewClaudeUsageClientWithPath(credentialsPath string) *ClaudeUsageClient {
	return &ClaudeUsageClient{
		credentialsPath: credentialsPath,
		usageURL:        claudeUsageURL,
		refreshURL:      claudeRefreshURL,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClaudeUsageClientWithOAuthToken creates a client for a long-lived OAuth
// token supplied through the environment rather than the credentials file.
func NewClaudeUsageClientWithOAuthToken(token string) *ClaudeUsageClient {
	return &ClaudeUsageClient{
		staticToken: token,
		usageURL:    claudeUsageURL,
		refreshURL:  claudeRefreshURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClaudeUsageClientWithTokenResolver creates a client that asks resolve for
// the OAuth token on every read. It is how a profile whose token lives in a
// secret store is read without the token ever being held by Kandev as a
// configured value.
func NewClaudeUsageClientWithTokenResolver(resolve TokenResolver) *ClaudeUsageClient {
	return &ClaudeUsageClient{
		tokenResolver: resolve,
		usageURL:      claudeUsageURL,
		refreshURL:    claudeRefreshURL,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// CredentialsPath returns the path this client reads credentials from.
func (c *ClaudeUsageClient) CredentialsPath() string {
	return c.credentialsPath
}

// HasSubscriptionCredentials reports whether the credentials file exists and
// carries an OAuth (subscription) token.
func (c *ClaudeUsageClient) HasSubscriptionCredentials() bool {
	if c.staticToken != "" {
		return true
	}
	creds, err := c.readCredentials()
	return err == nil && creds.ClaudeAiOauth != nil && creds.ClaudeAiOauth.AccessToken != ""
}

type claudeCredentials struct {
	ClaudeAiOauth *claudeOAuthToken `json:"claudeAiOauth"`
}

type claudeOAuthToken struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken,omitempty"`
	ExpiresAt        int64  `json:"expiresAt"` // Unix milliseconds
	SubscriptionType string `json:"subscriptionType,omitempty"`
}

type claudeUsageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at,omitempty"`
}

type claudeLimitScope struct {
	Model *struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
}

type claudeLimit struct {
	Kind     string            `json:"kind"`
	Percent  float64           `json:"percent"`
	ResetsAt string            `json:"resets_at"`
	Scope    *claudeLimitScope `json:"scope"`
}

type claudeUsageResponse struct {
	FiveHour *claudeUsageWindow `json:"five_hour"`
	SevenDay *claudeUsageWindow `json:"seven_day"`
	Limits   []claudeLimit      `json:"limits"`
}

// FetchUsage implements ProviderUsageClient.
func (c *ClaudeUsageClient) FetchUsage(ctx context.Context) (*ProviderUsage, error) {
	token, plan, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	body, err := c.getUsage(ctx, token)
	if err != nil {
		return nil, err
	}

	var raw claudeUsageResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &FetchError{Provider: claudeProvider, Reason: FailureDecode, Err: err}
	}

	now := time.Now()
	return &ProviderUsage{
		Provider:  claudeProvider,
		Plan:      plan,
		Windows:   claudeWindows(raw, now),
		FetchedAt: now,
	}, nil
}

// accessToken resolves the bearer token and the plan it belongs to. A static or
// per-read token is used as given; the credentials file is refreshed when it
// expires.
func (c *ClaudeUsageClient) accessToken(ctx context.Context) (token string, plan string, err error) {
	if c.staticToken != "" {
		return c.staticToken, "", nil
	}
	if c.tokenResolver != nil {
		token, err := c.tokenResolver(ctx)
		if err != nil || strings.TrimSpace(token) == "" {
			return "", "", &FetchError{Provider: claudeProvider, Reason: FailureCredentialMissing, Err: err}
		}
		return token, "", nil
	}
	creds, err := c.readCredentials()
	if err != nil {
		return "", "", &FetchError{Provider: claudeProvider, Reason: FailureCredentialMissing, Err: err}
	}
	if creds.ClaudeAiOauth == nil || creds.ClaudeAiOauth.AccessToken == "" {
		return "", "", &FetchError{Provider: claudeProvider, Reason: FailureCredentialMissing}
	}
	token, err = c.freshAccessToken(ctx, creds.ClaudeAiOauth)
	if err != nil {
		return "", "", &FetchError{Provider: claudeProvider, Reason: FailureUnauthorized, Err: err}
	}
	return token, creds.ClaudeAiOauth.SubscriptionType, nil
}

func (c *ClaudeUsageClient) getUsage(ctx context.Context, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.usageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("claude usage: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", claudeBetaHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &FetchError{Provider: claudeProvider, Reason: FailureNetwork, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &FetchError{Provider: claudeProvider, Reason: FailureNetwork, Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, statusFailure(claudeProvider, resp.StatusCode, resp.Header)
	}
	return body, nil
}

// claudeWindows prefers the richer limits[] array (session, weekly, per-model
// weekly) and falls back to the legacy five_hour/seven_day pair. Always
// returns a non-nil slice so the API serializes `windows` as an array.
func claudeWindows(raw claudeUsageResponse, now time.Time) []UtilizationWindow {
	if windows := claudeLimitWindows(raw.Limits); len(windows) > 0 {
		return windows
	}
	windows := make([]UtilizationWindow, 0, 2)
	if raw.FiveHour != nil {
		windows = append(windows, claudeFixedWindow(claudeLabel5Hour, raw.FiveHour, now, 5*time.Hour))
	}
	if raw.SevenDay != nil {
		windows = append(windows, claudeFixedWindow(claudeLabel7Day, raw.SevenDay, now, 7*24*time.Hour))
	}
	return windows
}

// claudeFixedWindow builds a window for the named known duration, so routing
// gets a numeric length rather than having to parse the display label. A reset
// that cannot be parsed yields a zero start, which makes the window unusable.
func claudeFixedWindow(label string, raw *claudeUsageWindow, now time.Time, duration time.Duration) UtilizationWindow {
	resetAt := parseResetAt(raw.ResetsAt, now, duration)
	window := UtilizationWindow{
		Label:           label,
		UtilizationPct:  raw.Utilization,
		ResetAt:         resetAt,
		DurationSeconds: int64(duration / time.Second),
	}
	if !resetAt.IsZero() {
		window.StartAt = resetAt.Add(-duration)
	}
	return window
}

func claudeLimitWindows(limits []claudeLimit) []UtilizationWindow {
	var windows []UtilizationWindow
	for _, l := range limits {
		label := claudeLimitLabel(l)
		duration, known := claudeWindowDuration(l.Kind)
		if label == "" || !known {
			continue
		}
		resetAt, err := time.Parse(time.RFC3339, l.ResetsAt)
		if err != nil {
			continue
		}
		windows = append(windows, UtilizationWindow{
			Label:           label,
			UtilizationPct:  l.Percent,
			ResetAt:         resetAt,
			DurationSeconds: int64(duration / time.Second),
			StartAt:         resetAt.Add(-duration),
			// A scoped limit names only a display name, which is not an identity,
			// so it cannot be matched to a candidate and stays unusable.
			AmbiguousModelScope: l.Kind == claudeLimitWeeklyScoped,
		})
	}
	return windows
}

// claudeWindowDuration maps a known provider window kind to its numeric length.
// An unknown kind has no duration, so it is skipped for routing rather than
// guessed from its display label.
func claudeWindowDuration(kind string) (time.Duration, bool) {
	switch kind {
	case "session":
		return 5 * time.Hour, true
	case "weekly_all", claudeLimitWeeklyScoped:
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

const claudeLimitWeeklyScoped = "weekly_scoped"

func claudeLimitLabel(l claudeLimit) string {
	switch l.Kind {
	case "session":
		return claudeLabel5Hour
	case "weekly_all":
		return claudeLabel7Day
	case "weekly_scoped":
		if l.Scope != nil && l.Scope.Model != nil && l.Scope.Model.DisplayName != "" {
			return "7-day (" + l.Scope.Model.DisplayName + ")"
		}
		return "7-day (model)"
	default:
		// Unknown kinds are skipped rather than shown with a cryptic label.
		return ""
	}
}

// freshAccessToken returns a valid access token, refreshing if expired.
func (c *ClaudeUsageClient) freshAccessToken(ctx context.Context, tok *claudeOAuthToken) (string, error) {
	// ExpiresAt is in milliseconds; treat as expired if within 60 s of now.
	expiresAt := time.UnixMilli(tok.ExpiresAt)
	if time.Until(expiresAt) > 60*time.Second {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		return "", fmt.Errorf("claude token expired and no refresh token available")
	}
	newTok, err := c.refreshToken(ctx, tok.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}
	// Write new token back. Non-fatal — we have the new token in memory even if
	// persistence fails, but log the error so it doesn't go unnoticed.
	if writeErr := c.persistRefreshedToken(newTok); writeErr != nil {
		fmt.Fprintf(os.Stderr, "claude usage: persist refreshed token: %v\n", writeErr)
	}
	return newTok.AccessToken, nil
}

func (c *ClaudeUsageClient) readCredentials() (*claudeCredentials, error) {
	data, err := os.ReadFile(c.credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.credentialsPath, err)
	}
	var creds claudeCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", c.credentialsPath, err)
	}
	return &creds, nil
}

// persistRefreshedToken updates only the token fields inside claudeAiOauth,
// preserving unknown siblings (scopes, subscriptionType, rateLimitTier, ...).
func (c *ClaudeUsageClient) persistRefreshedToken(tok *claudeOAuthToken) error {
	data, err := os.ReadFile(c.credentialsPath)
	if err != nil {
		return err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	oauth, _ := root["claudeAiOauth"].(map[string]any)
	if oauth == nil {
		oauth = map[string]any{}
	}
	oauth["accessToken"] = tok.AccessToken
	oauth["refreshToken"] = tok.RefreshToken
	oauth["expiresAt"] = tok.ExpiresAt
	root["claudeAiOauth"] = oauth
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(c.credentialsPath, out, 0o600)
}

type claudeRefreshRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

type claudeRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

func (c *ClaudeUsageClient) refreshToken(ctx context.Context, refreshToken string) (*claudeOAuthToken, error) {
	payload := claudeRefreshRequest{GrantType: "refresh_token", RefreshToken: refreshToken}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.refreshURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh: status %d: %s", resp.StatusCode, respBody)
	}
	var r claudeRefreshResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(time.Duration(r.ExpiresIn) * time.Second).UnixMilli()
	newRefresh := r.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken // keep old if not rotated
	}
	return &claudeOAuthToken{
		AccessToken:  r.AccessToken,
		RefreshToken: newRefresh,
		ExpiresAt:    expiresAt,
	}, nil
}

// parseResetAt parses an ISO timestamp or falls back to now+duration.
func parseResetAt(raw string, now time.Time, windowDuration time.Duration) time.Time {
	if raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t
		}
	}
	return now.Add(windowDuration)
}
