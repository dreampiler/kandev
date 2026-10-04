package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// AntigravityUsageClient reads the local Antigravity CLI's quota report.
type AntigravityUsageClient struct{}

func NewAntigravityUsageClient() *AntigravityUsageClient {
	return &AntigravityUsageClient{}
}

// FetchUsage implements ProviderUsageClient without starting a model turn.
func (c *AntigravityUsageClient) FetchUsage(ctx context.Context) (*ProviderUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "agy", "-p", "/usage", "--output-format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("antigravity usage: run agy: %w", err)
	}
	return parseAntigravityUsage(output, time.Now())
}

type antigravityBucket struct {
	ID                string   `json:"id"`
	Window            string   `json:"window"`
	RemainingFraction *float64 `json:"remaining_fraction"`
	ResetTime         string   `json:"reset_time"`
}

type antigravityUsageResponse struct {
	Status  string `json:"status"`
	Command struct {
		Data struct {
			Groups []struct {
				Name    string              `json:"name"`
				Buckets []antigravityBucket `json:"buckets"`
			} `json:"groups"`
		} `json:"data"`
	} `json:"command"`
}

// Quota response shape follows MIT-licensed https://github.com/Moris-kr/ai-chatroom/blob/main/lib/usage.mjs.
func parseAntigravityUsage(output []byte, now time.Time) (*ProviderUsage, error) {
	var raw antigravityUsageResponse
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("antigravity usage: decode: %w", err)
	}
	if raw.Status != "SUCCESS" {
		return nil, fmt.Errorf("antigravity usage: command status %q", raw.Status)
	}
	for _, group := range raw.Command.Data.Groups {
		if !strings.EqualFold(group.Name, "Gemini Models") {
			continue
		}
		windows := make([]UtilizationWindow, 0, len(group.Buckets))
		for _, bucket := range group.Buckets {
			if window, ok := antigravityWindow(bucket); ok {
				windows = append(windows, window)
			}
		}
		if len(windows) == 0 {
			return nil, fmt.Errorf("antigravity usage: no active Gemini quota buckets")
		}
		sort.SliceStable(windows, func(i, j int) bool {
			return windows[i].Label == claudeLabel5Hour && windows[j].Label != claudeLabel5Hour
		})
		return &ProviderUsage{Provider: "google", Windows: windows, FetchedAt: now}, nil
	}
	return nil, fmt.Errorf("antigravity usage: Gemini Models group missing")
}

func antigravityWindow(bucket antigravityBucket) (UtilizationWindow, bool) {
	if bucket.RemainingFraction == nil || *bucket.RemainingFraction < 0 || *bucket.RemainingFraction > 1 || math.IsNaN(*bucket.RemainingFraction) {
		return UtilizationWindow{}, false
	}
	resetAt, err := time.Parse(time.RFC3339, bucket.ResetTime)
	if err != nil {
		return UtilizationWindow{}, false // Disabled buckets have no reset time.
	}
	label, duration := antigravityWindowShape(bucket.Window)
	if label == "" {
		label = bucket.ID
	}
	window := UtilizationWindow{
		Label:          label,
		UtilizationPct: (1 - *bucket.RemainingFraction) * 100,
		ResetAt:        resetAt,
	}
	// A window the bucket report does not name keeps no duration, which leaves
	// it unknown for routing rather than scored against a guessed length.
	if duration > 0 {
		window.DurationSeconds = int64(duration / time.Second)
		window.StartAt = resetAt.Add(-duration)
	}
	return window, true
}

// antigravityWindowShape maps a reported bucket window to its display label and
// numeric length. An unrecognized window has no known length and reports zero.
func antigravityWindowShape(window string) (string, time.Duration) {
	switch window {
	case "5h":
		return claudeLabel5Hour, 5 * time.Hour
	case "weekly":
		return claudeLabel7Day, 7 * 24 * time.Hour
	default:
		return window, 0
	}
}
