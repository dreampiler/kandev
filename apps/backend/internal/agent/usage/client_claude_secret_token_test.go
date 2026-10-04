package usage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A profile whose token lives in the secret store authenticates the usage read
// with the value the resolver returns, and never falls through to the
// credentials file that file path would otherwise read.
func TestClaudeTokenResolverSuppliesTheBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"five_hour": {"utilization": 10.0, "resets_at": "2026-07-14T22:09:59+00:00"}}`))
	}))
	defer srv.Close()

	calls := 0
	c := NewClaudeUsageClientWithTokenResolver(func(context.Context) (string, error) {
		calls++
		return "resolved-token", nil
	})
	c.usageURL = srv.URL

	if _, err := c.FetchUsage(context.Background()); err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if gotAuth != "Bearer resolved-token" {
		t.Fatalf("Authorization = %q, want the resolved token", gotAuth)
	}
	if calls != 1 {
		t.Fatalf("resolver called %d times, want one call per read", calls)
	}
	if c.CredentialsPath() != "" {
		t.Fatalf("credentials path = %q, want no file source", c.CredentialsPath())
	}
}

// A credential the resolver cannot supply is the same bounded reason as a
// missing one, and the resolver's own message never reaches a log line.
func TestClaudeTokenResolverFailureIsCredentialMissingWithoutDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the provider must not be called without a token")
	}))
	defer srv.Close()

	detail := errors.New("reveal secret credential-abc123: decrypt secret: cipher: message authentication failed")
	for _, tc := range []struct {
		name  string
		token string
		err   error
	}{
		{"resolver failed", "", detail},
		{"resolved empty", "   ", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClaudeUsageClientWithTokenResolver(func(context.Context) (string, error) {
				return tc.token, tc.err
			})
			c.usageURL = srv.URL

			_, err := c.FetchUsage(context.Background())
			reason, status := FailureOf(err)
			if reason != FailureCredentialMissing || status != 0 {
				t.Fatalf("reason = %q status = %d, want credential_missing without a status", reason, status)
			}
			if err == nil || strings.Contains(err.Error(), "credential-abc123") || strings.Contains(err.Error(), "decrypt") {
				t.Fatalf("error %v must name neither the secret nor the store failure", err)
			}
		})
	}
}
