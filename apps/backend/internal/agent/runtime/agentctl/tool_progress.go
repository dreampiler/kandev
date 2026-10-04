package client

import (
	"context"
	"fmt"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	ws "github.com/kandev/kandev/pkg/websocket"
)

// ProbeToolProgress returns no evidence when an older agentctl lacks this optional action.
func (c *Client) ProbeToolProgress(ctx context.Context, req streams.ToolProgressRequest) (streams.ToolProgressResponse, error) {
	var result streams.ToolProgressResponse
	resp, err := c.sendStreamRequest(ctx, "agent.tool.progress", req)
	if err != nil {
		return result, fmt.Errorf("tool progress request: %w", err)
	}
	if resp.Type != ws.MessageTypeResponse {
		return result, fmt.Errorf("tool progress unavailable")
	}
	if err := resp.ParsePayload(&result); err != nil {
		return streams.ToolProgressResponse{}, fmt.Errorf("tool progress response: %w", err)
	}
	return result, nil
}
