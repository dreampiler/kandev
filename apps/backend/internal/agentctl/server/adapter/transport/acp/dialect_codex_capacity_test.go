package acp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

func TestCodexSystemErrorAndCapacityEvidence(t *testing.T) {
	for name, meta := range map[string]map[string]any{
		"top-level thread status": {
			"threadStatus": map[string]any{"type": codexSystemErrorType},
		},
		"codex thread status": {
			"codex": map[string]any{
				"threadStatus": map[string]any{"type": codexSystemErrorType},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if !codexSystemErrorMeta(meta) {
				t.Fatal("expected Codex system-error marker")
			}
		})
	}

	if codexSystemErrorMeta(map[string]any{
		"threadStatus": map[string]any{"type": "completed"},
	}) {
		t.Fatal("completed thread status must not be treated as a system error")
	}
	if !codexModelCapacityMessage("Selected   model is at capacity. Please try a different model.") {
		t.Fatal("expected normalized capacity message to match")
	}
	if codexModelCapacityMessage("The model completed successfully") {
		t.Fatal("ordinary model text must not match capacity evidence")
	}
}

func TestCodexQuotaNoticeCorrelatesWithGenericPromptError(t *testing.T) {
	a := newTestAdapter()
	a.agentID = codexAgentID
	t.Cleanup(func() { _ = a.Close() })
	_, turn := a.registerPromptTurn(context.Background(), 7)
	t.Cleanup(func() { a.clearPromptTurn(turn) })

	notice := "You’ve hit your usage limit. Try again at Sep 27th, 2026 3:09 AM."
	if a.observeCodexProviderEvidence(8, &AgentEvent{Type: streams.EventTypeMessageChunk, Text: notice, Role: "assistant", ProviderDiagnosticCandidate: true}) {
		t.Fatal("stale notice must not be suppressed")
	}
	a.observeCodexProviderEvidence(7, &AgentEvent{Type: streams.EventTypeMessageChunk, Text: notice, Role: "assistant", ProviderDiagnosticCandidate: true})
	terminal := &acp.RequestError{Code: -32603, Message: "Internal error"}
	if got := codexQuotaPromptError(turn, terminal); got != terminal {
		t.Fatalf("uncorrelated prompt error = %v, want original error", got)
	}
	a.observeCodexProviderEvidence(7, &AgentEvent{Type: streams.EventTypeSessionInfo, SessionMeta: map[string]any{"threadStatus": map[string]any{"type": codexSystemErrorType}}})
	got := codexQuotaPromptError(turn, terminal)
	var providerErr *providerPromptError
	if !errors.As(got, &providerErr) || providerErr.ProviderError.Source != streams.ProviderErrorSourceCodexACP {
		t.Fatalf("correlated prompt error = %v, want Codex provider error", got)
	}
	if providerErr.ProviderError.Message != streams.SanitizeProviderMessage(notice) {
		t.Fatalf("provider message = %q", providerErr.ProviderError.Message)
	}
	if providerErr.ProviderError.OccurredAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("provider occurrence time is in the future: %v", providerErr.ProviderError.OccurredAt)
	}
	var requestErr *acp.RequestError
	if !errors.As(got, &requestErr) || requestErr != terminal {
		t.Fatal("original ACP error metadata was lost")
	}
	projected := ProviderErrorFromError(got, codexAgentID, "")
	if projected == nil || projected.RPCCode != -32603 {
		t.Fatalf("provider projection = %+v, want original RPC code", projected)
	}
	classified := routingerr.Classify(routingerr.Input{
		Phase: routingerr.PhasePromptSend, ProviderID: codexAgentID,
		Stderr: got.Error(), ResetHint: projected.ResetAt,
	})
	if classified.Code != routingerr.CodeQuotaLimited || classified.ResetHint == nil {
		t.Fatalf("terminal error classification = %+v", classified)
	}
}

func TestCodexQuotaNoticeDoesNotSurviveOrdinaryOutput(t *testing.T) {
	a := newTestAdapter()
	a.agentID = codexAgentID
	t.Cleanup(func() { _ = a.Close() })
	_, turn := a.registerPromptTurn(context.Background(), 7)
	t.Cleanup(func() { a.clearPromptTurn(turn) })
	a.observeCodexProviderEvidence(7, &AgentEvent{Type: streams.EventTypeMessageChunk, Text: "You've hit your usage limit", Role: "assistant", ProviderDiagnosticCandidate: true})
	a.observeCodexProviderEvidence(7, &AgentEvent{Type: streams.EventTypeMessageChunk, Text: "I completed the work", Role: "assistant"})
	a.observeCodexProviderEvidence(7, &AgentEvent{Type: streams.EventTypeSessionInfo, SessionMeta: map[string]any{"threadStatus": map[string]any{"type": codexSystemErrorType}}})
	terminal := &acp.RequestError{Code: -32603, Message: "Internal error"}
	if got := codexQuotaPromptError(turn, terminal); got != terminal {
		t.Fatalf("ordinary output must clear quota evidence: %v", got)
	}
}

func TestObserveCodexProviderEvidenceRequiresMatchingPrompt(t *testing.T) {
	a := newTestAdapter()
	a.agentID = codexAgentID
	t.Cleanup(func() { _ = a.Close() })

	_, turn := a.registerPromptTurn(context.Background(), 7)
	t.Cleanup(func() { a.clearPromptTurn(turn) })

	if a.observeCodexProviderEvidence(8, &AgentEvent{
		Type:        streams.EventTypeSessionInfo,
		SessionMeta: map[string]any{"threadStatus": map[string]any{"type": codexSystemErrorType}},
	}) {
		t.Fatal("stale prompt evidence must be ignored")
	}
	if a.observeCodexProviderEvidence(7, &AgentEvent{
		Type:        streams.EventTypeSessionInfo,
		SessionMeta: map[string]any{"threadStatus": map[string]any{"type": codexSystemErrorType}},
	}) {
		t.Fatal("system-error metadata alone must not settle a capacity failure")
	}
	if !a.observeCodexProviderEvidence(7, &AgentEvent{
		Type: streams.EventTypeMessageChunk,
		Text: "Selected model is at capacity. Please try a different model.",
	}) {
		t.Fatal("matching system-error and capacity evidence must suppress the explanatory chunk")
	}
	if !turn.codexCapacityFailure() {
		t.Fatal("expected correlated evidence to mark the prompt as a capacity failure")
	}
}
