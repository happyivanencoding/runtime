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

type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type ScoreAnswer struct {
	Score         float64            `json:"score"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
	Legend        map[string]any     `json:"legend"`
}

type PolicyRequest struct {
	State                    any
	ActionCriteria           map[string]string
	ActionInstructions       string
	DoneInstructions         string
	RiskInstructions         string
	ConfirmationInstructions string
	ContextStateInstructions string
	ContextStateCriteria     map[string]string
}

type PolicyResult struct {
	Model            string
	NextAction       ChoiceAnswer
	TaskDone         float64
	ActionRisk       ScoreAnswer
	NeedConfirmation float64
	ContextState     ChoiceAnswer
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

func (c *Client) Policy(ctx context.Context, request PolicyRequest) (PolicyResult, error) {
	if !c.Configured() {
		return PolicyResult{}, fmt.Errorf("TypeSafe Jev API key is not configured")
	}
	if len(request.ActionCriteria) == 0 {
		return PolicyResult{}, fmt.Errorf("Jev policy requires at least one action criterion")
	}
	if len(request.ActionCriteria) > 255 {
		return PolicyResult{}, fmt.Errorf("Jev choice supports at most 255 action criteria")
	}
	if len(request.ContextStateCriteria) < 2 || len(request.ContextStateCriteria) > 255 {
		return PolicyResult{}, fmt.Errorf("Jev policy context state requires between 2 and 255 criteria")
	}

	payload := map[string]any{
		"state": request.State,
		"model": defaultModel,
		"questions": map[string]any{
			"next_action": map[string]any{
				"type":         "choice",
				"instructions": strings.TrimSpace(request.ActionInstructions),
				"criteria":     request.ActionCriteria,
			},
			"task_done": map[string]any{
				"type":         "noul",
				"instructions": strings.TrimSpace(request.DoneInstructions),
			},
			"action_risk": map[string]any{
				"type":         "score",
				"instructions": strings.TrimSpace(request.RiskInstructions),
				"criteria": []string{
					"Low risk and readily reversible, such as reading, scrolling, navigating, focusing, or editing unsent text",
					"Moderate impact but normally reversible, such as submitting a search/filter or changing a non-sensitive setting",
					"High impact or externally visible, such as sending/publishing content, uploading data, deleting data, authorizing access, installing software, or initiating a transaction",
					"Critical or potentially irreversible security/financial/account impact, such as payment, purchase, credential/security changes, or destructive account actions",
				},
			},
			"need_confirmation": map[string]any{
				"type":         "noul",
				"instructions": strings.TrimSpace(request.ConfirmationInstructions),
			},
			"context_state": map[string]any{
				"type":         "choice",
				"instructions": strings.TrimSpace(request.ContextStateInstructions),
				"criteria":     request.ContextStateCriteria,
			},
		},
	}

	responseBody, err := c.call(ctx, payload)
	if err != nil {
		return PolicyResult{}, err
	}

	var decoded struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Type          string             `json:"type"`
			Choice        string             `json:"choice"`
			Score         float64            `json:"score"`
			Noul          float64            `json:"noul"`
			Confidence    float64            `json:"confidence"`
			Probabilities map[string]float64 `json:"probabilities"`
			Legend        map[string]any     `json:"legend"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return PolicyResult{}, fmt.Errorf("decode TypeSafe Jev response: %w", err)
	}

	nextAction, ok := decoded.Answers["next_action"]
	if !ok || strings.TrimSpace(nextAction.Choice) == "" {
		return PolicyResult{}, fmt.Errorf("TypeSafe Jev response did not contain next_action choice")
	}
	taskDone, ok := decoded.Answers["task_done"]
	if !ok {
		return PolicyResult{}, fmt.Errorf("TypeSafe Jev response did not contain task_done")
	}
	actionRisk, ok := decoded.Answers["action_risk"]
	if !ok {
		return PolicyResult{}, fmt.Errorf("TypeSafe Jev response did not contain action_risk")
	}
	needConfirmation, ok := decoded.Answers["need_confirmation"]
	if !ok {
		return PolicyResult{}, fmt.Errorf("TypeSafe Jev response did not contain need_confirmation")
	}
	contextState, ok := decoded.Answers["context_state"]
	if !ok || strings.TrimSpace(contextState.Choice) == "" {
		return PolicyResult{}, fmt.Errorf("TypeSafe Jev response did not contain context_state choice")
	}

	return PolicyResult{
		Model: decoded.Model,
		NextAction: ChoiceAnswer{
			Choice: nextAction.Choice, Confidence: nextAction.Confidence, Probabilities: nextAction.Probabilities,
		},
		TaskDone: taskDone.Noul,
		ActionRisk: ScoreAnswer{
			Score: actionRisk.Score, Confidence: actionRisk.Confidence, Probabilities: actionRisk.Probabilities, Legend: actionRisk.Legend,
		},
		NeedConfirmation: needConfirmation.Noul,
		ContextState: ChoiceAnswer{
			Choice: contextState.Choice, Confidence: contextState.Confidence, Probabilities: contextState.Probabilities,
		},
	}, nil
}

func (c *Client) call(ctx context.Context, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Jev request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, systemOneURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Jev request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	response, err := c.http.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("call TypeSafe Jev: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read TypeSafe Jev response: %w", err)
	}
	if len(responseBody) > maxBodyBytes {
		return nil, fmt.Errorf("TypeSafe Jev response exceeded %d bytes", maxBodyBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("TypeSafe Jev returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}
