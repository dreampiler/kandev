package main

import (
	"fmt"
	"strconv"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

const (
	shellStreamDefaultUpdates = 50
	shellStreamDefaultChunk   = 4096
	shellStreamDefaultDelayMS = 20
	shellStreamMaxUpdates     = 2000
	shellStreamMaxChunk       = 64 * 1024
	shellStreamMaxDelayMS     = 10_000
)

// executeSimulatedShellStream emits an execute-kind (shell) tool call that
// streams its output through many in-progress updates before completing, the
// way a long Codex shell command does. Each update carries one
// terminal_output_delta chunk, so the adapter's cumulative shell payload grows
// with every update.
// Format: e2e:shell_stream("<command>", <updates>, <chunk_bytes>, <delay_ms>)
func executeSimulatedShellStream(e *emitter, line string) {
	inner := extractParenContent(line, "e2e:shell_stream(")
	command, rest := extractFirstStringArg(inner)
	if command == "" {
		command = "mock-stream"
	}
	updates, chunkBytes, delayMS := parseShellStreamArgs(rest)

	toolID := nextToolID()
	e.startTool(toolID, command, acp.ToolKindExecute, map[string]any{rawInputCommandKey: command})
	var full strings.Builder
	for index := 0; index < updates; index++ {
		if e.ctx != nil && e.ctx.Err() != nil {
			return
		}
		chunk := shellStreamChunk(index, chunkBytes)
		full.WriteString(chunk)
		e.shellProgress(toolID, chunk)
		waitForDelay(e.ctx, delayMS)
	}
	e.completeTool(toolID, map[string]any{rawOutputKey: full.String()})
}

func parseShellStreamArgs(rest string) (int, int, int) {
	values := []int{shellStreamDefaultUpdates, shellStreamDefaultChunk, shellStreamDefaultDelayMS}
	limits := []int{shellStreamMaxUpdates, shellStreamMaxChunk, shellStreamMaxDelayMS}
	fields := strings.Split(strings.TrimPrefix(strings.TrimSpace(rest), ","), ",")
	for index := range values {
		if index >= len(fields) {
			break
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(fields[index]))
		if err != nil || parsed < 0 {
			continue
		}
		values[index] = min(parsed, limits[index])
	}
	if values[0] == 0 {
		values[0] = 1
	}
	if values[1] == 0 {
		values[1] = 1
	}
	return values[0], values[1], values[2]
}

// shellStreamChunk builds one deterministic output chunk of exactly size bytes.
func shellStreamChunk(index, size int) string {
	line := fmt.Sprintf("mock shell stream line %06d ", index)
	var chunk strings.Builder
	for chunk.Len() < size-1 {
		chunk.WriteString(line)
	}
	text := chunk.String()
	if len(text) > size-1 {
		text = text[:size-1]
	}
	return text + "\n"
}

// shellProgress emits an in-progress tool_call_update carrying one terminal
// output delta, matching the ACP terminal extension a real shell tool uses.
func (e *emitter) shellProgress(id acp.ToolCallId, delta string) {
	withUpdateMeta := func(meta map[string]any) acp.ToolCallUpdateOpt {
		return func(tu *acp.SessionToolCallUpdate) { tu.Meta = meta }
	}
	_ = e.conn.SessionUpdate(e.ctx, acp.SessionNotification{
		SessionId: e.sid,
		Update: acp.UpdateToolCall(id,
			acp.WithUpdateStatus(acp.ToolCallStatusInProgress),
			withUpdateMeta(map[string]any{"terminal_output_delta": map[string]any{"data": delta}}),
		),
	})
}
