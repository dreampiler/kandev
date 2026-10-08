package acp

import (
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
)

// streamAssistantText feeds blocks as assistant message chunks of the current
// turn and reports the diagnostic the notice check queued, if any.
func streamAssistantText(t *testing.T, a *Adapter, turn *promptTurnState, blocks ...string) (providerNoticeDiagnostic, bool) {
	t.Helper()
	for _, block := range blocks {
		if event := a.convertMessageChunk("ses-current", acp.TextBlock(block), "assistant"); event == nil {
			t.Fatalf("block %q produced no event", block)
		}
	}
	select {
	case diagnostic := <-turn.providerErrorCh:
		return diagnostic, true
	default:
		return providerNoticeDiagnostic{}, false
	}
}

// An agent that reports other sessions' provider errors is doing its job; the
// words it writes are not its own provider's failure. These turns are the
// production reports that ended live Claude and OpenCode turns and suspended
// their accounts.
func TestAgentProseQuotingLimitErrorsDoesNotEndTheTurn(t *testing.T) {
	cases := []struct {
		name   string
		agent  string
		blocks []string
	}{
		{
			name:  "claude coordinator classifying stalled children",
			agent: claudeAgentID,
			blocks: []string{
				"완료 신호 없이 턴이 끝난 6개 작업을 하나씩 분류하겠습니다.",
				"6개 작업 모두 같은 유형입니다. 세션을 재개할 때 모델 호출이 실패해 턴이 끝났습니다.",
				"[→운영 세션] 막힘 — R4 자식 6개가 세션을 재개할 때 모델 호출이 실패" +
					"(`AI_APICallError: Not Found`·`Rate limit exceeded`)해 완료 신호 없이 멈췄습니다.",
			},
		},
		{
			name:  "claude quoting an exact provider error line",
			agent: claudeAgentID,
			blocks: []string{
				"The child's last message was `API Error: 429 rate_limit_error`, ",
				"so I recorded it and left the child on hold.",
			},
		},
		{
			name:  "opencode summarizing blocked work",
			agent: opencodeAgentID,
			blocks: []string{
				"Now I have a clear picture. The decision task is waiting for the owner. ",
				"Other tasks are stuck on technical issues (rate limits, model not found).",
			},
		},
		{
			name:   "opencode quoting another session's usage limit",
			agent:  opencodeAgentID,
			blocks: []string{"The parent session is quota-blocked (`Go usage limit exceeded`) and could not continue."},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAdapterForAgent(tc.agent)
			turn := installQuotaNoticeTurn(t, a)
			defer a.finishPromptTurn(turn)

			if diagnostic, ended := streamAssistantText(t, a, turn, tc.blocks...); ended {
				t.Fatalf("agent prose ended its own turn as a provider failure: %+v", diagnostic.ProviderError)
			}
		})
	}
}

// A provider that states its failure as the whole reply still ends the turn:
// the output opens with the provider's own signature.
func TestProviderNoticeOpeningTheTurnStillEndsIt(t *testing.T) {
	cases := []struct {
		name     string
		agent    string
		blocks   []string
		wantCode routingerr.Code
	}{
		{
			name:     "claude rate-limit error as the reply",
			agent:    claudeAgentID,
			blocks:   []string{"API Error: 429 ", `{"type":"error","error":{"type":"rate_limit_error"}}`},
			wantCode: routingerr.CodeRateLimited,
		},
		{
			name:     "opencode usage limit as the reply",
			agent:    opencodeAgentID,
			blocks:   []string{"AI_APICallError: Go usage limit exceeded"},
			wantCode: routingerr.CodeQuotaLimited,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestAdapterForAgent(tc.agent)
			turn := installQuotaNoticeTurn(t, a)
			defer a.finishPromptTurn(turn)

			diagnostic, ended := streamAssistantText(t, a, turn, tc.blocks...)
			if !ended {
				t.Fatal("provider notice opening the turn did not end it")
			}
			classified := routingerr.Classify(routingerr.Input{
				Phase: routingerr.PhasePromptSend, ProviderID: tc.agent, Stderr: diagnostic.ProviderError.Message,
			})
			if classified.Code != tc.wantCode {
				t.Fatalf("notice classified as %s (%s), want %s", classified.Code, classified.ClassifierRule, tc.wantCode)
			}
		})
	}
}
