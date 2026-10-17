package usage

import (
	"os"
	"runtime"
	"strings"
)

// ExecutionUsageInput is installed by the runtime owner from the final execution
// snapshot. It is never decoded from an API payload or inferred from a profile.
type ExecutionUsageInput struct {
	WorkDir                                              string            `json:"-"`
	Command                                              []string          `json:"-"`
	ExecutionID, ProfileID, AgentID, ExecutorType, Model string            `json:"-"`
	ExpectedAccountRef, ExpectedOrganizationRef          string            `json:"-"`
	Environment                                          map[string]string `json:"-"`
}

type ExecutionUsage struct {
	SourceKnown bool
	Reason      string
	Current     *ProviderUsage
	Previous    *ProviderUsage
}

type executionAccount struct{ account, organization string }

type executionUsageSource struct{ provider, path, token string }

// CaptureExecutionEnvironment retains credential-affecting effective values
// only. Local execution inherits this process; remote execution cannot borrow it.
func CaptureExecutionEnvironment(executor string, overrides map[string]string) map[string]string {
	if !localUsageExecution(executor) || overrides == nil {
		return nil
	}
	result := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && usageEnvironmentKey(key) {
			result[usageEnvironmentName(key)] = value
		}
	}
	for key, value := range overrides {
		if usageEnvironmentKey(key) {
			result[usageEnvironmentName(key)] = value
		}
	}
	return result
}

func localUsageExecution(executor string) bool {
	switch executor {
	case "local", "local_pc", "worktree", "standalone":
		return true
	default:
		return false
	}
}

func usageEnvironmentName(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func usageEnvironmentKey(key string) bool {
	key = usageEnvironmentName(key)
	return key == "HOME" || key == "USERPROFILE" || key == "XDG_CONFIG_HOME" ||
		strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") ||
		strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "CODEX_")
}

const (
	executionSelectionUnknown = "provider_selection_unknown"
	executionAgentCodex       = "codex-acp"
	executionAgentClaude      = "claude-acp"
	executionProviderOpenAI   = "openai"
)
