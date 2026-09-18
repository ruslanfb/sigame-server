package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/ai"
)

// AITestRequest mirrors ai.Request with every field optional; when no
// accepted answer is given the server judges its built-in sample.
type AITestRequest struct {
	Language        string   `json:"language,omitempty" doc:"Pack language hint, e.g. ru or en"`
	Theme           string   `json:"theme,omitempty" doc:"Theme name"`
	QuestionText    string   `json:"questionText,omitempty" doc:"Plain-text question body"`
	Right           []string `json:"right,omitempty" doc:"Accepted answers; empty = use the built-in sample request"`
	Wrong           []string `json:"wrong,omitempty" doc:"Known wrong answers"`
	AnswerType      string   `json:"answerType,omitempty" enum:",text,select,number,point,client" doc:"Empty = text"`
	Deviation       float64  `json:"deviation,omitempty" doc:"number: ± tolerance; point: distance tolerance"`
	PlayerAnswer    string   `json:"playerAnswer,omitempty" doc:"The answer to judge"`
	ShowmanComments string   `json:"showmanComments,omitempty" doc:"Pack author's notes for the showman"`
}

func (r AITestRequest) toRequest() ai.Request {
	return ai.Request{
		Language:        r.Language,
		Theme:           r.Theme,
		QuestionText:    r.QuestionText,
		Right:           r.Right,
		Wrong:           r.Wrong,
		AnswerType:      r.AnswerType,
		Deviation:       r.Deviation,
		PlayerAnswer:    r.PlayerAnswer,
		ShowmanComments: r.ShowmanComments,
	}
}

// ModelList wraps the upstream model list.
type ModelList struct {
	Models []ai.ModelInfo `json:"models" doc:"Models offered by the provider, sorted by id"`
}

type aiStatusOutput struct {
	Body ai.StatusInfo
}

type aiModelsOutput struct {
	Body ModelList
}

type aiTestInput struct {
	Body *AITestRequest `doc:"Optional request to judge; omitted or without accepted answers = built-in sample"`
}

type aiTestOutput struct {
	Body ai.Verdict
}

func (s *Server) registerAI() {
	huma.Register(s.API, huma.Operation{
		OperationID: "getAIStatus",
		Method:      http.MethodGet,
		Path:        basePath + "/ai/status",
		Tags:        []string{tagAI},
		Summary:     "AI judge status",
		Description: "Reports whether the AI showman is configured, which model and base URL are in use, and counters of the calls made since start. Never touches the network.",
	}, s.getAIStatus)

	huma.Register(s.API, huma.Operation{
		OperationID: "listAIModels",
		Method:      http.MethodGet,
		Path:        basePath + "/ai/models",
		Tags:        []string{tagAI},
		Summary:     "List available AI models",
		Description: "Fetches the provider's model list (id, name, context length, modalities) so an operator can pick OPENROUTER_MODEL. 503 when no provider is configured.",
		Errors:      []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
	}, s.listAIModels)

	huma.Register(s.API, huma.Operation{
		OperationID: "testAI",
		Method:      http.MethodPost,
		Path:        basePath + "/ai/test",
		Tags:        []string{tagAI},
		Summary:     "Test the AI judge",
		Description: "Performs one real judging call and returns the verdict, so the operator can verify the key, model and latency. All request fields are optional; without accepted answers a built-in sample (a geography question) is judged. 503 when the AI judge is not configured.",
		Errors:      []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
	}, s.testAI)
}

func (s *Server) getAIStatus(ctx context.Context, _ *struct{}) (*aiStatusOutput, error) {
	if s.deps.AIStatus != nil {
		return &aiStatusOutput{Body: s.deps.AIStatus.Status(ctx)}, nil
	}
	return &aiStatusOutput{Body: ai.StatusInfo{
		Configured: s.deps.Cfg.AIConfigured(),
		Model:      s.deps.Cfg.OpenRouterModel,
		BaseURL:    s.deps.Cfg.OpenRouterBaseURL,
	}}, nil
}

func (s *Server) listAIModels(ctx context.Context, _ *struct{}) (*aiModelsOutput, error) {
	if s.deps.AIStatus == nil {
		return nil, mapError(ai.ErrNotConfigured)
	}
	models, err := s.deps.AIStatus.ListModels(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	if models == nil {
		models = []ai.ModelInfo{}
	}
	return &aiModelsOutput{Body: ModelList{Models: models}}, nil
}

func (s *Server) testAI(ctx context.Context, in *aiTestInput) (*aiTestOutput, error) {
	req := ai.Request{}
	if in.Body != nil {
		req = in.Body.toRequest()
	}
	var (
		v   ai.Verdict
		err error
	)
	switch {
	case s.deps.AIStatus != nil:
		v, err = s.deps.AIStatus.Test(ctx, req)
	case s.deps.AI != nil && s.aiConfigured(ctx):
		if len(req.Right) == 0 {
			req = ai.SampleRequest
		}
		v, err = s.deps.AI.Judge(ctx, req)
	default:
		return nil, mapError(ai.ErrNotConfigured)
	}
	if err != nil {
		return nil, s.fail(err)
	}
	return &aiTestOutput{Body: v}, nil
}
