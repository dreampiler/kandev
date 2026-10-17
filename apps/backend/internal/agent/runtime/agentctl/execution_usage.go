package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/kandev/kandev/internal/agent/runtime/routingerr"
	"github.com/kandev/kandev/internal/agent/usage"
)

func (c *Client) ConfigureAgentWithUsage(ctx context.Context, command string, args []string, env map[string]string, continueCommand string, continueArgs []string, policy *usage.ExecutionUsagePolicy) error {
	return c.configureAgent(ctx, command, args, env, continueCommand, continueArgs, false, policy)
}

func (c *Client) ReadExecutionUsage(ctx context.Context, model string) (*usage.ExecutionUsageReport, error) {
	var report usage.ExecutionUsageReport
	request, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/agent/usage?model="+url.QueryEscape(model), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("execution usage read failed: status %d", response.StatusCode)
	}
	body, err := readResponseBody(response)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("invalid execution usage report: %w", err)
	}
	return &report, nil
}

func executionUsageAdmissionError(report *usage.ExecutionUsageReport) error {
	if report == nil {
		return fmt.Errorf("execution usage observation unavailable")
	}
	if report.BindingChanged {
		return &usage.ExecutionUsageBindingError{}
	}
	if !report.Limited {
		return nil
	}
	return &routingerr.Error{Code: routingerr.CodeQuotaLimited, Class: routingerr.ClassHard, CatalogueVersion: routingerr.CatalogueVersion, Confidence: routingerr.ConfHigh, Phase: routingerr.PhasePromptSend, FallbackAllowed: true, AutoRetryable: report.ResetAt != nil, ResetHint: report.ResetAt, ClassifierRule: "native_measured_usage_limit"}
}

func (c *Client) CheckExecutionUsage(ctx context.Context, model string) error {
	report, err := c.ReadExecutionUsage(ctx, model)
	if err != nil {
		return err
	}
	return executionUsageAdmissionError(report)
}
