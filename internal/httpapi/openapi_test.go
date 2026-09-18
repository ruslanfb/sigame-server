package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIDocument(t *testing.T) {
	e := newTestEnv(t)

	rec := e.do(t, http.MethodGet, "/openapi.json", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Title       string `json:"title"`
			Version     string `json:"version"`
			Description string `json:"description"`
		} `json:"info"`
		Servers []any `json:"servers"`
		Tags    []struct {
			Name string `json:"name"`
		} `json:"tags"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, "3.1.0", doc.OpenAPI)
	require.Equal(t, "SIGame Server API", doc.Info.Title)
	require.Equal(t, "test", doc.Info.Version)
	require.Contains(t, doc.Info.Description, "/docs")
	require.Empty(t, doc.Servers)
	tagNames := []string{}
	for _, tg := range doc.Tags {
		tagNames = append(tagNames, tg.Name)
	}
	require.ElementsMatch(t, []string{"packs", "media", "ai", "system"}, tagNames)

	wantOps := map[string]bool{
		"listPacks": false, "getPack": false, "createPack": false, "updatePack": false, "deletePack": false,
		"duplicatePack": false, "validatePack": false, "importSiq": false, "exportSiq": false,
		"addRound": false, "updateRound": false, "deleteRound": false, "reorderRounds": false,
		"addTheme": false, "updateTheme": false, "deleteTheme": false, "reorderThemes": false,
		"addQuestion": false, "updateQuestion": false, "deleteQuestion": false, "reorderQuestions": false,
		"uploadMedia": false, "getMediaMeta": false, "deleteMedia": false, "streamMedia": false,
		"getAIStatus": false, "listAIModels": false, "testAI": false,
		"getHealth": false, "getSystemInfo": false,
	}
	seen := map[string]bool{}
	for path, item := range doc.Paths {
		for method, raw := range item {
			if method == "parameters" || method == "summary" || method == "description" {
				continue
			}
			var op struct {
				OperationID string   `json:"operationId"`
				Summary     string   `json:"summary"`
				Description string   `json:"description"`
				Tags        []string `json:"tags"`
			}
			require.NoError(t, json.Unmarshal(raw, &op), "%s %s", method, path)
			require.NotEmpty(t, op.OperationID, "%s %s", method, path)
			require.NotEmpty(t, op.Summary, "%s %s", method, path)
			require.NotEmpty(t, op.Description, "%s %s", method, path)
			require.NotEmpty(t, op.Tags, "%s %s", method, path)
			require.False(t, seen[op.OperationID], "duplicate operationId %s", op.OperationID)
			seen[op.OperationID] = true
			if _, ok := wantOps[op.OperationID]; ok {
				wantOps[op.OperationID] = true
			}
		}
	}
	for id, ok := range wantOps {
		require.True(t, ok, "operation %s missing from the spec", id)
	}
	require.Contains(t, doc.Paths, "/api/v1/packs/import")
	require.Contains(t, doc.Paths, "/api/v1/packs/{id}/export")
	require.Contains(t, doc.Paths, "/api/v1/media")
	require.Contains(t, doc.Paths, "/media/{id}")

	// Server-managed fields are optional/readOnly in the pack schema.
	pack, ok := doc.Components.Schemas["Pack"]
	require.True(t, ok, "Pack schema registered")
	require.NotContains(t, pack.Required, "id")
	require.NotContains(t, pack.Required, "version")
	require.NotContains(t, pack.Required, "createdAt")
	require.Contains(t, pack.Required, "name")
	require.Contains(t, string(pack.Properties["id"]), `"readOnly":true`)
	require.NotContains(t, doc.Components.Schemas["Round"].Required, "id")

	rec = e.do(t, http.MethodGet, "/openapi.yaml", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "operationId: listPacks")

	rec = e.do(t, http.MethodGet, "/docs", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rec.Body.String(), "/openapi.json")
}
