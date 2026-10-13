package review

import (
	"errors"
	"testing"
)

func TestParseReviewNormalOutput(t *testing.T) {
	for name, response := range map[string]string{
		"explicit empty":          "No findings.",
		"explicit empty review":   "No issues found.",
		"prose with other braces": "I checked {example} in the diff.\n" + validEnvelope + "\nSee {context} above.",
		"fenced with prose":       "Review complete:\n```json\n" + validEnvelope + "\n```\nDone.",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParseFindings(response)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if name == "explicit empty" || name == "explicit empty review" {
				want = 0
			}
			if len(got.Findings) != want {
				t.Fatalf("findings = %d, want %d", len(got.Findings), want)
			}
		})
	}
}

func TestParseReviewDoesNotHideMalformedFindings(t *testing.T) {
	for _, response := range []string{
		`{"findings":null}`, `{"summary":"mentions findings"}`,
		"No findings. But check the nil dereference in a.go.",
		"No findings.\n```json\n{\"findings\":broken}\n```",
	} {
		if _, err := ParseFindings(response); !errors.Is(err, ErrUnparseableResponse) {
			t.Fatalf("expected malformed or contradictory output to fail, got %v", err)
		}
	}
}
