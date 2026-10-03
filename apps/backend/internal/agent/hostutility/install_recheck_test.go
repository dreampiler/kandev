package hostutility

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/registry"
	agentctlclient "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	agentctlutil "github.com/kandev/kandev/internal/agentctl/server/utility"
	"github.com/kandev/kandev/internal/common/logger"
)

type fakeInstallSource struct {
	mu        sync.Mutex
	available bool
	known     bool
	calls     int
}

func (s *fakeInstallSource) AgentAvailability(context.Context, string) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.available, s.known, nil
}

func (s *fakeInstallSource) set(available bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.available = available
}

func (s *fakeInstallSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type notInstalledInferenceAgent struct {
	installedInferenceAgent
	calls atomic.Int32
}

func (a *notInstalledInferenceAgent) IsInstalled(context.Context) (*agents.DiscoveryResult, error) {
	a.calls.Add(1)
	return &agents.DiscoveryResult{Available: false}, nil
}

// newRecheckManager wires a manager to loopback agentctl stand-ins whose probe
// always succeeds, and counts the probes it receives.
func newRecheckManager(t *testing.T, log *logger.Logger, reg *registry.Registry) (*Manager, *atomic.Int32) {
	t.Helper()
	var probes atomic.Int32
	instanceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/inference/probe":
			probes.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(agentctlutil.ProbeResponse{
				Success:        true,
				AgentName:      "Recheck ACP",
				CurrentModelID: "test-model",
			}); err != nil {
				t.Errorf("encode probe response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(instanceServer.Close)
	_, instancePort := serverHostPort(t, instanceServer)

	controlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(agentctlclient.CreateInstanceResponse{
				ID:   "recheck-instance",
				Port: instancePort,
			}); err != nil {
				t.Errorf("encode create instance response: %v", err)
			}
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(controlServer.Close)
	controlHost, controlPort := serverHostPort(t, controlServer)

	mgr := NewManager(reg, controlHost, controlPort,
		agentctlclient.NewControlClient(controlHost, controlPort, log), log)
	return mgr, &probes
}

func waitForSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// The capability record and the discovery endpoints must give one answer, so
// bootstrap reads installation from the shared source rather than measuring
// the agent a second time.
// @covers AC-AGENTS-INSTALL-DETECTION-002.1
func TestBootstrapReadsInstallationFromSource(t *testing.T) {
	log := newTestLogger(t)
	reg := registry.NewRegistry(log)
	ag := &notInstalledInferenceAgent{installedInferenceAgent: installedInferenceAgent{id: "source-acp"}}
	require.NoError(t, reg.Register(ag))
	mgr, probes := newRecheckManager(t, log, reg)
	mgr.installRecheckDelay = time.Hour
	mgr.SetInstallationSource(&fakeInstallSource{available: true, known: true})

	require.NoError(t, mgr.Start(context.Background()))
	defer mgr.Stop(context.Background())

	caps, ok := mgr.Get("source-acp")
	require.True(t, ok)
	require.Equal(t, StatusOK, caps.Status)
	require.Equal(t, int32(1), probes.Load())
	require.Zero(t, ag.calls.Load(), "agent detection must not run when the source knows the answer")
}

// @covers AC-AGENTS-INSTALL-DETECTION-002.1
func TestBootstrapMeasuresAgentWhenSourceHasNoAnswer(t *testing.T) {
	log := newTestLogger(t)
	reg := registry.NewRegistry(log)
	ag := &notInstalledInferenceAgent{installedInferenceAgent: installedInferenceAgent{id: "unknown-acp"}}
	require.NoError(t, reg.Register(ag))
	mgr, _ := newRecheckManager(t, log, reg)
	mgr.installRecheckDelay = time.Hour
	mgr.SetInstallationSource(&fakeInstallSource{known: false})

	require.NoError(t, mgr.Start(context.Background()))
	defer mgr.Stop(context.Background())

	caps, ok := mgr.Get("unknown-acp")
	require.True(t, ok)
	require.Equal(t, StatusNotInstalled, caps.Status)
	require.Equal(t, int32(1), ag.calls.Load())
}

// A not_installed record written by one failed measurement is replaced as soon
// as discovery reports the agent available, and open pages are told.
// @covers AC-AGENTS-INSTALL-DETECTION-002.4
func TestRecheckNotInstalledRefreshesAgentDiscoveryFoundInstalled(t *testing.T) {
	log := newTestLogger(t)
	reg := registry.NewRegistry(log)
	const agentType = "recheck-acp"
	require.NoError(t, reg.Register(&installedInferenceAgent{id: agentType}))
	mgr, probes := newRecheckManager(t, log, reg)
	mgr.parentTmpDir = t.TempDir()
	mgr.SetInstallationSource(&fakeInstallSource{available: true, known: true})
	changed := make(chan struct{}, 1)
	mgr.SetCapabilityChangeListener(func() { changed <- struct{}{} })
	mgr.cache.set(AgentCapabilities{AgentType: agentType, Status: StatusNotInstalled, Error: "agent not installed"})

	mgr.RecheckNotInstalled([]string{agentType})
	waitForSignal(t, changed, "capability change notification")

	caps, ok := mgr.Get(agentType)
	require.True(t, ok)
	require.Equal(t, StatusOK, caps.Status)
	require.Equal(t, "test-model", caps.CurrentModelID)
	require.Equal(t, int32(1), probes.Load())
	mgr.Stop(context.Background())
}

// @covers AC-AGENTS-INSTALL-DETECTION-002.2
func TestRecheckNotInstalledLeavesOtherRecordsAlone(t *testing.T) {
	log := newTestLogger(t)
	reg := registry.NewRegistry(log)
	require.NoError(t, reg.Register(&installedInferenceAgent{id: "failed-acp"}))
	require.NoError(t, reg.Register(&installedInferenceAgent{id: "missing-acp"}))
	mgr, probes := newRecheckManager(t, log, reg)
	mgr.parentTmpDir = t.TempDir()
	source := &fakeInstallSource{available: false, known: true}
	mgr.SetInstallationSource(source)
	var notified atomic.Int32
	mgr.SetCapabilityChangeListener(func() { notified.Add(1) })
	mgr.cache.set(AgentCapabilities{AgentType: "failed-acp", Status: StatusFailed})
	mgr.cache.set(AgentCapabilities{AgentType: "missing-acp", Status: StatusNotInstalled})

	mgr.RecheckNotInstalled([]string{"failed-acp", "missing-acp"})
	mgr.Stop(context.Background())

	require.Equal(t, 1, source.callCount(), "only the not_installed record is re-measured")
	require.Zero(t, probes.Load())
	require.Zero(t, notified.Load(), "an agent still not installed is not a change")
	caps, _ := mgr.Get("failed-acp")
	require.Equal(t, StatusFailed, caps.Status)
	caps, _ = mgr.Get("missing-acp")
	require.Equal(t, StatusNotInstalled, caps.Status)
}

// With no page open there is no discovery sweep to trigger a re-measurement,
// so an agent the boot measurement missed is measured once more after a delay.
// @covers AC-AGENTS-INSTALL-DETECTION-002.3
func TestStartRemeasuresNotInstalledAgentsAfterDelay(t *testing.T) {
	log := newTestLogger(t)
	reg := registry.NewRegistry(log)
	const agentType = "late-acp"
	require.NoError(t, reg.Register(&installedInferenceAgent{id: agentType}))
	mgr, probes := newRecheckManager(t, log, reg)
	mgr.installRecheckDelay = 200 * time.Millisecond
	source := &fakeInstallSource{available: false, known: true}
	mgr.SetInstallationSource(source)
	changed := make(chan struct{}, 1)
	mgr.SetCapabilityChangeListener(func() { changed <- struct{}{} })

	require.NoError(t, mgr.Start(context.Background()))
	defer mgr.Stop(context.Background())
	caps, ok := mgr.Get(agentType)
	require.True(t, ok)
	require.Equal(t, StatusNotInstalled, caps.Status)

	source.set(true)
	waitForSignal(t, changed, "delayed re-measurement")
	caps, _ = mgr.Get(agentType)
	require.Equal(t, StatusOK, caps.Status)
	require.Equal(t, int32(1), probes.Load())
}

// @covers AC-AGENTS-INSTALL-DETECTION-002.7
func TestStopCancelsPendingInstallRecheck(t *testing.T) {
	log := newTestLogger(t)
	reg := registry.NewRegistry(log)
	require.NoError(t, reg.Register(&installedInferenceAgent{id: "pending-acp"}))
	mgr, _ := newRecheckManager(t, log, reg)
	mgr.installRecheckDelay = time.Hour
	source := &fakeInstallSource{available: false, known: true}
	mgr.SetInstallationSource(source)

	require.NoError(t, mgr.Start(context.Background()))
	stopped := make(chan struct{})
	go func() {
		mgr.Stop(context.Background())
		close(stopped)
	}()
	waitForSignal(t, stopped, "Stop with a pending re-measurement")
	require.Equal(t, 1, source.callCount(), "the pending re-measurement must not run after Stop")

	mgr.RecheckNotInstalled([]string{"pending-acp"})
	require.Equal(t, 1, source.callCount(), "nothing starts after Stop")
}
