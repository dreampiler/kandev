package usage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	apiKeyUsageHTTPTimeout = 10 * time.Second
	// maxUsageResponseBytes bounds a provider response. Model catalogs are the
	// largest bodies read here, at under a megabyte today.
	maxUsageResponseBytes = 8 << 20
)

// OpenCodeAuthPath returns the credential file the local OpenCode CLI reads.
// OpenCode stores provider API keys under its XDG data directory on every
// platform, so a host-run OpenCode agent authenticates with exactly these keys.
func OpenCodeAuthPath(home string) string {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return filepath.Join(dir, "opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// OpenCodeKeySource reads one provider's API key from the OpenCode auth file.
// Providers lists the auth entries to try in order; the first non-empty key
// wins. The key is read on every fetch so a re-login is picked up without a
// restart.
type OpenCodeKeySource struct {
	Path      string
	Providers []string
}

type openCodeAuthEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// APIKey returns the first configured key. A missing file or entry is a
// credential failure, never an empty key sent to the provider.
func (s OpenCodeKeySource) APIKey(provider string) (string, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return "", &FetchError{Provider: provider, Reason: FailureCredentialMissing, Err: err}
	}
	var entries map[string]openCodeAuthEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return "", &FetchError{Provider: provider, Reason: FailureCredentialMissing, Err: err}
	}
	for _, name := range s.Providers {
		if key := strings.TrimSpace(entries[name].Key); key != "" {
			return key, nil
		}
	}
	return "", &FetchError{Provider: provider, Reason: FailureCredentialMissing}
}

// getBearerJSON performs one authenticated GET and decodes a JSON body. Errors
// are classified without retaining the response body, so a provider echoing
// account details into an error page cannot reach logs or clients.
func getBearerJSON(
	ctx context.Context,
	client *http.Client,
	provider string,
	url string,
	apiKey string,
	out any,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &FetchError{Provider: provider, Reason: FailureNetwork, Err: err}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return &FetchError{Provider: provider, Reason: FailureNetwork, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUsageResponseBytes))
	if err != nil {
		return &FetchError{Provider: provider, Reason: FailureNetwork, Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		return statusFailure(provider, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &FetchError{Provider: provider, Reason: FailureDecode, Err: err}
	}
	return nil
}

// flexDecimal decodes a provider number that may be encoded as a JSON number
// or as a decimal string, as LLM Gateway does for credit amounts.
type flexDecimal struct {
	Value float64
	Set   bool
}

func (d *flexDecimal) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		return nil
	}
	var value float64
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return err
	}
	d.Value, d.Set = value, true
	return nil
}
