package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kandev/kandev/internal/agentctl/types"
)

// workspaceStreamInputEndTimeout bounds how long the handler may take to
// notice that its client is gone. The fix makes the return immediate; the
// bound only has to be long enough not to flake on a loaded machine.
const workspaceStreamInputEndTimeout = 3 * time.Second

// TestHandleWorkspaceStreamWS_ReturnsWhenClientLeavesWithoutEvents pins
// AC-PLATFORM-AGENTCTL-WORKSPACE-STREAM-LIFETIME-001.1: when the client closes
// the workspace stream and the instance publishes nothing afterwards, the
// handler (and its forwarding loop) must still return. Before the fix the
// forwarding loop only noticed a departed client on its next write, so a
// stopped instance - which never publishes again - kept the loop parked in its
// select forever and pinned the whole instance in memory.
//
// No event is published between the client close and the wait below. The
// cleanup falls back to publishing so a failing run still releases the handler
// before httptest.Server.Close waits on it.
func TestHandleWorkspaceStreamWS_ReturnsWhenClientLeavesWithoutEvents(t *testing.T) {
	srv := newTestServer(t)
	handlerReturned := make(chan struct{})
	var returnedOnce sync.Once
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer returnedOnce.Do(func() { close(handlerReturned) })
		srv.Router().ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)

	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/api/v1/workspace/stream", nil)
	if err != nil {
		t.Fatalf("dial workspace stream: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		waitForWorkspaceStreamShutdown(t, srv.procMgr.GetWorkspaceTracker().NotifyGitCommit, handlerReturned)
	})

	if got := readWorkspaceMessage(t, conn); got.Type != types.WorkspaceMessageTypeConnected {
		t.Fatalf("first frame type = %q, want %q", got.Type, types.WorkspaceMessageTypeConnected)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	select {
	case <-handlerReturned:
	case <-time.After(workspaceStreamInputEndTimeout):
		t.Fatalf("workspace stream handler still running %s after the client left with no further events; "+
			"the forwarding loop does not observe the end of the input side", workspaceStreamInputEndTimeout)
	}
}
