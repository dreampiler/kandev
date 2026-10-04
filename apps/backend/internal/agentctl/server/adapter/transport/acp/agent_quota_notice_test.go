package acp

import (
	"errors"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
	"github.com/kandev/kandev/internal/agentctl/types/streams"
)

// antigravityQuotaNotice is the quota notice agy states as ordinary assistant
// text and then never settles the prompt for.
const antigravityQuotaNotice = "Usage Limit Reached\n\nYou have reached your current quota for this period. Your limit will reset in 1 hour, 28 minutes."

// installQuotaNoticeTurn puts the adapter in the state of a prompt that is
// waiting on a session/prompt reply which the provider will never send.
func installQuotaNoticeTurn(t *testing.T, a *Adapter) *promptTurnState {
	t.Helper()
	_, turn := newPromptTurnState(t.Context(), 1, false)
	a.promptTurnMu.Lock()
	a.promptTurn = turn
	a.promptTurnMu.Unlock()
	turn.gateOwned = true
	a.promptGate <- struct{}{}
	return turn
}

func TestQuotaNoticeStatedAsAgentMessageReleasesTheTurn(t *testing.T) {
	a := newTestAdapterForAgent("antigravity-acp")
	a.cancelJoinTimeout = 20 * time.Millisecond
	turn := installQuotaNoticeTurn(t, a)

	// The provider streams the notice in blocks, so the turn must settle once
	// the accumulated text forms the whole notice, not on the first block.
	for _, block := range []string{
		"Usage Limit Reached\n\n",
		"You have reached your current quota for this period. ",
		"Your limit will reset in 1 hour, 28 minutes.",
	} {
		if event := a.convertMessageChunk("ses-current", acp.TextBlock(block), "assistant"); event == nil {
			t.Fatalf("block %q produced no event", block)
		}
	}

	done := make(chan error, 1)
	go func() { done <- a.waitForPromptRPCAfterUserCancel(turn, "ses-current") }()

	select {
	case err := <-done:
		var providerErr *providerPromptError
		if !errors.As(err, &providerErr) {
			t.Fatalf("error = %v, want providerPromptError", err)
		}
		if providerErr.ProviderError.Source != streams.ProviderErrorSourceAgentMessage {
			t.Fatalf("source = %q, want %q", providerErr.ProviderError.Source, streams.ProviderErrorSourceAgentMessage)
		}
		if providerErr.ProviderError.ProviderID != "antigravity-acp" {
			t.Fatalf("provider id = %q, want antigravity-acp", providerErr.ProviderError.ProviderID)
		}
		if providerErr.ProviderError.ResetAt == nil {
			t.Fatal("reset time from the notice was not carried into the diagnostic")
		}
		if got := providerErr.ProviderError.Message; got == "" || len(got) > streams.MaxProviderMessageBytes {
			t.Fatalf("message = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("quota notice did not settle the stalled prompt turn")
	}

	// The session-ceiling slot must be free for the next prompt once the turn
	// finishes, or the notice only moves the stall to the following prompt.
	a.finishPromptTurn(turn)
	if a.currentPromptTurn() != nil {
		t.Fatal("prompt turn still installed after finish")
	}
	select {
	case a.promptGate <- struct{}{}:
		<-a.promptGate
	default:
		t.Fatal("prompt gate not released")
	}
}

func TestQuotaNoticeInAUserChunkDoesNotEndTheTurn(t *testing.T) {
	a := newTestAdapterForAgent("antigravity-acp")
	turn := installQuotaNoticeTurn(t, a)
	defer a.finishPromptTurn(turn)

	if event := a.convertMessageChunk("ses-current", acp.TextBlock(antigravityQuotaNotice), acpUserRole); event == nil {
		t.Fatal("user chunk produced no event")
	}
	select {
	case diagnostic := <-turn.providerErrorCh:
		t.Fatalf("user chunk produced a diagnostic: %+v", diagnostic)
	default:
	}
}

func TestOrdinaryAssistantTextMentioningALimitDoesNotEndTheTurn(t *testing.T) {
	a := newTestAdapterForAgent("antigravity-acp")
	turn := installQuotaNoticeTurn(t, a)
	defer a.finishPromptTurn(turn)

	const prose = "The provider said 'Usage Limit Reached' yesterday; I raised the timeout to 60s."
	if event := a.convertMessageChunk("ses-current", acp.TextBlock(prose), "assistant"); event == nil {
		t.Fatal("assistant chunk produced no event")
	}
	select {
	case diagnostic := <-turn.providerErrorCh:
		t.Fatalf("prose produced a diagnostic: %+v", diagnostic)
	default:
	}
}

func TestQuotaNoticeWithNoActiveTurnIsIgnored(t *testing.T) {
	a := newTestAdapterForAgent("antigravity-acp")

	if event := a.convertMessageChunk("ses-idle", acp.TextBlock(antigravityQuotaNotice), "assistant"); event == nil {
		t.Fatal("quota notice produced no event")
	}
}
