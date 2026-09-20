package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	systemOneURL = "https://api.typesafe.ai/v1/systemone"
	defaultModel = "jev-latest"
	maxBodyBytes = 1 << 20
)

type Client struct {
	apiKey string
	http   *http.Client
}

type ChoiceRequest struct {
	State        any
	Instructions string
	Criteria     map[string]string
}

type ChoiceResult struct {
	Model         string
	Choice        string
	Confidence    float64
	Probabilities map[string]float64
}

func New(apiKey string) *Client {
	return &Client{
		apiKey: strings.TrimSpace(apiKey),
		http:   &http.Client{Timeout: 8 * time.Second},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.apiKey != ""
}

func (c *Client) Choose(ctx context.Context, request ChoiceRequest) (ChoiceResult, error) {
	if !c.Configured() {
		return ChoiceResult{}, fmt.Errorf("TypeSafe Jev API key is not configured")
	}
	if len(request.Criteria) == 0 {
		return ChoiceResult{}, fmt.Errorf("Jev choice requires at least one criterion")
	}
	if len(request.Criteria) > 255 {
		return ChoiceResult{}, fmt.Errorf("Jev choice supports at most 255 criteria")
	}

	payload := map[string]any{
		"state": request.State,
		"model": defaultModel,
		"questions": map[string]any{
			"next_action": map[string]any{
				"type":         "choice",
				"instructions": strings.TrimSpace(request.Instructions),
				"criteria":     request.Criteria,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChoiceResult{}, fmt.Errorf("encode Jev request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, systemOneURL, bytes.NewReader(body))
	if err != nil {
		return ChoiceResult{}, fmt.Errorf("create Jev request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	response, err := c.http.Do(httpRequest)
	if err != nil {
		return ChoiceResult{}, fmt.Errorf("call TypeSafe Jev: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return ChoiceResult{}, fmt.Errorf("read TypeSafe Jev response: %w", err)
	}
	if len(responseBody) > maxBodyBytes {
		return ChoiceResult{}, fmt.Errorf("TypeSafe Jev response exceeded %d bytes", maxBodyBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ChoiceResult{}, fmt.Errorf("TypeSafe Jev returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var decoded struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Choice        string             `json:"choice"`
			Confidence    float64            `json:"confidence"`
			Probabilities map[string]float64 `json:"probabilities"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return ChoiceResult{}, fmt.Errorf("decode TypeSafe Jev response: %w", err)
	}
	answer, ok := decoded.Answers["next_action"]
	if !ok || strings.TrimSpace(answer.Choice) == "" {
		return ChoiceResult{}, fmt.Errorf("TypeSafe Jev response did not contain next_action choice")
	}
	return ChoiceResult{
		Model:         decoded.Model,
		Choice:        answer.Choice,
		Confidence:    answer.Confidence,
		Probabilities: answer.Probabilities,
	}, nil
}
