package routingerr

import (
	"strings"
	"testing"
	"time"
)

// antigravityQuotaNotice is the notice agy states as ordinary assistant text
// when its quota for the period is exhausted. It is the only failure signal the
// provider gives: the prompt is never settled afterwards.
const antigravityQuotaNotice = "Usage Limit Reached\n\nYou have reached your current quota for this period. Your limit will reset in 1 hour, 28 minutes."

func TestClassify_QuotaNoticeStatedAsAgentMessage(t *testing.T) {
	observed := time.Date(2026, 10, 4, 15, 51, 8, 0, time.UTC)

	got := Classify(Input{
		Phase:      PhasePromptSend,
		ProviderID: "antigravity-acp",
		Stderr:     antigravityQuotaNotice,
		OccurredAt: observed,
	})

	if got.Code != CodeQuotaLimited || got.Confidence != ConfHigh || !got.FallbackAllowed {
		t.Fatalf("code=%s rule=%s confidence=%s fallback=%v, want quota_limited/high/fallback",
			got.Code, got.ClassifierRule, got.Confidence, got.FallbackAllowed)
	}
	if got.ResetHint == nil {
		t.Fatal("relative reset notice produced no reset hint")
	}
	if want := observed.Add(88 * time.Minute); !got.ResetHint.Equal(want) {
		t.Fatalf("reset hint = %s, want %s", got.ResetHint, want)
	}
}

func TestClassify_QuotaNoticeHeadlineAloneIsNotHighConfidence(t *testing.T) {
	// A terse "usage limit reached" also arrives as a terminal ACP error string
	// and inside an agent's prose. On its own it must stay short of the
	// confidence that ends a turn or promotes the quota recovery surface; the
	// adapter instead accumulates the turn's text and matches the whole notice.
	for _, chunk := range []string{"Usage Limit Reached", "Usage Limit Reached.", "  usage limit reached  "} {
		got := Classify(Input{Phase: PhasePromptSend, ProviderID: "antigravity-acp", Stderr: chunk})
		if got.Code == CodeQuotaLimited && got.Confidence == ConfHigh {
			t.Fatalf("chunk %q → code=%s rule=%s confidence=%s", chunk, got.Code, got.ClassifierRule, got.Confidence)
		}
	}
}

func TestClassify_ProseAboutALimitIsNotHighConfidenceQuota(t *testing.T) {
	// A false positive here ends a live turn, so an agent explaining or quoting
	// a limit it read about must stay out of the high-confidence quota class.
	for _, text := range []string{
		"The API returned 'Usage Limit Reached' last night, so I raised the timeout.",
		"You have reached your current quota for this period is the error the provider shows; here is the fix.",
		"usage limit reached check passed for the new endpoint",
		"",
	} {
		got := Classify(Input{Phase: PhaseStreaming, ProviderID: "antigravity-acp", Stderr: text})
		if got.Code == CodeQuotaLimited && got.Confidence == ConfHigh {
			t.Fatalf("prose %q misclassified as high-confidence quota (%s)", text, got.ClassifierRule)
		}
	}
}

func TestParseRelativeResetHintAt(t *testing.T) {
	observed := time.Date(2026, 10, 4, 6, 0, 0, 0, time.UTC)

	for text, want := range map[string]time.Duration{
		"limit resets in 30 minutes":        30 * time.Minute,
		"Resets in 3 days 4 hours 19 min.":  3*24*time.Hour + 4*time.Hour + 19*time.Minute,
		"will reset in 1 hour, 28 minutes.": 88 * time.Minute,
		"reset in 2 days":                   48 * time.Hour,
		"resets in 45 min":                  45 * time.Minute,
	} {
		got := parseRelativeResetHintAt(text, observed)
		if got == nil {
			t.Fatalf("%q → no hint", text)
		}
		if !got.Equal(observed.Add(want)) {
			t.Fatalf("%q → %s, want %s", text, got, observed.Add(want))
		}
	}

	// A zero or partly understood duration must never become a circuit deadline.
	for _, text := range []string{"resets in 0 minutes", "resets in soon", "resets in 3 weeks", ""} {
		if got := parseRelativeResetHintAt(text, observed); got != nil {
			t.Fatalf("%q → %s, want nil", text, got)
		}
	}
}

func TestSanitizePreservesNoticeDates(t *testing.T) {
	// A notice states the date its capacity frees; the slash form is otherwise
	// indistinguishable from a path tail to the local-path pass.
	if got := Sanitize("blocked until 10/7/2026 at 23:20"); got != "blocked until 10/7/2026 at 23:20" {
		t.Fatalf("slash date lost: %q", got)
	}
	if got := Sanitize("window closes 2026-10-07T10:00:00Z"); got != "window closes 2026-10-07T10:00:00Z" {
		t.Fatalf("iso date lost: %q", got)
	}
	if got := Sanitize("see /home/agent/work/notes for details"); strings.Contains(got, "/home/agent/") {
		t.Fatalf("real path not redacted: %q", got)
	}
	once := Sanitize("blocked until 10/7/2026 and 2026-10-08")
	if twice := Sanitize(once); twice != once {
		t.Fatalf("sanitize is not idempotent across the date mask: %q vs %q", once, twice)
	}
}
