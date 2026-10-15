package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/kandev/kandev/internal/agentctl/server/utility"
)

// PrepareInferenceAttempt obtains a one-use token before any inference request.
// The client-generated preparation ID permits cleanup after a lost response.
func (c *Client) PrepareInferenceAttempt(ctx context.Context, callID string) (*utility.AttemptStatus, error) {
	preparationID := uuid.NewString()
	status, err := c.postInferenceObservation(ctx, "/api/v1/inference/attempts", utility.PrepareAttemptRequest{PreparationID: preparationID, CallID: callID})
	if err == nil {
		return status, nil
	}
	failure := &InferenceAttemptError{PreparationID: preparationID, NoPromptRequestSent: true, Cause: err}
	if errors.Is(err, ErrInferenceOwnershipUnsupported) {
		return nil, failure
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.httpClient.Timeout)
	defer cancel()
	if settled, cleanupErr := c.CancelInferencePreparation(cleanupCtx, preparationID); cleanupErr == nil {
		failure.Attempt = settled
	}
	return nil, failure
}

// CancelPreparedInferenceAttempt only settles an unconsumed token. It never
// stops, releases or relabels an already-started inference child.
func (c *Client) CancelPreparedInferenceAttempt(ctx context.Context, id string) (*utility.AttemptStatus, error) {
	return c.postInferenceObservation(ctx, "/api/v1/inference/attempts/"+url.PathEscape(id)+"/cancel", struct{}{})
}

func (c *Client) CancelInferencePreparation(ctx context.Context, id string) (*utility.AttemptStatus, error) {
	return c.postInferenceObservation(ctx, "/api/v1/inference/preparations/"+url.PathEscape(id)+"/cancel", struct{}{})
}

func (c *Client) postInferenceObservation(ctx context.Context, path string, body any) (*utility.AttemptStatus, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var status utility.AttemptStatus
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrInferenceOwnershipUnsupported
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return &status, fmt.Errorf("inference ownership request rejected (status %d)", resp.StatusCode)
	}
	return &status, nil
}
