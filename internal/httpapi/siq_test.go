package httpapi

import (
	"archive/zip"
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

func TestImportSiq(t *testing.T) {
	e := newTestEnv(t)
	data := readFixture(t, "SIGameTestEn.siq")

	body, ct := multipartBody(t, "file", "SIGameTestEn.siq", data)
	rec := e.do(t, http.MethodPost, "/api/v1/packs/import", body, "Content-Type", ct)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var res ImportResult
	decodeJSON(t, rec, &res)
	require.NotNil(t, res.Pack)
	require.NotNil(t, res.Report)
	require.NotEmpty(t, res.Pack.ID)
	require.Equal(t, 1, res.Pack.Version)
	require.Greater(t, res.Report.Stats.Questions, 0)
	require.Greater(t, res.Report.Stats.MediaImported, 0)
	require.Equal(t, "/api/v1/packs/"+res.Pack.ID, rec.Header().Get("Location"))

	// The official test pack contains one content-less placeholder question;
	// it is turned into an empty slot, listed in the report and saved that way.
	require.GreaterOrEqual(t, res.Report.Count(CodeAutoFixed), 1, "report: %+v", res.Report.Entries)
	fixedPaths := map[string]bool{}
	for _, en := range res.Report.Entries {
		if en.Code == CodeAutoFixed {
			fixedPaths[en.Path] = true
		}
	}
	require.True(t, fixedPaths["/rounds/0/themes/0/questions/8/params/question"], "%v", fixedPaths)
	// A numberSet whose step does not divide max-min is valid SIGame data (100, 300, ..., 900).
	q4 := res.Pack.Rounds[0].Themes[0].Questions[4]
	require.NotNil(t, q4.Params.Price)
	require.Equal(t, packs.NumberSet{Min: 100, Max: 1000, Step: 200}, *q4.Params.Price)
	require.True(t, res.Pack.Rounds[0].Themes[0].Questions[8].IsEmpty())

	// Persisted, with media references recorded.
	rec = e.do(t, http.MethodGet, "/api/v1/packs/"+res.Pack.ID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var stored packs.Pack
	decodeJSON(t, rec, &stored)
	require.NotEmpty(t, stored.MediaIDs())
	for _, id := range stored.MediaIDs() {
		rec = e.do(t, http.MethodGet, "/api/v1/media/"+id, nil)
		require.Equal(t, http.StatusOK, rec.Code, id)
	}

	// Dry run: report + pack, nothing persisted.
	body, ct = multipartBody(t, "file", "SIGameTestEn.siq", data)
	rec = e.do(t, http.MethodPost, "/api/v1/packs/import?dryRun=true", body, "Content-Type", ct)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	res = ImportResult{}
	decodeJSON(t, rec, &res)
	require.Equal(t, 0, res.Pack.Version, "dry run must not assign a version")
	require.Zero(t, res.Pack.CreatedAt)
	require.Greater(t, res.Report.Stats.Questions, 0)
	rec = e.do(t, http.MethodGet, "/api/v1/packs/"+res.Pack.ID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "dry run must not persist the pack")
	var list PackList
	rec = e.do(t, http.MethodGet, "/api/v1/packs", nil)
	decodeJSON(t, rec, &list)
	require.Equal(t, 1, list.Total)

	// Not a zip → 400.
	body, ct = multipartBody(t, "file", "bad.siq", []byte("definitely not a zip archive"))
	rec = e.do(t, http.MethodPost, "/api/v1/packs/import", body, "Content-Type", ct)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Equal(t, http.StatusBadRequest, decodeProblem(t, rec).Status)

	// Zip without content.xml → 422.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, err := zw.Create("readme.txt")
	require.NoError(t, err)
	_, _ = w.Write([]byte("hi"))
	require.NoError(t, zw.Close())
	body, ct = multipartBody(t, "file", "empty.siq", zbuf.Bytes())
	rec = e.do(t, http.MethodPost, "/api/v1/packs/import", body, "Content-Type", ct)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// Missing file field → 422; wrong content type → 415.
	body, ct = multipartBody(t, "other", "x.siq", data[:100])
	rec = e.do(t, http.MethodPost, "/api/v1/packs/import", body, "Content-Type", ct)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Equal(t, "body.file", decodeProblem(t, rec).Errors[0].Location)
	rec = e.do(t, http.MethodPost, "/api/v1/packs/import", "{}")
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code, rec.Body.String())

	// Oversize body → 413.
	e.srv.siqBodyCap = 1024
	body, ct = multipartBody(t, "file", "SIGameTestEn.siq", data)
	rec = e.do(t, http.MethodPost, "/api/v1/packs/import", body, "Content-Type", ct)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
}

func TestExportSiq(t *testing.T) {
	e := newTestEnv(t)
	body, ct := multipartBody(t, "file", "SIGameTestEn.siq", readFixture(t, "SIGameTestEn.siq"))
	rec := e.do(t, http.MethodPost, "/api/v1/packs/import", body, "Content-Type", ct)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var res ImportResult
	decodeJSON(t, rec, &res)

	rec = e.do(t, http.MethodGet, "/api/v1/packs/"+res.Pack.ID+"/export", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	cd := rec.Header().Get("Content-Disposition")
	require.Contains(t, cd, "attachment;")
	require.Contains(t, cd, ".siq")
	require.Contains(t, cd, "filename*=UTF-8''")

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(t, err)
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	require.True(t, names["content.xml"], "entries: %v", names)
	require.Greater(t, len(zr.File), 2, "media entries expected")

	rec = e.do(t, http.MethodGet, "/api/v1/packs/0190a000-0000-7000-8000-000000000000/export", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, http.StatusNotFound, decodeProblem(t, rec).Status)
}

func TestExportFileName(t *testing.T) {
	require.Equal(t, "Мой пак", exportFileName("  Мой пак ", "id"))
	require.Equal(t, "a_b_c", exportFileName(`a/b\c`, "id"))
	require.Equal(t, "id", exportFileName("...", "id"))
	require.Equal(t, `attachment; filename="___.siq"; filename*=UTF-8''%D0%BF%D0%B0%D0%BA.siq`, attachmentDisposition("пак.siq"))
}
