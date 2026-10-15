package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/kandev/kandev/internal/agentctl/server/utility"
)

// InferenceAttemptError preserves the issued token when transport has no
// settlement response. Cancellation or an HTTP failure does not release it.
type InferenceAttemptError struct {
	AttemptID           string
	PreparationID       string
	NoPromptRequestSent bool
	Attempt             *utility.AttemptStatus
	Cause               error
}

var ErrInferenceOwnershipUnsupported = errors.New("agentctl inference ownership is unsupported")

func (e *InferenceAttemptError) Error() string { return e.Cause.Error() }
func (e *InferenceAttemptError) Unwrap() error { return e.Cause }

// InferenceAttempts observes the existing owner after backend reconnection.
// Any error, including an old agentctl's 404, leaves population unknown.
func (c *Client) InferenceAttempts(ctx context.Context) (*utility.AttemptSnapshot, error) {
	var snapshot utility.AttemptSnapshot
	if err := c.readInferenceObservation(ctx, "/api/v1/inference/attempts", &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// InferenceAttempt reads one token; missing or unretained tokens are unknown.
func (c *Client) InferenceAttempt(ctx context.Context, id string) (*utility.AttemptStatus, error) {
	var attempt utility.AttemptStatus
	if err := c.readInferenceObservation(ctx, "/api/v1/inference/attempts/"+url.PathEscape(id), &attempt); err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (c *Client) readInferenceObservation(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return ErrInferenceOwnershipUnsupported
		}
		return fmt.Errorf("inference observation unavailable (status %d)", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
