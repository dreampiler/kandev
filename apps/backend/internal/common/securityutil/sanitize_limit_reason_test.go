package securityutil

import (
	"strings"
	"testing"
)

// The gateway's rate-limit reason code names a public model or allowance, not
// an account, and routing reads how far a limit reaches from it. The broad
// redaction tier must not mask it, and Sanitize must stay idempotent.
func TestSanitizePreservesLimitReasonCodes(t *testing.T) {
	cases := []struct {
		in   string
		keep string
	}{
		{
			in:   "AI_APICallError: Rate limit exceeded: free-models-per-day-high-balance",
			keep: "free-models-per-day-high-balance",
		},
		{
			in:   "AI_APICallError: Rate limit exceeded: limit_rpd/nvidia/nemotron-3-ultra-550b-a55b:free",
			keep: "limit_rpd/nvidia/nemotron-3-ultra-550b-a55b:free",
		},
	}
	for _, tc := range cases {
		got := Sanitize(tc.in)
		if !strings.Contains(got, tc.keep) {
			t.Fatalf("Sanitize(%q) = %q, want it to keep %q", tc.in, got, tc.keep)
		}
		if again := Sanitize(got); again != got {
			t.Fatalf("Sanitize is not idempotent: %q -> %q", got, again)
		}
	}
}

// Preserving the reason code must not weaken credential redaction beside it.
func TestSanitizeStillRedactsCredentialsBesideLimitReason(t *testing.T) {
	got := Sanitize("AI_APICallError: Rate limit exceeded: free-models-per-day api_key=sk-abcdefghijklmnopqrstuvwxyz0123456789")
	if !strings.Contains(got, "free-models-per-day") {
		t.Fatalf("Sanitize = %q, want it to keep the reason code", got)
	}
	if strings.Contains(got, "abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("Sanitize = %q, want the credential redacted", got)
	}
}
