package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// UsageSourceMeasuredCall marks an observation taken by running a minimal probe
// command against the provider CLI.
const UsageSourceMeasuredCall = "measured_call"

// ClaudeMeasuredReadTimeout is the maximum duration a measured probe invocation may take.
const ClaudeMeasuredReadTimeout = 60 * time.Second

// ClaudeStreamMessage models an event line emitted by the Claude CLI in stream-json mode.
type ClaudeStreamMessage struct {
	Type          string                `json:"type"`
	RateLimitInfo *ClaudeRateLimitEvent `json:"rate_limit_info,omitempty"`
}

// ClaudeRateLimitEvent models the rate limit payload emitted on the rate_limit_event stream.
type ClaudeRateLimitEvent struct {
	Status         string                                `json:"status"`
	RateLimitType  string                                `json:"rateLimitType,omitempty"`
	UnifiedWindows map[string]ClaudeUnifiedWindowPayload `json:"unifiedWindows,omitempty"`
}

// ClaudeUnifiedWindowPayload is the per-window utilization info inside unifiedWindows.
type ClaudeUnifiedWindowPayload struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    int64   `json:"resetsAt"` // Unix timestamp in seconds
	Status      string  `json:"status"`
}

// ParseClaudeStreamRateLimits scans lines of stream-json output from the Claude CLI
// and extracts all observed rate limit windows.
func ParseClaudeStreamRateLimits(scanner *bufio.Scanner, now time.Time) []ObservedWindow {
	var collected []ObservedWindow
	seen := make(map[string]bool)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, `"rate_limit_event"`) {
			continue
		}
		var msg ClaudeStreamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Type != "rate_limit_event" || msg.RateLimitInfo == nil {
			continue
		}
		info := msg.RateLimitInfo
		if len(info.UnifiedWindows) > 0 {
			for winType, payload := range info.UnifiedWindows {
				if seen[winType] {
					continue
				}
				seen[winType] = true
				var resetsAt time.Time
				if payload.ResetsAt > 0 {
					resetsAt = time.Unix(payload.ResetsAt, 0)
				}
				collected = append(collected, ObservedWindow{
					Provider:    claudeProvider,
					WindowType:  winType,
					Utilization: payload.Utilization,
					ResetsAt:    resetsAt,
					Status:      payload.Status,
					ObservedAt:  now,
					Source:      UsageSourceMeasuredCall,
				})
			}
		}
	}
	return collected
}

// ClaudeMeasuredReader defines the signature for invoking a measured probe.
type ClaudeMeasuredReader func(ctx context.Context, token string) ([]ObservedWindow, error)

// ExecuteClaudeMeasuredRead invokes the Claude CLI once with a minimal token query
// to capture rate limit windows from its stream-json output.
func ExecuteClaudeMeasuredRead(ctx context.Context, token string) ([]ObservedWindow, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("cannot execute claude measured read: empty oauth token")
	}

	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude executable not found in PATH: %w", err)
	}

	subCtx, cancel := context.WithTimeout(ctx, ClaudeMeasuredReadTimeout)
	defer cancel()

	args := []string{
		"-p", "ok",
		"--output-format", "stream-json",
		"--verbose",
		"--tools", "",
		"--no-session-persistence",
		"--strict-mcp-config",
		"--safe-mode",
		"--model", "claude-haiku-4-5",
	}

	cmd := exec.CommandContext(subCtx, claudeBin, args...)
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = hideWindowSysProcAttr()

	// Environment isolation: pass token and safe system variables only.
	env := []string{
		ClaudeOAuthTokenEnv + "=" + token,
	}
	for _, k := range []string{"PATH", "Path", "SYSTEMROOT", "SystemRoot", "COMSPEC", "ComSpec", "TEMP", "TMP", "USERPROFILE", "HOME", "APPDATA", "LOCALAPPDATA"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude command: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	now := time.Now()
	windows := ParseClaudeStreamRateLimits(scanner, now)

	_ = cmd.Wait()

	if len(windows) == 0 {
		return nil, errors.New("claude measured read did not produce rate limit windows")
	}

	return windows, nil
}
