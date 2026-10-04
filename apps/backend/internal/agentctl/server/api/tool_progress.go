package api

import (
	"context"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	ws "github.com/kandev/kandev/pkg/websocket"
)

func (s *Server) handleWSToolProgress(ctx context.Context, msg *ws.Message) *ws.Message {
	var req streams.ToolProgressRequest
	if err := msg.ParsePayload(&req); err != nil || req.SessionID == "" || len(req.ToolCallIDs) > 64 {
		resp, _ := ws.NewError(msg.ID, msg.Action, ws.ErrorCodeBadRequest, "invalid tool progress request", nil)
		return resp
	}
	if s.procMgr == nil {
		resp, _ := ws.NewError(msg.ID, msg.Action, ws.ErrorCodeInternalError, "agent not running", nil)
		return resp
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := s.procMgr.ProbeToolProgress(ctx, req)
	resp, _ := ws.NewResponse(msg.ID, msg.Action, result)
	return resp
}
