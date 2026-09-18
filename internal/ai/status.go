package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
)

// StatusInfo is the operator-facing state of the AI judge.
type StatusInfo struct {
	Configured        bool   `json:"configured" doc:"An API key is set"`
	Model             string `json:"model"`
	BaseURL           string `json:"baseUrl"`
	LastError         string `json:"lastError,omitempty" doc:"Error of the most recent call, empty on success"`
	LastLatencyMs     int64  `json:"lastLatencyMs" doc:"Duration of the most recent call"`
	Calls             int64  `json:"calls" doc:"Total calls since start"`
	Failures          int64  `json:"failures" doc:"Failed calls since start"`
	TotalTokens       int64  `json:"totalTokens" doc:"Sum of usage.total_tokens"`
	StructuredOutputs bool   `json:"structuredOutputs" doc:"response_format json_schema is currently being sent"`
}

// ModelInfo is the subset of GET /models the operator needs to pick a model.
type ModelInfo struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	ContextLength       int      `json:"contextLength"`
	InputModalities     []string `json:"inputModalities"`
	SupportedParameters []string `json:"supportedParameters,omitempty" doc:"Contains structured_outputs / response_format when JSON schema output is supported"`
}

// Status returns the current state without touching the network.
func (j *OpenRouterJudge) Status(_ context.Context) StatusInfo {
	if j == nil {
		return StatusInfo{Model: DefaultModel, BaseURL: DefaultBaseURL}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return StatusInfo{
		Configured:        j.cfg.APIKey != "",
		Model:             j.cfg.Model,
		BaseURL:           j.cfg.BaseURL,
		LastError:         j.lastErr,
		LastLatencyMs:     j.lastLatencyMs,
		Calls:             j.calls,
		Failures:          j.failures,
		TotalTokens:       j.totalTokens,
		StructuredOutputs: j.useSchema.Load(),
	}
}

// ListModels fetches GET /models and returns the models sorted by ID.
// The endpoint is public, so it works without an API key.
func (j *OpenRouterJudge) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if j == nil {
		return nil, ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, j.cfg.Timeout)
	defer cancel()
	status, raw, err := j.doWithRetry(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, mapCtxErr(ctx, ctx, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrUpstream, status)
	}
	var body struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			Architecture  struct {
				InputModalities []string `json:"input_modalities"`
			} `json:"architecture"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("%w: models list: %v", ErrBadResponse, err)
	}
	out := make([]ModelInfo, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, ModelInfo{
			ID:                  m.ID,
			Name:                m.Name,
			ContextLength:       m.ContextLength,
			InputModalities:     m.Architecture.InputModalities,
			SupportedParameters: m.SupportedParameters,
		})
	}
	slices.SortFunc(out, func(a, b ModelInfo) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out, nil
}

// SampleRequest is the built-in request used by Test when none is given.
var SampleRequest = Request{
	Language:     "ru",
	Theme:        "География",
	QuestionText: "Столица Франции.",
	Right:        []string{"Париж"},
	PlayerAnswer: "Paris",
}

// Test performs one real judging call (with SampleRequest when sample has no
// accepted answers) so the operator can verify the key, model and latency.
func (j *OpenRouterJudge) Test(ctx context.Context, sample Request) (Verdict, error) {
	if len(sample.Right) == 0 {
		sample = SampleRequest
	}
	return j.Judge(ctx, sample)
}
