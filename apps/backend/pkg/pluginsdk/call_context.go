package pluginsdk

import (
	"context"
	"sync"

	"github.com/google/uuid"
	pluginv1 "github.com/kandev/kandev/proto/kandev/plugin/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const pluginCallMetadata = "x-kandev-plugin-call"

// HostCallContextPolicy is implemented by the host application. The SDK does
// not interpret task identities; it carries the original server context only.
type HostCallContextPolicy interface {
	HostCallContextEnabled() bool
	ValidateHostCallContext(context.Context, bool) error
}

type pluginCallOrigins struct {
	mu     sync.Mutex
	policy HostCallContextPolicy
	live   map[string]context.Context
}

func newPluginCallOrigins(host Host) *pluginCallOrigins {
	policy, _ := host.(HostCallContextPolicy)
	return &pluginCallOrigins{policy: policy, live: make(map[string]context.Context)}
}

func (o *pluginCallOrigins) enabled() bool {
	return o.policy != nil && o.policy.HostCallContextEnabled()
}

func (o *pluginCallOrigins) bind(ctx context.Context, agentTool bool) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := o.policy.ValidateHostCallContext(ctx, agentTool); err != nil {
		return nil, nil, status.Error(codes.PermissionDenied, "plugin caller context is unavailable")
	}
	origin, cancel := context.WithCancel(ctx)
	// Tool InvocationID is caller correlation data, not a unique lifetime.
	// A new server reference prevents late callbacks from following its reuse.
	reference := uuid.NewString()
	o.mu.Lock()
	o.live[reference] = origin
	o.mu.Unlock()
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(pluginCallMetadata, reference)
	return metadata.NewOutgoingContext(ctx, md), func() {
		o.mu.Lock()
		delete(o.live, reference)
		o.mu.Unlock()
		cancel()
	}, nil
}

// Plugin-owned startup storage/configuration keeps its existing capability
// checks. Every other (including future) Host operation requires an origin.
func hostOperationNeedsCaller(method string) bool {
	switch method {
	case pluginv1.Host_GetState_FullMethodName, pluginv1.Host_SetState_FullMethodName,
		pluginv1.Host_DeleteState_FullMethodName, pluginv1.Host_ListState_FullMethodName,
		pluginv1.Host_GetConfig_FullMethodName, pluginv1.Host_RevealSecret_FullMethodName,
		pluginv1.Host_GetSecret_FullMethodName, pluginv1.Host_SetSecret_FullMethodName,
		pluginv1.Host_DeleteSecret_FullMethodName, pluginv1.Host_EmitEvent_FullMethodName:
		return false
	default:
		return true
	}
}

func (o *pluginCallOrigins) intercept(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if !o.enabled() {
		return handler(ctx, req)
	}
	emitting := info.FullMethod == pluginv1.Host_EmitEvent_FullMethodName
	if !emitting && !hostOperationNeedsCaller(info.FullMethod) {
		return handler(ctx, req)
	}
	md, _ := metadata.FromIncomingContext(ctx)
	refs := md.Get(pluginCallMetadata)
	if len(refs) != 1 || refs[0] == "" {
		if emitting {
			return handler(unidentifiedHostCallContext(ctx), req)
		}
		return nil, status.Error(codes.PermissionDenied, "plugin Host call requires a live caller context")
	}
	o.mu.Lock()
	origin := o.live[refs[0]]
	o.mu.Unlock()
	if origin == nil || origin.Err() != nil {
		if emitting {
			return handler(unidentifiedHostCallContext(ctx), req)
		}
		return nil, status.Error(codes.PermissionDenied, "plugin caller context has expired")
	}
	if !emitting && origin.Value(unidentifiedHostCallKey{}) != nil {
		return nil, status.Error(codes.PermissionDenied, "plugin event caller context is unavailable")
	}
	call, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(origin, cancel)
	defer stop()
	defer cancel()
	caller := pluginCallerContext{Context: call, origin: origin}
	return handler(context.WithValue(caller, hostCallReferenceKey{}, refs[0]), req)
}

type pluginCallerContext struct {
	context.Context
	origin context.Context
}

func (c pluginCallerContext) Value(key any) any {
	if value := c.origin.Value(key); value != nil {
		return value
	}
	return c.Context.Value(key)
}

type pluginCallClientConn struct {
	grpc.ClientConnInterface
	origins *pluginCallOrigins
}

func (c pluginCallClientConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	if !c.origins.enabled() {
		return c.ClientConnInterface.Invoke(ctx, method, args, reply, opts...)
	}
	call, release, err := c.origins.bind(ctx, method == pluginv1.Plugin_InvokeAgentTool_FullMethodName)
	if err != nil {
		return err
	}
	defer release()
	return c.ClientConnInterface.Invoke(call, method, args, reply, opts...)
}

type hostCallClientConn struct{ grpc.ClientConnInterface }

func (c hostCallClientConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	// The plugin handler's incoming RPC context is the only origin source.
	// context.Background and JSON task fields cannot manufacture one.
	incoming, _ := metadata.FromIncomingContext(ctx)
	if refs := incoming.Get(pluginCallMetadata); len(refs) == 1 && refs[0] != "" {
		outgoing, _ := metadata.FromOutgoingContext(ctx)
		outgoing = outgoing.Copy()
		outgoing.Set(pluginCallMetadata, refs[0])
		ctx = metadata.NewOutgoingContext(ctx, outgoing)
	}
	return c.ClientConnInterface.Invoke(ctx, method, args, reply, opts...)
}
