package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/ai"
)

// fakeProvider implements AIStatusProvider.
type fakeProvider struct {
	status ai.StatusInfo
	models []ai.ModelInfo
	err    error
	last   ai.Request
}

func (f *fakeProvider) Status(context.Context) ai.StatusInfo { return f.status }

func (f *fakeProvider) ListModels(context.Context) ([]ai.ModelInfo, error) {
	return f.models, f.err
}

func (f *fakeProvider) Test(_ context.Context, req ai.Request) (ai.Verdict, error) {
	f.last = req
	if f.err != nil {
		return ai.Verdict{}, f.err
	}
	return ai.Verdict{Right: true, Factor: 1, Source: ai.SourceAI, Reason: "fake"}, nil
}

func TestAIUnconfigured(t *testing.T) {
	e := newTestEnv(t)

	rec := e.do(t, http.MethodGet, "/api/v1/ai/status", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var st ai.StatusInfo
	decodeJSON(t, rec, &st)
	require.False(t, st.Configured)
	require.Equal(t, "tencent/hy4-preview", st.Model)

	rec = e.do(t, http.MethodPost, "/api/v1/ai/test", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusServiceUnavailable, decodeProblem(t, rec).Status)

	rec = e.do(t, http.MethodGet, "/api/v1/ai/models", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())

	// A typed-nil OpenRouter judge behaves as "not configured" too.
	var nilJudge *ai.OpenRouterJudge
	e2 := newTestEnv(t, withDeps(func(d *Deps) { d.AIStatus = nilJudge }))
	rec = e2.do(t, http.MethodGet, "/api/v1/ai/status", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	decodeJSON(t, rec, &st)
	require.False(t, st.Configured)
	rec = e2.do(t, http.MethodPost, "/api/v1/ai/test", map[string]any{"playerAnswer": "x"})
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
}

func TestAIWithFakeJudge(t *testing.T) {
	var seen ai.Request
	judge := ai.JudgeFunc(func(_ context.Context, req ai.Request) (ai.Verdict, error) {
		seen = req
		return ai.Verdict{Right: true, Factor: 1, Source: ai.SourceAI, Reason: "ok"}, nil
	})
	e := newTestEnv(t, withDeps(func(d *Deps) { d.AI = judge; d.Cfg.OpenRouterAPIKey = "test-key" }))

	rec := e.do(t, http.MethodGet, "/api/v1/ai/status", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var st ai.StatusInfo
	decodeJSON(t, rec, &st)
	require.True(t, st.Configured)

	// Empty body → the built-in sample is judged.
	rec = e.do(t, http.MethodPost, "/api/v1/ai/test", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var v ai.Verdict
	decodeJSON(t, rec, &v)
	require.True(t, v.Right)
	require.Equal(t, ai.SourceAI, v.Source)
	require.Equal(t, ai.SampleRequest.Right, seen.Right)

	// Custom request is forwarded.
	rec = e.do(t, http.MethodPost, "/api/v1/ai/test", AITestRequest{Right: []string{"Париж"}, PlayerAnswer: "Paris", Language: "ru"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []string{"Париж"}, seen.Right)
	require.Equal(t, "Paris", seen.PlayerAnswer)

	// Models still need a provider.
	rec = e.do(t, http.MethodGet, "/api/v1/ai/models", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestAIWithProvider(t *testing.T) {
	fp := &fakeProvider{
		status: ai.StatusInfo{Configured: true, Model: "m", BaseURL: "http://x"},
		models: []ai.ModelInfo{{ID: "a/b", Name: "B", ContextLength: 1000}},
	}
	e := newTestEnv(t, withDeps(func(d *Deps) { d.AIStatus = fp }))

	rec := e.do(t, http.MethodGet, "/api/v1/ai/models", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var ml ModelList
	decodeJSON(t, rec, &ml)
	require.Len(t, ml.Models, 1)
	require.Equal(t, "a/b", ml.Models[0].ID)

	rec = e.do(t, http.MethodPost, "/api/v1/ai/test", AITestRequest{Right: []string{"1"}, PlayerAnswer: "1"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "1", fp.last.PlayerAnswer)

	// Upstream failures map to 502/504/503.
	fp.err = ai.ErrUpstream
	rec = e.do(t, http.MethodGet, "/api/v1/ai/models", nil)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	fp.err = errors.Join(ai.ErrTimeout)
	rec = e.do(t, http.MethodPost, "/api/v1/ai/test", nil)
	require.Equal(t, http.StatusGatewayTimeout, rec.Code, rec.Body.String())
	fp.err = ai.ErrNotConfigured
	rec = e.do(t, http.MethodPost, "/api/v1/ai/test", nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
}
