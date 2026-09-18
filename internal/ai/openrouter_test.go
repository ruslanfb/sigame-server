package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// completion renders a minimal OpenRouter chat-completion body.
func completion(content any) string {
	c, _ := json.Marshal(content)
	return fmt.Sprintf(`{"id":"gen-1","object":"chat.completion","model":"tencent/hy4-preview",
		"choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`, c)
}

const rightJSON = `{"verdict":"right","factor":1,"reason":"Это Пушкин"}`

// recorder captures every request the fake server receives.
type recorder struct {
	mu     sync.Mutex
	reqs   []*http.Request
	bodies []map[string]any
}

func (r *recorder) capture(req *http.Request) map[string]any {
	raw, _ := io.ReadAll(req.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, req)
	r.bodies = append(r.bodies, body)
	return body
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

func (r *recorder) body(i int) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies[i]
}

func newTestJudge(t *testing.T, handler http.HandlerFunc, mod func(*Config)) *OpenRouterJudge {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := Config{APIKey: "test-key", BaseURL: srv.URL + "/", Referer: "http://sigame.local", Timeout: 3 * time.Second}
	if mod != nil {
		mod(&cfg)
	}
	return NewOpenRouter(cfg)
}

// hangUntilGone drains the request body (the server only watches for the
// client leaving once the body is consumed) and blocks until the client
// disconnects or the test releases it.
func hangUntilGone(r *http.Request, release <-chan struct{}) {
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-release:
	}
}

var sampleReq = Request{
	QuestionID: "q1", Language: "ru", Theme: "Литература",
	QuestionText: "Автор «Евгения Онегина».", Right: []string{"Пушкин"}, Wrong: []string{"Лермонтов"},
	PlayerAnswer: "Александр Пушкин",
}

func TestOpenRouterRequestShape(t *testing.T) {
	rec := &recorder{}
	j := newTestJudge(t, func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "sigame-server", r.Header.Get("X-Title"))
		require.Equal(t, "http://sigame.local", r.Header.Get("HTTP-Referer"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, completion(rightJSON))
	}, nil)

	v, err := j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, 1.0, v.Factor)
	require.False(t, v.Uncertain)
	require.Equal(t, SourceAI, v.Source)
	require.Equal(t, "Это Пушкин", v.Reason)

	require.Equal(t, 1, rec.count())
	body := rec.body(0)
	require.Equal(t, DefaultModel, body["model"])
	require.Equal(t, 0.0, body["temperature"])
	require.Equal(t, float64(DefaultMaxTokens), body["max_tokens"])
	_, streaming := body["stream"]
	require.False(t, streaming)

	msgs := body["messages"].([]any)
	require.Len(t, msgs, 2)
	sys := msgs[0].(map[string]any)
	user := msgs[1].(map[string]any)
	require.Equal(t, "system", sys["role"])
	require.Equal(t, SystemPrompt, sys["content"])
	require.Equal(t, "user", user["role"])
	require.Contains(t, user["content"], "PLAYER_ANSWER_BEGIN\nАлександр Пушкин\nPLAYER_ANSWER_END")
	require.Contains(t, user["content"], "- Пушкин")
	require.Contains(t, user["content"], "- Лермонтов")
	require.Contains(t, user["content"], "Автор «Евгения Онегина».")

	rf := body["response_format"].(map[string]any)
	require.Equal(t, "json_schema", rf["type"])
	js := rf["json_schema"].(map[string]any)
	require.Equal(t, "verdict", js["name"])
	require.Equal(t, true, js["strict"])
	schema := js["schema"].(map[string]any)
	require.Equal(t, "object", schema["type"])
	require.Equal(t, false, schema["additionalProperties"])
	props := schema["properties"].(map[string]any)
	require.ElementsMatch(t, []any{"right", "wrong", "uncertain"}, props["verdict"].(map[string]any)["enum"])
	require.ElementsMatch(t, []any{"verdict", "factor", "reason"}, schema["required"])

	st := j.Status(context.Background())
	require.True(t, st.Configured)
	require.Equal(t, int64(25), st.TotalTokens)
	require.Equal(t, int64(1), st.Calls)
	require.Equal(t, int64(0), st.Failures)
	require.Empty(t, st.LastError)
	require.True(t, st.StructuredOutputs)
	require.Equal(t, DefaultModel, st.Model)
}

func TestOpenRouterPlainTextAndArrayContent(t *testing.T) {
	var content any
	j := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, completion(content))
	}, nil)

	content = "Sure! Here is my verdict:\n```json\n{\"verdict\": \"wrong\", \"factor\": 0, \"reason\": \"Это Лермонтов, а не Пушкин\"}\n```\nHope this helps."
	v, err := j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	require.False(t, v.Right)
	require.False(t, v.Uncertain)
	require.Equal(t, 0.0, v.Factor)
	require.Equal(t, "Это Лермонтов, а не Пушкин", v.Reason)

	content = []map[string]any{{"type": "text", "text": `{"verdict":"uncertain",`}, {"type": "text", "text": `"factor":0,"reason":"Неоднозначно"}`}}
	v, err = j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	require.True(t, v.Uncertain)
	require.False(t, v.Right)
	require.Equal(t, "Неоднозначно", v.Reason)
}

func TestOpenRouterRetryOn429(t *testing.T) {
	var calls atomic.Int32
	j := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":429,"message":"Rate limited"}}`)
			return
		}
		_, _ = io.WriteString(w, completion(rightJSON))
	}, nil)

	start := time.Now()
	v, err := j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, int32(2), calls.Load())
	require.GreaterOrEqual(t, time.Since(start), retryBackoff)
}

func TestOpenRouterRetryOnErrorInOKBody(t *testing.T) {
	var calls atomic.Int32
	j := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"error":{"code":502,"message":"Provider returned error"}}`)
			return
		}
		_, _ = io.WriteString(w, completion(rightJSON))
	}, nil)
	v, err := j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, int32(2), calls.Load())
}

func TestOpenRouterGivesUpAfterOneRetry(t *testing.T) {
	var calls atomic.Int32
	j := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":502,"message":"upstream down"}}`)
	}, nil)
	_, err := j.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrUpstream)
	require.Contains(t, err.Error(), "502")
	require.Contains(t, err.Error(), "upstream down")
	require.Equal(t, int32(2), calls.Load())

	st := j.Status(context.Background())
	require.Equal(t, int64(1), st.Failures)
	require.Contains(t, st.LastError, "502")
}

func TestOpenRouterNoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	j := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":401,"message":"Invalid API key"}}`)
	}, nil)
	_, err := j.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrUpstream)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "Invalid API key")
	require.Equal(t, int32(1), calls.Load())
}

func TestOpenRouterTimeout(t *testing.T) {
	release := make(chan struct{})
	j := newTestJudge(t, func(w http.ResponseWriter, r *http.Request) {
		hangUntilGone(r, release)
	}, func(c *Config) { c.Timeout = 50 * time.Millisecond })
	t.Cleanup(func() { close(release) }) // runs before srv.Close

	start := time.Now()
	_, err := j.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrTimeout)
	require.Less(t, time.Since(start), 2*time.Second)
	require.NotEmpty(t, j.Status(context.Background()).LastError)
}

func TestOpenRouterCallerCancel(t *testing.T) {
	release := make(chan struct{})
	j := newTestJudge(t, func(w http.ResponseWriter, r *http.Request) {
		hangUntilGone(r, release)
	}, nil)
	t.Cleanup(func() { close(release) }) // runs before srv.Close
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := j.Judge(ctx, sampleReq)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrTimeout)
}

func TestOpenRouterStructuredOutputFallback(t *testing.T) {
	rec := &recorder{}
	j := newTestJudge(t, func(w http.ResponseWriter, r *http.Request) {
		body := rec.capture(r)
		if _, withSchema := body["response_format"]; withSchema {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":400,"message":"response_format json_schema is not supported by this model"}}`)
			return
		}
		_, _ = io.WriteString(w, completion("Verdict: "+rightJSON))
	}, nil)

	v, err := j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, 2, rec.count())
	_, has := rec.body(0)["response_format"]
	require.True(t, has)
	_, has = rec.body(1)["response_format"]
	require.False(t, has)
	require.False(t, j.Status(context.Background()).StructuredOutputs)

	// The fallback is remembered: the next call goes straight to plain JSON.
	_, err = j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	require.Equal(t, 3, rec.count())
	_, has = rec.body(2)["response_format"]
	require.False(t, has)
}

func TestOpenRouterStructuredOutputsDisabled(t *testing.T) {
	rec := &recorder{}
	off := false
	j := newTestJudge(t, func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, completion(rightJSON))
	}, func(c *Config) { c.StructuredOutputs = &off })
	_, err := j.Judge(context.Background(), sampleReq)
	require.NoError(t, err)
	_, has := rec.body(0)["response_format"]
	require.False(t, has)
}

func TestOpenRouterBadResponse(t *testing.T) {
	var content any
	j := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, completion(content))
	}, nil)

	content = "I am not sure what you mean."
	_, err := j.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrBadResponse)

	content = `{"verdict":"maybe","factor":0,"reason":""}`
	_, err = j.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrBadResponse)

	j2 := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"x","choices":[]}`)
	}, nil)
	_, err = j2.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrBadResponse)

	j3 := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html>not json</html>`)
	}, nil)
	_, err = j3.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrBadResponse)
}

func TestOpenRouterNotConfigured(t *testing.T) {
	j := NewOpenRouter(Config{})
	_, err := j.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrNotConfigured)
	require.False(t, j.Configured())
	st := j.Status(context.Background())
	require.False(t, st.Configured)
	require.Equal(t, DefaultModel, st.Model)
	require.Equal(t, DefaultBaseURL, st.BaseURL)

	var nilJudge *OpenRouterJudge
	_, err = nilJudge.Judge(context.Background(), sampleReq)
	require.ErrorIs(t, err, ErrNotConfigured)
	require.False(t, nilJudge.Status(context.Background()).Configured)
	_, err = nilJudge.ListModels(context.Background())
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestOpenRouterConfigDefaults(t *testing.T) {
	cfg := Config{APIKey: "k"}.withDefaults()
	require.Equal(t, DefaultModel, cfg.Model)
	require.Equal(t, DefaultBaseURL, cfg.BaseURL)
	require.Equal(t, DefaultTimeout, cfg.Timeout)
	require.Equal(t, 0.0, cfg.Temperature)
	require.Equal(t, DefaultMaxTokens, cfg.MaxTokens)
	require.Equal(t, DefaultAppName, cfg.AppName)
	require.NotNil(t, cfg.HTTPClient)
	require.NotNil(t, cfg.Logger)
	require.Equal(t, "https://x/api/v1", Config{BaseURL: "https://x/api/v1/"}.withDefaults().BaseURL)
}

func TestListModels(t *testing.T) {
	j := newTestJudge(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"data":[
			{"id":"tencent/hy4-preview","name":"Tencent: Hy4 preview","context_length":1000000,
			 "architecture":{"modality":"text->text","input_modalities":["text"],"output_modalities":["text"]},
			 "pricing":{"prompt":"0"},"supported_parameters":["temperature","response_format","structured_outputs"]},
			{"id":"anthropic/claude-x","name":"Anthropic: Claude X","context_length":200000,
			 "architecture":{"input_modalities":["text","image"]}}
		]}`)
	}, nil)
	models, err := j.ListModels(context.Background())
	require.NoError(t, err)
	require.Equal(t, []ModelInfo{
		{ID: "anthropic/claude-x", Name: "Anthropic: Claude X", ContextLength: 200000, InputModalities: []string{"text", "image"}},
		{ID: "tencent/hy4-preview", Name: "Tencent: Hy4 preview", ContextLength: 1000000, InputModalities: []string{"text"},
			SupportedParameters: []string{"temperature", "response_format", "structured_outputs"}},
	}, models)
}

func TestListModelsErrors(t *testing.T) {
	j := newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}, nil)
	_, err := j.ListModels(context.Background())
	require.ErrorIs(t, err, ErrUpstream)

	j = newTestJudge(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": "nope"}`)
	}, nil)
	_, err = j.ListModels(context.Background())
	require.ErrorIs(t, err, ErrBadResponse)
}

func TestTestUsesSampleRequest(t *testing.T) {
	rec := &recorder{}
	j := newTestJudge(t, func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, completion(rightJSON))
	}, nil)
	v, err := j.Test(context.Background(), Request{})
	require.NoError(t, err)
	require.True(t, v.Right)
	user := rec.body(0)["messages"].([]any)[1].(map[string]any)["content"].(string)
	require.Contains(t, user, "Париж")
	require.Contains(t, user, "PLAYER_ANSWER_BEGIN\nParis\n")

	custom := sampleReq
	_, err = j.Test(context.Background(), custom)
	require.NoError(t, err)
	user = rec.body(1)["messages"].([]any)[1].(map[string]any)["content"].(string)
	require.Contains(t, user, "Александр Пушкин")
}

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Verdict
		wantErr error
	}{
		{"bare right", `{"verdict":"right","factor":1,"reason":"ok"}`, Verdict{Right: true, Factor: 1, Reason: "ok", Source: SourceAI}, nil},
		{"partial credit", `{"verdict":"right","factor":0.5,"reason":"half"}`, Verdict{Right: true, Factor: 0.5, Reason: "half", Source: SourceAI}, nil},
		{"right with factor 0 → 1", `{"verdict":"right","factor":0,"reason":""}`, Verdict{Right: true, Factor: 1, Source: SourceAI}, nil},
		{"right without factor", `{"verdict":"correct","reason":"x"}`, Verdict{Right: true, Factor: 1, Reason: "x", Source: SourceAI}, nil},
		{"wrong ignores factor", `{"verdict":"wrong","factor":1,"reason":"no"}`, Verdict{Reason: "no", Source: SourceAI}, nil},
		{"uncertain", `{"verdict":"UNCERTAIN","factor":0.3,"reason":"hm"}`, Verdict{Uncertain: true, Reason: "hm", Source: SourceAI}, nil},
		{"fenced", "```json\n{\"verdict\":\"wrong\",\"factor\":0,\"reason\":\"r\"}\n```", Verdict{Reason: "r", Source: SourceAI}, nil},
		{"braces inside strings", `text {"verdict":"right","factor":1,"reason":"see {this} and \"that\""} tail`, Verdict{Right: true, Factor: 1, Reason: `see {this} and "that"`, Source: SourceAI}, nil},
		{"multiline reason collapsed", "{\"verdict\":\"right\",\"factor\":1,\"reason\":\"a\\n  b\"}", Verdict{Right: true, Factor: 1, Reason: "a b", Source: SourceAI}, nil},
		{"no json", "I don't know", Verdict{}, ErrBadResponse},
		{"unknown verdict", `{"verdict":"meh"}`, Verdict{}, ErrBadResponse},
		{"unterminated", `{"verdict":"right"`, Verdict{}, ErrBadResponse},
		{"empty", ``, Verdict{}, ErrBadResponse},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseVerdict(c.in)
			if c.wantErr != nil {
				require.ErrorIs(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	all := []error{ErrNotConfigured, ErrTimeout, ErrBadResponse, ErrUpstream}
	for i, a := range all {
		for k, b := range all {
			if i != k {
				require.False(t, errors.Is(a, b))
			}
		}
	}
}
