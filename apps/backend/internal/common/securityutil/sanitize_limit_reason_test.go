package securityutil

import (
	"strings"
	"testing"
)

// A gateway's daily reason code must not become an exception from the
// diagnostic redaction: an opaque value that merely wears the "limit_rpd/"
// prefix is still masked, and a trailing mixed-case run does not survive.
func TestSanitizeMasksLimitReasonShapedOpaqueRuns(t *testing.T) {
	run := strings.Repeat("a", 40)
	for _, in := range []string{
		"diagnostic: limit_rpd/" + run,
		"diagnostic: limit_rpd/" + run + "XYZ",
	} {
		got := Sanitize(in)
		if strings.Contains(got, run) {
			t.Fatalf("Sanitize(%q) = %q, want the opaque run masked", in, got)
		}
	}
}

// Sanitize never restores a token it did not mask, so an original string that
// looks like an internal marker survives untouched rather than being rewritten
// into a different diagnostic.
func TestSanitizeDoesNotRestoreMarkerLookalikes(t *testing.T) {
	in := "Rate limit exceeded: kandevkeepmask0x"
	if got := Sanitize(in); got != in {
		t.Fatalf("Sanitize(%q) = %q, want it unchanged", in, got)
	}
}
