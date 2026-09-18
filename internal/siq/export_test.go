package siq

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/media"
	"sigame/internal/packs"
)

func samplePack(store *fakeStore) *packs.Pack {
	logo := store.add("logo.png", []byte("LOGO"), media.KindImage)
	pic := store.add("Снимок 6.jpg", []byte("PIC"), "") // kind by extension, jpg → meta ext jpg
	pic2 := store.add("Снимок 6.jpg", []byte("PIC-2"), media.KindImage)
	track := store.add("dir/@my%track#1?.mp3", []byte("MP3"), media.KindAudio)
	clip := store.add("", []byte("CLIP"), media.KindVideo)
	page := store.add("page.html", []byte("<p>hi</p>"), media.KindHTML)
	return &packs.Pack{
		ID: "0190a000-0000-7000-8000-000000000001", Name: "Экспорт & <тест>", Language: "ru-RU", Difficulty: 7, Restriction: "12+",
		Date: "01.01.2026", Publisher: "Pub", ContactURI: "https://example.com", LogoMediaID: logo,
		Tags: []string{"A", "B"},
		Info: packs.Info{Authors: []string{"Автор"}, Sources: []string{"@ae9f7eb2#с.256"}, Comments: "line1\nline2", ShowmanComments: "secret note"},
		Extra: &packs.Extra{
			GlobalAuthors: []packs.AuthorRecord{{ID: "ae9f7eb2", Name: "Иван", Surname: "Петров"}},
			GlobalSources: []packs.SourceRecord{{ID: "7ab08cfa", Title: "Книга", Year: "2004"}},
		},
		Rounds: []packs.Round{
			{Name: "Раунд 1", Type: packs.RoundStandard, Themes: []packs.Theme{{Name: "Тема", Questions: []packs.Question{
				{Price: 100, Type: packs.QSimple, Right: []string{"Ответ", "Alt"}, Wrong: []string{"Нет"}, Params: packs.QuestionParams{Question: []packs.ContentItem{
					{Type: packs.ContentText, Text: "Реплика", Placement: packs.PlaceReplic},
					{Type: packs.ContentText, Text: "Текст", DurationMs: 8000, NoWait: true},
					{Type: packs.ContentImage, MediaID: pic},
					{Type: packs.ContentImage, MediaID: pic2},
					{Type: packs.ContentAudio, MediaID: track},
					{Type: packs.ContentVideo, MediaID: clip, DurationMs: 2500},
					{Type: packs.ContentHTML, MediaID: page},
					{Type: packs.ContentImage, URL: "https://example.com/x.png"},
				}, Answer: []packs.ContentItem{{Type: packs.ContentImage, MediaID: pic}}}},
				{Price: 200, Type: packs.QSecret, Right: []string{"S"}, Params: packs.QuestionParams{Theme: "Тема кота", Price: &packs.NumberSet{Min: 100, Max: 500, Step: 200}, SelectionMode: packs.SelectAny, Question: []packs.ContentItem{{Type: packs.ContentText, Text: "Q"}}}},
				{Price: 300, Type: packs.QCustom, Right: []string{"C"}, Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "Q"}}, AnswerType: packs.AnswerSelect,
					AnswerOptions: []packs.AnswerOption{{Label: "A", Content: []packs.ContentItem{{Type: packs.ContentText, Text: "1"}}}, {Label: "B", Content: []packs.ContentItem{{Type: packs.ContentImage, MediaID: pic}}}, {Label: "C", Content: []packs.ContentItem{{Type: packs.ContentText, Text: "3"}}}}},
					Extra:  []packs.Param{{Name: "myParam", Value: "v"}, {Name: "grp", Type: "group", Params: []packs.Param{{Name: "inner", Type: "numberSet", NumberSet: &packs.NumberSet{Min: 1, Max: 2, Step: 1}}}}},
					Script: []packs.ScriptStep{{Type: "showContent", Params: []packs.Param{{Name: "content", Type: "content", Items: []packs.ContentItem{{Type: packs.ContentText, Text: "in script"}}}}}, {Type: "askAnswer", Params: []packs.Param{{Name: "fallbackRefId", IsRef: true, Value: "right"}}}}},
				{Price: 400, Type: packs.QForAll, Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "N"}}, AnswerType: packs.AnswerNumber, AnswerDeviation: 0.5, AnswerDurationMs: 15400}, Right: []string{"100"}},
				{Price: -1},
				{Price: 500, Type: "myType", Right: []string{}, Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "X"}}, AnswerType: packs.AnswerText}},
			}}}},
			{Name: "Финал", Type: packs.RoundFinal, Themes: []packs.Theme{{Name: "Ф", Questions: []packs.Question{{Price: 0, Type: packs.QStakeAll, Right: []string{"F"}, Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentText, Text: "Final"}}}}}}}},
		},
	}
}

func TestExportArchive(t *testing.T) {
	store := newFakeStore()
	p := samplePack(store)
	var out bytes.Buffer
	require.NoError(t, Export(context.Background(), p, store, &out))
	data := out.Bytes()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	require.Equal(t, []string{"content.xml", "[Content_Types].xml", "Audio/mytrack1.mp3", "Html/page.html", "Images/logo.png",
		"Images/Снимок 6-2.jpg", "Images/Снимок 6.jpg", "Video/" + clipStem(store) + ".mp4"}, names)
	for _, f := range zr.File {
		switch f.Name {
		case "content.xml", "[Content_Types].xml":
			require.Equal(t, zip.Deflate, f.Method)
		default:
			require.Equal(t, zip.Store, f.Method, f.Name)
		}
		require.False(t, f.NonUTF8, f.Name)
		if strings.ContainsAny(f.Name, "Ск") {
			require.NotZero(t, f.Flags&0x800, "UTF-8 flag must be set for %s", f.Name)
		}
	}
	require.Equal(t, ContentTypesXML, string(readZipEntry(t, data, "[Content_Types].xml")))
	require.Equal(t, "MP3", string(readZipEntry(t, data, "Audio/mytrack1.mp3")))

	xmlBytes := readZipEntry(t, data, "content.xml")
	require.True(t, bytes.HasPrefix(xmlBytes, utf8BOM))
	require.True(t, bytes.HasPrefix(xmlBytes[3:], []byte(xmlDeclaration+"<package ")))
	require.Contains(t, string(xmlBytes), `xmlns="`+NamespaceV5+`"`)
	require.Contains(t, string(xmlBytes), `version="5"`)
	require.NotContains(t, string(xmlBytes), "\n<")

	xp, err := decodeContentXML(xmlBytes)
	require.NoError(t, err)
	require.Equal(t, "Экспорт & <тест>", xp.Name)
	require.Equal(t, "5", xp.Version)
	require.Equal(t, p.ID, xp.ID, "pack id is used when there is no SIQID")
	require.Equal(t, "12+", xp.Restriction)
	require.Equal(t, "7", xp.Difficulty)
	require.Equal(t, "@logo.png", xp.Logo)
	require.Equal(t, "ru-RU", xp.Language)
	require.Equal(t, "https://example.com", xp.ContactURI)
	require.Equal(t, []string{"A", "B"}, xp.Tags)
	require.NotNil(t, xp.Global)
	require.Equal(t, "Петров", xp.Global.Authors[0].Surname)
	require.Equal(t, "2004", xp.Global.Sources[0].Year)
	require.Equal(t, "line1\nline2", xp.Info.Comments)
	require.Equal(t, "secret note", xp.Info.ShowmanComments)
	require.Len(t, xp.Files, 6)
	for _, f := range xp.Files {
		require.Len(t, f.Hash, 64)
		require.Equal(t, strings.ToUpper(f.Hash), f.Hash)
		require.Contains(t, []string{"Images", "Audio", "Video", "Html"}, strings.SplitN(f.Name, "/", 2)[0])
	}
	require.Equal(t, "Audio/mytrack1.mp3", xp.Files[0].Name, "files are sorted ordinally")

	require.Len(t, xp.Rounds, 2)
	require.Equal(t, "", xp.Rounds[0].Type, "standard round type is omitted")
	require.Equal(t, "final", xp.Rounds[1].Type)
	qs := xp.Rounds[0].Themes[0].Questions
	require.Len(t, qs, 6)

	q1 := qs[0]
	require.Equal(t, "100", q1.Price)
	require.Equal(t, "", q1.Type, "simple type is omitted")
	require.NotNil(t, q1.Params)
	require.Equal(t, []string{"Ответ", "Alt"}, q1.Right.Answers)
	require.Equal(t, []string{"Нет"}, q1.Wrong.Answers)
	require.Equal(t, "question", q1.Params.Params[0].Name)
	require.Equal(t, "content", q1.Params.Params[0].Type)
	items := q1.Params.Params[0].Items
	require.Len(t, items, 8)
	require.Equal(t, xmlItem{Text: "Реплика", Placement: "replic"}, items[0])
	require.Equal(t, xmlItem{Text: "Текст", Duration: "00:00:08", WaitForFinish: "False"}, items[1])
	require.Equal(t, xmlItem{Type: "image", IsRef: "True", Text: "Снимок 6.jpg"}, items[2])
	require.Equal(t, xmlItem{Type: "image", IsRef: "True", Text: "Снимок 6-2.jpg"}, items[3])
	require.Equal(t, xmlItem{Type: "audio", IsRef: "True", Placement: "background", Text: "mytrack1.mp3"}, items[4])
	require.Equal(t, xmlItem{Type: "video", IsRef: "True", Duration: "00:00:03", Text: clipStem(store) + ".mp4"}, items[5])
	require.Equal(t, xmlItem{Type: "html", IsRef: "True", Text: "page.html"}, items[6])
	require.Equal(t, xmlItem{Type: "image", Text: "https://example.com/x.png"}, items[7])
	require.Equal(t, "answer", q1.Params.Params[1].Name)

	q2 := qs[1]
	require.Equal(t, "secret", q2.Type)
	names2 := []string{}
	for _, pr := range q2.Params.Params {
		names2 = append(names2, pr.Name)
	}
	require.Equal(t, []string{"theme", "price", "selectionMode", "question"}, names2)
	require.Equal(t, &xmlNumberSet{Minimum: "100", Maximum: "500", Step: "200"}, q2.Params.Params[1].NumberSet)
	require.Equal(t, "any", q2.Params.Params[2].Text)

	q3 := qs[2]
	require.Equal(t, "custom", q3.Type)
	byName := map[string]xmlParam{}
	for _, pr := range q3.Params.Params {
		byName[pr.Name] = pr
	}
	require.Equal(t, "select", byName["answerType"].Text)
	require.Equal(t, "group", byName["answerOptions"].Type)
	require.Len(t, byName["answerOptions"].Params, 3)
	require.Equal(t, "B", byName["answerOptions"].Params[1].Name)
	require.Equal(t, "True", byName["answerOptions"].Params[1].Items[0].IsRef)
	require.Equal(t, "v", byName["myParam"].Text)
	require.Equal(t, "numberSet", byName["grp"].Params[0].Type)
	require.NotNil(t, q3.Script)
	require.Len(t, q3.Script.Steps, 2)
	require.Equal(t, "True", q3.Script.Steps[1].Params[0].IsRef)
	require.Equal(t, "right", q3.Script.Steps[1].Params[0].Text)

	q4 := qs[3]
	byName = map[string]xmlParam{}
	for _, pr := range q4.Params.Params {
		byName[pr.Name] = pr
	}
	require.Equal(t, "forAll", q4.Type)
	require.Equal(t, "number", byName["answerType"].Text)
	require.Equal(t, "0.5", byName["answerDeviation"].Text)
	require.Equal(t, "15", byName["answerDuration"].Text, "milliseconds are rounded to seconds")

	q5 := qs[4]
	require.Equal(t, "-1", q5.Price)
	require.Equal(t, "", q5.Type)
	require.NotNil(t, q5.Params)
	require.Empty(t, q5.Params.Params)
	require.Equal(t, []string{""}, q5.Right.Answers, "<right> always has at least one answer")
	require.Nil(t, q5.Wrong)

	q6 := qs[5]
	require.Equal(t, "myType", q6.Type)
	require.Equal(t, []string{""}, q6.Right.Answers)
	for _, pr := range q6.Params.Params {
		require.NotEqual(t, "answerType", pr.Name, "answerType text is omitted")
	}

	require.Equal(t, "stakeAll", xp.Rounds[1].Themes[0].Questions[0].Type)

	// The archive imports back cleanly.
	store2 := newFakeStore()
	p2, rep, err := importBytes(t, data, store2, ImportOptions{})
	require.NoError(t, err)
	require.Empty(t, entriesWithLevel(rep, LevelError))
	require.Equal(t, p.ID, p2.SIQID)
	require.Equal(t, p.LogoMediaID, p2.LogoMediaID)
	require.ElementsMatch(t, p.MediaIDs(), p2.MediaIDs())
	require.Equal(t, 6, rep.Stats.MediaImported)
	// Canonical form after a v5 round-trip: whole seconds, empty slot as a
	// simple question with one empty answer, answerType text omitted.
	want := normalize(t, p)
	want.Rounds[0].Themes[0].Questions[0].Params.Question[5].DurationMs = 3000
	want.Rounds[0].Themes[0].Questions[3].Params.AnswerDurationMs = 15000
	want.Rounds[0].Themes[0].Questions[4] = packs.Question{Price: -1, Type: packs.QSimple, Params: packs.QuestionParams{Question: []packs.ContentItem{}}, Right: []string{""}}
	want.Rounds[0].Themes[0].Questions[5].Params.AnswerType = ""
	want.Rounds[0].Themes[0].Questions[5].Right = []string{""}
	require.Equal(t, want, normalize(t, p2))
}

// clipStem is the name given to a media file without an original name.
func clipStem(store *fakeStore) string {
	id := store.add("", []byte("CLIP"), media.KindVideo)
	return id[:16]
}

func TestExportMissingMedia(t *testing.T) {
	store := newFakeStore()
	p := &packs.Pack{Name: "x", Rounds: []packs.Round{{Name: "r", Themes: []packs.Theme{{Name: "t", Questions: []packs.Question{
		{Price: 100, Params: packs.QuestionParams{Question: []packs.ContentItem{{Type: packs.ContentImage, MediaID: strings.Repeat("0", 64)}}}},
	}}}}}}
	var out bytes.Buffer
	err := Export(context.Background(), p, store, &out)
	require.ErrorIs(t, err, ErrMediaNotFound)
}

func TestExportExtensionAndNames(t *testing.T) {
	store := newFakeStore()
	id := store.add("photo.jpg", []byte("A"), media.KindImage)
	store.objects[id].meta.Ext = "png" // the store sniffed a PNG
	id2 := store.add("photo.jpeg", []byte("B"), media.KindImage)
	id3 := store.add("noext", []byte("C"), media.KindAudio)
	store.objects[id3].meta.Ext = "mp3"
	p := &packs.Pack{Name: "x", Rounds: []packs.Round{{Name: "r", Themes: []packs.Theme{{Name: "t", Questions: []packs.Question{
		{Price: 100, Params: packs.QuestionParams{Question: []packs.ContentItem{
			{Type: packs.ContentImage, MediaID: id}, {Type: packs.ContentImage, MediaID: id2}, {Type: packs.ContentAudio, MediaID: id3},
		}}},
	}}}}}}
	var out bytes.Buffer
	require.NoError(t, Export(context.Background(), p, store, &out))
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	require.NoError(t, err)
	var names []string
	for _, f := range zr.File[2:] {
		names = append(names, f.Name)
	}
	require.Equal(t, []string{"Audio/noext.mp3", "Images/photo.jpeg", "Images/photo.png"}, names)
	xp, err := decodeContentXML(readZipEntry(t, out.Bytes(), "content.xml"))
	require.NoError(t, err)
	require.Equal(t, "photo.png", xp.Rounds[0].Themes[0].Questions[0].Params.Params[0].Items[0].Text)
	require.Equal(t, "", xp.Difficulty, "difficulty 0 is omitted")
	require.NotEmpty(t, xp.ID, "a fresh GUID is generated when the pack has no id")
}
