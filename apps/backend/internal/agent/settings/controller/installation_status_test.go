package controller

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agent/agents"
	"github.com/kandev/kandev/internal/agent/discovery"
	"github.com/kandev/kandev/internal/agent/hostutility"
	"github.com/kandev/kandev/internal/agent/registry"
	agentctlclient "github.com/kandev/kandev/internal/agent/runtime/agentctl"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/pkg/agent"
)

// lateInstalledAgent is an ACP agent whose installation check fails until the
// test flips it, standing in for a check that lost a race with boot load.
type lateInstalledAgent struct {
	testAgent
	installed atomic.Bool
}

func (a *lateInstalledAgent) IsInstalled(context.Context) (*agents.DiscoveryResult, error) {
	return &agents.DiscoveryResult{Available: a.installed.Load()}, nil
}

func (a *lateInstalledAgent) InferenceConfig() *agents.InferenceConfig {
	return &agents.InferenceConfig{Supported: true, Command: agents.NewCommand(a.id)}
}

func TestAvailableAgentNamesKeepsOnlyAvailable(t *testing.T) {
	got := availableAgentNames([]discovery.Availability{
		{Name: "hermes-acp", Available: true},
		{Name: "missing-acp", Available: false},
		{Name: "claude-acp", Available: true},
	})
	if len(got) != 2 || got[0] != "hermes-acp" || got[1] != "claude-acp" {
		t.Fatalf("available names = %v, want [hermes-acp claude-acp]", got)
	}
}

// The capability record must not keep a not_installed answer that discovery
// has since corrected: the next sweep that finds the agent re-measures it.
// @covers AC-AGENTS-INSTALL-DETECTION-002.2
func TestDiscoverySweepRemeasuresNotInstalledCapability(t *testing.T) {
	log, err := logger.NewLogger(logger.LoggingConfig{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	reg := registry.NewRegistry(log)
	ag := &lateInstalledAgent{testAgent: testAgent{
		id:      "late-acp",
		name:    "late-acp",
		enabled: true,
		runtime: &agents.RuntimeConfig{Protocol: agent.ProtocolACP},
	}}
	if err := reg.Register(ag); err != nil {
		t.Fatalf("register: %v", err)
	}
	disc, err := discovery.LoadRegistry(context.Background(), reg, log)
	if err != nil {
		t.Fatalf("load discovery: %v", err)
	}

	// Instance creation is refused, so a re-measurement ends in "failed": the
	// assertion is that the record leaves not_installed, not that a probe ran.
	controlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "refused", http.StatusInternalServerError)
	}))
	defer controlServer.Close()
	host, portText, err := net.SplitHostPort(controlServer.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	mgr := hostutility.NewManager(reg, host, port, agentctlclient.NewControlClient(host, port, log), log)
	defer mgr.Stop(context.Background())

	ctrl := &Controller{agentRegistry: reg, logger: log, discovery: disc}
	ctrl.SetHostUtility(mgr)

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("start host utility: %v", err)
	}
	if caps, _ := mgr.Get("late-acp"); caps.Status != hostutility.StatusNotInstalled {
		t.Fatalf("boot status = %q, want not_installed", caps.Status)
	}

	ag.installed.Store(true)
	disc.InvalidateCache()
	if _, err := disc.Detect(context.Background()); err != nil {
		t.Fatalf("detect: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		caps, _ := mgr.Get("late-acp")
		if caps.Status == hostutility.StatusFailed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("status after discovery found the agent = %q, want it re-measured", caps.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
