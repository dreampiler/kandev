package instance

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/server/config"
	"github.com/kandev/kandev/internal/agentctl/server/process"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/pkg/agent"
)

// TestConcurrentCreationsOverlapStartup pins the fix for the standalone
// control-API timeout: startup (process manager, comparison targets, workspace
// trackers) must not hold m.mu, or N concurrent creates queue behind one slow
// startup and the 30s control client gives up before the last instance exists.
//
// The afterTrackerStart seam blocks each creation at the end of its startup,
// which is exactly the window that used to be inside the lock. Under the old
// code only the first creation could reach it; the rest would be waiting on
// m.mu and the barrier would never fill.
func TestConcurrentCreationsOverlapStartup(t *testing.T) {
	log := newTestLogger(t)
	mgr := NewManager(&config.Config{
		Ports:    config.PortConfig{Base: 42500, Max: 42520},
		Defaults: config.InstanceDefaults{Protocol: agent.ProtocolACP},
	}, log)
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	mgr.SetServerFactory(func(*config.InstanceConfig, *process.Manager, *logger.Logger) http.Handler {
		return http.NotFoundHandler()
	})

	const concurrent = 4
	dirs := make([]string, concurrent)
	for i := range dirs {
		dirs[i] = t.TempDir()
	}

	var reached atomic.Int32
	allReached := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	mgr.afterTrackerStart = func() {
		if reached.Add(1) == concurrent {
			releaseOnce.Do(func() { close(allReached) })
		}
		<-release
	}

	var wg sync.WaitGroup
	errs := make([]error, concurrent)
	for i := range concurrent {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = mgr.CreateInstance(context.Background(), &CreateRequest{
				ID:            fmt.Sprintf("concurrent-%d", i),
				WorkspacePath: dirs[i],
			})
		}(i)
	}

	select {
	case <-allReached:
	case <-time.After(5 * time.Second):
		close(release)
		wg.Wait()
		t.Fatalf("only %d/%d creations reached the startup barrier; creation still serializes on m.mu",
			reached.Load(), concurrent)
	}
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("CreateInstance %d failed: %v", i, err)
		}
	}
}
