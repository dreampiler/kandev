package pluginsdk

import (
	"context"
	"sync"
	"testing"
	"time"

	hcplugin "github.com/hashicorp/go-plugin"
	pluginv1 "github.com/kdlbs/kandev/pluginsdk/proto/kandev/plugin/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type classifiedHost struct {
	*recordingHost
	enabled bool
}

func (h *classifiedHost) HostCallContextEnabled() bool { return h.enabled }

func (h *classifiedHost) ValidateHostCallContext(context.Context, bool) error { return nil }

type hostCallProbe struct {
	UnimplementedPlugin
	mu      sync.Mutex
	host    Host
	results chan probeResult
}

type probeResult struct {
	call string
	err  error
}

func (p *hostCallProbe) SetHost(host Host) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.host = host
}

func (p *hostCallProbe) OnEvent(ctx context.Context, event *Event) error {
	p.mu.Lock()
	host := p.host
	p.mu.Unlock()
	if host == nil {
		return status.Error(codes.Unavailable, "host not injected")
	}
	call, _ := event.Payload["call"].(string)
	use := ctx
	switch call {
	case "background-preview", "background-task", "dropped-task":
		use = context.Background()
	}
	var err error
	if call == "background-preview" {
		manager, ok := PluginOwnedTaskTrees(host)
		if !ok {
			return status.Error(codes.Unavailable, "plugin-owned task trees are unavailable")
		}
		_, err = manager.Preview(use, "root")
	} else {
		_, err = host.Tasks().Get(use, "task-1")
	}
	p.results <- probeResult{call: call, err: err}
	return nil
}

func TestHostCallContextClassificationCoversHostService(t *testing.T) {
	methods := map[string]struct{}{}
	for _, method := range pluginv1.Host_ServiceDesc.Methods {
		methods["/"+pluginv1.Host_ServiceDesc.ServiceName+"/"+method.MethodName] = struct{}{}
	}
	for _, stream := range pluginv1.Host_ServiceDesc.Streams {
		methods["/"+pluginv1.Host_ServiceDesc.ServiceName+"/"+stream.StreamName] = struct{}{}
	}
	for method := range methods {
		_, required := hostCallCallerRequired[method]
		_, exempt := hostCallCallerExempt[method]
		require.NotEqual(t, required, exempt, "%s must be classified exactly once", method)
	}
	for method := range hostCallCallerRequired {
		require.Contains(t, methods, method, "required %s is not a Host method", method)
	}
	for method := range hostCallCallerExempt {
		require.Contains(t, methods, method, "exempt %s is not a Host method", method)
	}
}

func TestHostCallContextClassifiesCapabilityVersusConversationCalls(t *testing.T) {
	for _, method := range []string{
		pluginv1.Host_PreviewPluginOwnedTaskTree_FullMethodName,
		pluginv1.Host_ListManagedAgentConversationsExact_FullMethodName,
		pluginv1.Host_GetState_FullMethodName,
	} {
		require.False(t, hostOperationNeedsCaller(method), "%s keeps native capability authority", method)
	}
	for _, method := range []string{
		pluginv1.Host_ListTasksExact_FullMethodName,
		pluginv1.Host_UpdateTaskExact_FullMethodName,
		pluginv1.Host_SendMessage_FullMethodName,
	} {
		require.True(t, hostOperationNeedsCaller(method), "%s carries conversation domain authority", method)
	}
}

func TestHostCallContextBackgroundKeepsCapabilityAuthority(t *testing.T) {
	remote, plugin := newHostCallProbeHarness(t, true)
	require.NoError(t, runHostCallProbe(t, remote, plugin, "background-preview"))
}

func TestHostCallContextBackgroundDomainCallsStayClosed(t *testing.T) {
	remote, plugin := newHostCallProbeHarness(t, true)
	err := runHostCallProbe(t, remote, plugin, "background-task")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestHostCallContextDroppedHandlerContextStaysClosed(t *testing.T) {
	remote, plugin := newHostCallProbeHarness(t, true)
	err := runHostCallProbe(t, remote, plugin, "dropped-task")
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestHostCallContextForwardedHandlerContextReachesHost(t *testing.T) {
	remote, plugin := newHostCallProbeHarness(t, true)
	err := runHostCallProbe(t, remote, plugin, "forward-task")
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestHostCallContextOffKeepsNativeBackground(t *testing.T) {
	remote, plugin := newHostCallProbeHarness(t, false)
	err := runHostCallProbe(t, remote, plugin, "background-task")
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func newHostCallProbeHarness(t *testing.T, enabled bool) (*RemotePlugin, *hostCallProbe) {
	t.Helper()
	host := &classifiedHost{recordingHost: &recordingHost{taskTrees: recordingTaskTreeManager{}}, enabled: enabled}
	plugin := &hostCallProbe{}
	gp := &GRPCPlugin{Impl: plugin, Host: host, HostDialTimeout: 5 * time.Second}
	client, server := hcplugin.TestPluginGRPCConn(t, false, map[string]hcplugin.Plugin{
		PluginMapKey: gp,
	})
	t.Cleanup(func() {
		_ = client.Close()
		server.Stop()
	})
	raw, err := client.Dispense(PluginMapKey)
	require.NoError(t, err)
	remote, ok := raw.(*RemotePlugin)
	require.True(t, ok, "Dispense(%q) should return *RemotePlugin, got %T", PluginMapKey, raw)
	require.Eventually(t, func() bool {
		plugin.mu.Lock()
		defer plugin.mu.Unlock()
		return plugin.host != nil
	}, 5*time.Second, 10*time.Millisecond, "plugin should receive the Host broker client")
	return remote, plugin
}

func runHostCallProbe(t *testing.T, remote *RemotePlugin, plugin *hostCallProbe, call string) error {
	t.Helper()
	plugin.results = make(chan probeResult, 1)
	require.NoError(t, remote.DeliverEvent(context.Background(), &Event{
		EventID: call, EventType: "probe", Payload: map[string]any{"call": call},
	}))
	select {
	case result := <-plugin.results:
		require.Equal(t, call, result.call)
		return result.err
	case <-time.After(5 * time.Second):
		t.Fatal("probe timed out")
		return nil
	}
}
