package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults for Config.
const (
	DefaultModel     = "google/gemini-3-flash-preview"
	DefaultBaseURL   = "https://openrouter.ai/api/v1"
	DefaultTimeout   = 25 * time.Second // generous: reasoning models (e.g. tencent/hy4-preview) need 15–20 s
	DefaultMaxTokens = 2000             // reasoning tokens count against max_tokens
	DefaultAppName   = "sigame-server"

	retryBackoff = 500 * time.Millisecond
	maxRetries   = 1
	maxBodyBytes = 1 << 20
)

// Sentinel errors of the OpenRouter client.
var (
	ErrNotConfigured = errors.New("ai: OpenRouter API key is not configured")
	ErrTimeout       = errors.New("ai: OpenRouter request timed out")
	ErrBadResponse   = errors.New("ai: unexpected OpenRouter response")
	ErrUpstream      = errors.New("ai: OpenRouter returned an error")
)

// Config configures OpenRouterJudge. Zero values take the Default* constants
// (Temperature 0 is intended).
type Config struct {
	APIKey      string
	Model       string        // default DefaultModel
	BaseURL     string        // default DefaultBaseURL (no trailing slash)
	Timeout     time.Duration // whole call including the retry; default DefaultTimeout
	Temperature float64       // default 0
	MaxTokens   int           // default DefaultMaxTokens
	AppName     string        // sent as X-Title; default DefaultAppName
	Referer     string        // sent as HTTP-Referer when non-empty

	// StructuredOutputs: nil = send response_format json_schema and fall back
	// to plain-JSON parsing once the provider rejects it; false = never send it.
	StructuredOutputs *bool
	HTTPClient        *http.Client // default: fresh client without its own timeout
	Logger            *slog.Logger // default slog.Default()
}

func (c Config) withDefaults() Config {
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	if c.AppName == "" {
		c.AppName = DefaultAppName
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Usage is the token accounting returned by OpenRouter.
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost,omitempty"` // USD, present only when usage accounting is enabled
}

// OpenRouterJudge asks an LLM through the OpenRouter chat-completions API.
// It is safe for concurrent use. A nil *OpenRouterJudge behaves as "not configured".
type OpenRouterJudge struct {
	cfg       Config
	useSchema atomic.Bool // send response_format json_schema

	mu            sync.Mutex
	lastErr       string
	lastLatencyMs int64
	lastUsage     Usage
	calls         int64
	failures      int64
	totalTokens   int64
}

// NewOpenRouter creates a judge; it does not talk to the network.
func NewOpenRouter(cfg Config) *OpenRouterJudge {
	j := &OpenRouterJudge{cfg: cfg.withDefaults()}
	j.useSchema.Store(cfg.StructuredOutputs == nil || *cfg.StructuredOutputs)
	return j
}

// Configured reports whether an API key is set.
func (j *OpenRouterJudge) Configured() bool { return j != nil && j.cfg.APIKey != "" }

// Model returns the configured model ID.
func (j *OpenRouterJudge) Model() string {
	if j == nil {
		return ""
	}
	return j.cfg.Model
}

// Judge implements Judge. Errors: ErrNotConfigured, ErrTimeout, ErrUpstream
// (HTTP/provider error after the retry), ErrBadResponse (unparseable verdict),
// or the caller's context error.
func (j *OpenRouterJudge) Judge(ctx context.Context, req Request) (Verdict, error) {
	if !j.Configured() {
		return Verdict{}, ErrNotConfigured
	}
	start := time.Now()
	v, usage, err := j.complete(ctx, BuildMessages(req))
	latency := time.Since(start)
	j.record(latency, usage, err)
	if err != nil {
		j.cfg.Logger.Debug("ai: openrouter judge failed", "model", j.cfg.Model, "latencyMs", latency.Milliseconds(), "err", err)
		return Verdict{}, err
	}
	j.cfg.Logger.Debug("ai: openrouter verdict",
		"model", j.cfg.Model, "latencyMs", latency.Milliseconds(),
		"right", v.Right, "uncertain", v.Uncertain, "factor", v.Factor,
		"promptTokens", usage.PromptTokens, "completionTokens", usage.CompletionTokens,
		"totalTokens", usage.TotalTokens, "cost", usage.Cost)
	return v, nil
}

func (j *OpenRouterJudge) record(latency time.Duration, usage Usage, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.calls++
	j.lastLatencyMs = latency.Milliseconds()
	if err != nil {
		j.failures++
		j.lastErr = err.Error()
		return
	}
	j.lastErr = ""
	j.lastUsage = usage
	j.totalTokens += int64(usage.TotalTokens)
}

// --- wire format -------------------------------------------------------------

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

// verdictSchema is the strict JSON schema of the model's reply.
const verdictSchema = `{
  "type": "object",
  "properties": {
    "verdict": {"type": "string", "enum": ["right", "wrong", "uncertain"]},
    "factor": {"type": "number", "minimum": 0, "maximum": 1},
    "reason": {"type": "string"}
  },
  "required": ["verdict", "factor", "reason"],
  "additionalProperties": false
}`

func verdictResponseFormat() *responseFormat {
	return &responseFormat{
		Type:       "json_schema",
		JSONSchema: jsonSchema{Name: "verdict", Strict: true, Schema: json.RawMessage(verdictSchema)},
	}
}

type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role    string      `json:"role"`
			Content flexContent `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage    `json:"usage"`
	Error *apiError `json:"error"`
}

type apiError struct {
	Code    json.Number `json:"code"`
	Message string      `json:"message"`
}

func (e *apiError) status() int {
	if e == nil {
		return 0
	}
	n, err := e.Code.Int64()
	if err != nil {
		return 0
	}
	return int(n)
}

// flexContent accepts both a plain string and the array-of-parts content form.
type flexContent string

func (c *flexContent) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0 || bytes.Equal(b, []byte("null")):
		*c = ""
	case b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = flexContent(s)
	case b[0] == '[':
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(b, &parts); err != nil {
			return err
		}
		var sb strings.Builder
		for _, p := range parts {
			sb.WriteString(p.Text)
		}
		*c = flexContent(sb.String())
	default:
		return fmt.Errorf("unsupported content JSON: %s", b)
	}
	return nil
}

// --- transport ---------------------------------------------------------------

// complete runs one judging conversation with the retry and the structured
// output fallback. The whole call is bounded by cfg.Timeout.
func (j *OpenRouterJudge) complete(parent context.Context, msgs []Message) (Verdict, Usage, error) {
	ctx, cancel := context.WithTimeout(parent, j.cfg.Timeout)
	defer cancel()

	withSchema := j.useSchema.Load()
	for {
		body := chatRequest{Model: j.cfg.Model, Messages: msgs, Temperature: j.cfg.Temperature, MaxTokens: j.cfg.MaxTokens}
		if withSchema {
			body.ResponseFormat = verdictResponseFormat()
		}
		status, raw, err := j.doWithRetry(ctx, http.MethodPost, "/chat/completions", body)
		if err != nil {
			return Verdict{}, Usage{}, mapCtxErr(parent, ctx, err)
		}
		var resp chatResponse
		if jsonErr := json.Unmarshal(raw, &resp); jsonErr != nil && status == http.StatusOK {
			return Verdict{}, Usage{}, fmt.Errorf("%w: invalid JSON body: %v", ErrBadResponse, jsonErr)
		}
		if status != http.StatusOK || resp.Error != nil {
			code := status
			if code == http.StatusOK {
				code = resp.Error.status()
			}
			msg := ""
			if resp.Error != nil {
				msg = resp.Error.Message
			}
			if withSchema && code == http.StatusBadRequest && looksLikeSchemaUnsupported(msg, raw) {
				j.cfg.Logger.Info("ai: model rejected response_format json_schema, falling back to plain JSON", "model", j.cfg.Model, "message", msg)
				j.useSchema.Store(false)
				withSchema = false
				continue
			}
			if msg == "" {
				msg = strings.TrimSpace(string(raw))
				if len(msg) > 200 {
					msg = msg[:200]
				}
			}
			return Verdict{}, Usage{}, fmt.Errorf("%w: HTTP %d: %s", ErrUpstream, code, msg)
		}
		if len(resp.Choices) == 0 {
			return Verdict{}, Usage{}, fmt.Errorf("%w: no choices", ErrBadResponse)
		}
		usage := Usage{}
		if resp.Usage != nil {
			usage = *resp.Usage
		}
		choice := resp.Choices[0]
		v, err := ParseVerdict(string(choice.Message.Content))
		if err != nil {
			if choice.FinishReason == "length" {
				// Reasoning models spend the token budget on hidden thinking and
				// return a truncated answer; a JSON-schema constraint makes some
				// of them never finish. Retry once without the schema, then give up.
				if withSchema {
					j.cfg.Logger.Info("ai: truncated structured answer, retrying without response_format", "model", j.cfg.Model, "totalTokens", usage.TotalTokens)
					j.useSchema.Store(false)
					withSchema = false
					continue
				}
				return Verdict{}, usage, fmt.Errorf("%w: answer truncated (finish_reason=length, %d tokens); raise SIGAME_AI_MAX_TOKENS or pick a faster model", ErrBadResponse, usage.TotalTokens)
			}
			return Verdict{}, usage, err
		}
		return v, usage, nil
	}
}

func looksLikeSchemaUnsupported(msg string, raw []byte) bool {
	s := strings.ToLower(msg)
	if s == "" {
		s = strings.ToLower(string(raw))
	}
	for _, needle := range []string{"response_format", "json_schema", "structured", "schema"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func mapCtxErr(parent, ctx context.Context, err error) error {
	switch {
	case parent.Err() != nil && errors.Is(parent.Err(), context.Canceled):
		return parent.Err()
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	case errors.Is(err, context.Canceled):
		return err
	default:
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
}

// doWithRetry performs the HTTP call and retries once after retryBackoff on a
// network error, 429 or 5xx (also when a 200 body carries such an error code).
func (j *OpenRouterJudge) doWithRetry(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var (
		status  int
		raw     []byte
		lastErr error
	)
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, retryBackoff); err != nil {
				return 0, nil, lastErr
			}
			j.cfg.Logger.Debug("ai: retrying openrouter request", "path", path, "attempt", attempt)
		}
		var err error
		status, raw, err = j.do(ctx, method, path, payload)
		if err != nil {
			if ctx.Err() != nil {
				return 0, nil, err
			}
			lastErr = err
			continue
		}
		if !retryableStatus(status, raw) {
			return status, raw, nil
		}
		lastErr = fmt.Errorf("HTTP %d", status)
	}
	if status != 0 {
		return status, raw, nil
	}
	return 0, nil, lastErr
}

func retryableStatus(status int, raw []byte) bool {
	if status == http.StatusTooManyRequests || status >= 500 {
		return true
	}
	if status == http.StatusOK {
		var probe struct {
			Error *apiError `json:"error"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Error != nil {
			c := probe.Error.status()
			return c == http.StatusTooManyRequests || c >= 500
		}
	}
	return false
}

func (j *OpenRouterJudge) do(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("ai: encode request: %w", err)
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, j.cfg.BaseURL+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("ai: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if j.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+j.cfg.APIKey)
	}
	req.Header.Set("X-Title", j.cfg.AppName)
	if j.cfg.Referer != "" {
		req.Header.Set("HTTP-Referer", j.cfg.Referer)
	}
	res, err := j.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, raw, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// --- verdict parsing ---------------------------------------------------------

// ParseVerdict turns the model's reply into a Verdict. It accepts a bare JSON
// object or free text that contains one (e.g. inside a markdown code fence).
func ParseVerdict(content string) (Verdict, error) {
	var raw struct {
		Verdict string   `json:"verdict"`
		Factor  *float64 `json:"factor"`
		Reason  string   `json:"reason"`
	}
	text := strings.TrimSpace(content)
	if err := json.Unmarshal([]byte(text), &raw); err != nil || raw.Verdict == "" {
		block, ok := extractJSONObject(text)
		if !ok {
			return Verdict{}, fmt.Errorf("%w: no JSON object in model output", ErrBadResponse)
		}
		if err := json.Unmarshal([]byte(block), &raw); err != nil {
			return Verdict{}, fmt.Errorf("%w: %v", ErrBadResponse, err)
		}
	}
	v := Verdict{Source: SourceAI, Reason: oneLine(raw.Reason, 300)}
	switch strings.ToLower(strings.TrimSpace(raw.Verdict)) {
	case "right", "correct", "yes", "true", "accept":
		v.Right = true
		v.Factor = 1
		if raw.Factor != nil && *raw.Factor > 0 && *raw.Factor < 1 {
			v.Factor = *raw.Factor
		}
	case "wrong", "incorrect", "no", "false", "reject":
	case "uncertain", "unsure", "unknown", "ambiguous":
		v.Uncertain = true
	default:
		return Verdict{}, fmt.Errorf("%w: unknown verdict %q", ErrBadResponse, raw.Verdict)
	}
	return v, nil
}

// extractJSONObject returns the first balanced {...} block of s, honouring
// string literals and escapes.
func extractJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}
