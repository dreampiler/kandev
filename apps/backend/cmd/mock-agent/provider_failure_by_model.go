package main

import (
	"context"
	"regexp"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// providerFailureByModelCommand makes every session running one model fail
// its prompt with a provider error, while sessions on any other model answer
// normally. It reproduces a model host that is down for one model only, so a
// dynamic profile's failover can be observed: the failing candidate, the same
// model's other candidates, and the candidate that finally answers.
//
// The command is not anchored, because a dynamic launch appends its
// continuation package to the prompt.
//
//	/provider-failure:<model>:<kind>
//
// kind is "unavailable" (a generic temporarily-unavailable notice),
// "header-timeout" (a model host that never sent response headers) or
// "midturn-unavailable" (the unavailable notice after some output, which
// interrupts a turn already under way).
var providerFailureByModelCommand = regexp.MustCompile(
	`(?i)/(?:e2e:)?provider-failure:([a-z0-9._\[\]-]+):(unavailable|header-timeout|midturn-unavailable)`,
)

var providerFailureMessages = map[string]string{
	"unavailable":         "Internal error: Upstream error: service temporarily unavailable",
	"header-timeout":      "ProviderHeaderTimeoutError: Provider response headers timed out after 300000ms",
	"midturn-unavailable": "Internal error: Upstream error: service temporarily unavailable",
}

type providerFailureByModel struct {
	model string
	kind  string
}

func parseProviderFailureByModel(prompt string) (providerFailureByModel, bool) {
	matches := providerFailureByModelCommand.FindStringSubmatch(stripKandevSystem(strings.TrimSpace(prompt)))
	if matches == nil {
		return providerFailureByModel{}, false
	}
	return providerFailureByModel{model: matches[1], kind: strings.ToLower(matches[2])}, true
}

func (a *mockAgent) handleProviderFailureByModel(
	ctx context.Context,
	sid acp.SessionId,
	prompt string,
) (acp.PromptResponse, error, bool) {
	scenario, ok := parseProviderFailureByModel(prompt)
	if !ok {
		return acp.PromptResponse{}, nil, false
	}
	model := a.sessionModel(sid)
	if !strings.EqualFold(model, scenario.model) {
		(&emitter{ctx: ctx, conn: a.conn, sid: sid}).text("Provider failure scenario: " + model + " answered.")
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil, true
	}
	if scenario.kind == "midturn-unavailable" {
		(&emitter{ctx: ctx, conn: a.conn, sid: sid}).text("Partial work before the provider dropped the turn.")
	}
	return acp.PromptResponse{}, &acp.RequestError{
		Code:    -32603,
		Message: providerFailureMessages[scenario.kind],
		Data:    map[string]any{"errorKind": "server_error"},
	}, true
}
