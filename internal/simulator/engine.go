// Package simulator sends labeled normal and fraud traffic to a running
// engine and reports how each scenario was decided (SP06), or measures its
// latency under load (D-035).
package simulator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/bescobarm/card-auth-engine/internal/domain"
	"github.com/bescobarm/card-auth-engine/internal/lifecycle"
)

const (
	pathAuthorize   = "/v1/authorizations"
	fmtPathEvent    = "/v1/authorizations/%s/%s"
	fmtPathPolicy   = "/v1/companies/%s/policy"
	headerType      = "Content-Type"
	contentTypeJSON = "application/json"
	maxErrorBody    = 512
)

// ErrUnexpectedStatus means the engine answered with a status the simulator didn't expect.
var ErrUnexpectedStatus = errors.New("unexpected status from the engine")

// Engine is the API the simulator drives.
type Engine interface {
	Authorize(ctx context.Context, request domain.AuthRequest) (domain.AuthResponse, error)
	ApplyEvent(ctx context.Context, authorizationID, path string, body EventBody) (lifecycle.Result, error)
	PublishPolicy(ctx context.Context, companyID string, policy json.RawMessage) error
}

// EventBody is the body of a lifecycle call.
type EventBody struct {
	EventID     string `json:"event_id"`
	AmountMinor *int64 `json:"amount_minor,omitempty"`
}

// HTTPEngine calls the engine over HTTP.
type HTTPEngine struct {
	baseURL string
	client  *http.Client
}

// NewHTTPEngine returns an Engine for the engine at baseURL.
func NewHTTPEngine(baseURL string, client *http.Client) *HTTPEngine {
	return &HTTPEngine{baseURL: baseURL, client: client}
}

// Authorize implements Engine.
func (h *HTTPEngine) Authorize(ctx context.Context, request domain.AuthRequest) (domain.AuthResponse, error) {
	var response domain.AuthResponse

	err := h.send(ctx, http.MethodPost, pathAuthorize, request, http.StatusOK, &response)

	return response, err
}

// ApplyEvent implements Engine; path is the lifecycle route, e.g. "increment".
func (h *HTTPEngine) ApplyEvent(ctx context.Context, authorizationID, path string, body EventBody) (lifecycle.Result, error) {
	var result lifecycle.Result

	err := h.send(ctx, http.MethodPost, fmt.Sprintf(fmtPathEvent, authorizationID, path), body, http.StatusOK, &result)

	return result, err
}

// PublishPolicy implements Engine.
func (h *HTTPEngine) PublishPolicy(ctx context.Context, companyID string, policy json.RawMessage) error {
	return h.send(ctx, http.MethodPut, fmt.Sprintf(fmtPathPolicy, companyID), policy, http.StatusCreated, nil)
}

func (h *HTTPEngine) send(ctx context.Context, method, path string, body any, wantStatus int, out any) error {
	var payload io.Reader = http.NoBody

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}

		payload = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, h.baseURL+path, payload)
	if err != nil {
		return fmt.Errorf("build %s: %w", path, err)
	}

	request.Header.Set(headerType, contentTypeJSON)

	response, err := h.client.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}

	defer response.Body.Close() //nolint:errcheck // the body was read or is being discarded

	if response.StatusCode != wantStatus {
		detail, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		if readErr != nil {
			detail = []byte(readErr.Error())
		}

		return fmt.Errorf("%w: %s %s: %d %s", ErrUnexpectedStatus, method, path, response.StatusCode, bytes.TrimSpace(detail))
	}

	if out == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(out)
}
