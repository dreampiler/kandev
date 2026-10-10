package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/managedruntime"
	"github.com/kandev/kandev/internal/agentruntime"
	"github.com/kandev/kandev/internal/task/models"
)

// The launch command follows the backend the caller resolved, not the
// requested executor type: a request with no executor type (or one that falls
// back to another backend) still runs on the resolved standalone backend, so a
// native OpenCode selection must yield its native command rather than an empty
// one.
func TestBuildExecutionUsesResolvedStandaloneForNativeOpenCode(t *testing.T) {
	// A deliberately invalid executable exercises the existing bounded probe
	// failure fallback without starting an agent or reading the host CLI.
	dir := t.TempDir()
	name := "opencode"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("invalid executable\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	manager := &Manager{commandBuilder: NewCommandBuilder(), logger: newTestLogger()}
	manager.SetManagedRuntimeSelectionStore(managedRuntimeOpenCodeStore{openCodeSelection: managedruntime.OpenCodeSelection{
		SchemaVersion: 1, Family: managedruntime.OpenCodeFamilyV1, Source: managedruntime.OpenCodeSourceNative,
		Package: "opencode-ai", AppliedDefaultVersion: "1.18.32", Revision: 1,
	}})
	agent := agents.NewOpenCodeACP()
	for _, executorType := range []string{"", string(models.ExecutorTypeLocal), string(models.ExecutorTypeLocalDocker)} {
		t.Run("requested_"+executorType, func(t *testing.T) {
			execution, err := manager.buildExecutionFromInstance(context.Background(),
				&LaunchRequest{ExecutorType: executorType},
				&ExecutorCreateRequest{ExecutorType: executorType, AgentConfig: agent},
				&ExecutorInstance{InstanceID: "test-instance"}, &StandaloneExecutor{}, nil, agent, nil)
			if err != nil {
				t.Fatalf("build resolved standalone execution: %v", err)
			}
			want := []string{"opencode", "acp", "--print-logs"}
			if execution.RuntimeName != agentruntime.RuntimeStandalone || !reflect.DeepEqual(execution.AgentArgs, want) {
				t.Fatalf("runtime=%s args=%q, want standalone native v1 command %q", execution.RuntimeName, execution.AgentArgs, want)
			}
		})
	}
}
