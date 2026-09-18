package siq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

// TestCorpus imports every official fixture with media enabled.
func TestCorpus(t *testing.T) {
	start := time.Now()
	total := Stats{ByType: map[string]int{}}
	versions := map[int]int{}
	codes := map[string]int{}
	for _, name := range listFixtures(t) {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			p, rep, err := importBytes(t, readFixture(t, name), store, ImportOptions{})
			require.NoError(t, err)
			require.NotNil(t, p)
			require.NotEmpty(t, p.Name)
			require.NotEmpty(t, p.ID)
			require.Empty(t, entriesWithLevel(rep, LevelError), "no error entries expected for official packs")
			require.Greater(t, rep.Stats.Questions, 0)
			require.Equal(t, len(p.Rounds), rep.Stats.Rounds)
			versions[rep.Version]++
			total.Rounds += rep.Stats.Rounds
			total.Themes += rep.Stats.Themes
			total.Questions += rep.Stats.Questions
			total.MediaImported += rep.Stats.MediaImported
			total.Bytes += rep.Stats.Bytes
			for k, v := range rep.Stats.ByType {
				total.ByType[k] += v
			}
			for _, e := range rep.Entries {
				codes[e.Level+":"+e.Code]++
			}
			// every question of a standard round has a right answer slot and non-nil content
			for _, r := range p.Rounds {
				for _, th := range r.Themes {
					for _, q := range th.Questions {
						require.NotEmpty(t, q.Right)
						require.NotNil(t, q.Params.Question)
						require.NotEmpty(t, q.Type)
						for _, it := range q.Params.Question {
							if it.Type != packs.ContentText {
								require.True(t, it.MediaID != "" || it.URL != "", "media item without id/url: %+v", it)
							}
						}
					}
				}
			}
		})
	}
	t.Logf("corpus report entries: %v", codes)
	t.Logf("corpus: %d packs, versions=%v, rounds=%d themes=%d questions=%d byType=%v media=%d bytes=%d in %s",
		len(listFixtures(t)), versions, total.Rounds, total.Themes, total.Questions, total.ByType, total.MediaImported, total.Bytes, time.Since(start))
	require.Equal(t, 5331, total.Questions)
	for _, typ := range []packs.QuestionType{packs.QSimple, packs.QStake, packs.QSecret, packs.QSecretPublicPrice,
		packs.QSecretNoQuestion, packs.QNoRisk, packs.QForAll, packs.QStakeAll, packs.QCustom} {
		require.Greater(t, total.ByType[string(typ)], 0, "type %s must be present in the corpus", typ)
	}
	require.Equal(t, 2, versions[4]+versions[3], "two legacy packs expected")
	require.Less(t, time.Since(start), 10*time.Second)
}

func TestCorpusSkipMedia(t *testing.T) {
	store := newFakeStore()
	p, rep, err := importBytes(t, readFixture(t, "SIGameTestEn.siq"), store, ImportOptions{SkipMedia: true})
	require.NoError(t, err)
	require.Empty(t, store.puts)
	require.Equal(t, 0, rep.Stats.MediaImported)
	require.Empty(t, entriesWithLevel(rep, LevelError))
	q := questionByPrice(t, findTheme(t, p, "Question Content"), 300)
	require.Equal(t, packs.ContentImage, q.Params.Question[0].Type)
	require.Empty(t, q.Params.Question[0].MediaID, "SkipMedia keeps the item as a placeholder")
}

func TestSIGameTestEn(t *testing.T) {
	store := newFakeStore()
	p, rep, err := importBytes(t, readFixture(t, "SIGameTestEn.siq"), store, ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, 5, rep.Version)
	require.Empty(t, entriesWithLevel(rep, LevelError))

	require.Equal(t, "SIGame Test Package", p.Name)
	require.Equal(t, "a16f96e7-4616-47d0-8652-aee5196094af", p.SIQID)
	require.Equal(t, "en-US", p.Language)
	require.Equal(t, "12+", p.Restriction)
	require.Equal(t, "07.07.2022", p.Date)
	require.Equal(t, "https://mysite.com", p.ContactURI)
	require.Equal(t, 5, p.Difficulty)
	require.Equal(t, []string{"Package Theme 1", "Package Theme 2"}, p.Tags)
	require.Equal(t, []string{"Vladimir Khil"}, p.Info.Authors)
	require.NotEmpty(t, p.LogoMediaID)
	require.Empty(t, p.LogoURL)
	require.Len(t, p.Rounds, 2)
	require.Equal(t, packs.RoundStandard, p.Rounds[0].Type)
	require.Len(t, p.Rounds[0].Info.Sources, 1)

	types := findTheme(t, p, "Questions of Different Types")
	require.Len(t, types.Info.Authors, 1)
	require.Equal(t, packs.QSimple, questionByPrice(t, types, 100).Type)
	require.Equal(t, []string{"This is the correct answer"}, questionByPrice(t, types, 100).Right)
	stake := questionByPrice(t, types, 200)
	require.Equal(t, packs.QStake, stake.Type)
	require.Len(t, stake.Right, 2)
	require.Equal(t, []string{"Incorrect version for rejection/by bot"}, stake.Wrong)
	secret := questionByPrice(t, types, 500)
	require.Equal(t, packs.QSecret, secret.Type)
	require.Equal(t, &packs.NumberSet{Min: 100, Max: 1000, Step: 200}, secret.Params.Price)
	require.Equal(t, packs.SelectAny, secret.Params.SelectionMode)
	require.Equal(t, "New Theme", secret.Params.Theme)
	require.Equal(t, packs.QSecretPublicPrice, questionByPrice(t, types, 600).Type)
	require.Equal(t, &packs.NumberSet{Min: 100, Max: 500, Step: 400}, questionByPrice(t, types, 600).Params.Price)
	noq := questionByPrice(t, types, 700)
	require.Equal(t, packs.QSecretNoQuestion, noq.Type)
	require.Equal(t, packs.SelectExceptCurrent, noq.Params.SelectionMode)
	require.Equal(t, []string{""}, noq.Right)
	require.Equal(t, packs.QNoRisk, questionByPrice(t, types, 800).Type)
	empty := questionByPrice(t, types, 900)
	require.Equal(t, []string{""}, empty.Right, "empty <right/> becomes one empty answer")
	require.Equal(t, packs.QCustom, questionByPrice(t, types, 1000).Type)
	require.Equal(t, packs.QStakeAll, questionByPrice(t, types, 1100).Type)
	require.Equal(t, packs.QForAll, questionByPrice(t, types, 1200).Type)

	content := findTheme(t, p, "Question Content")
	q200 := questionByPrice(t, content, 200)
	require.Equal(t, packs.PlaceReplic, q200.Params.Question[0].Placement)
	require.Equal(t, int64(8000), q200.Params.Question[1].DurationMs)
	img := questionByPrice(t, content, 300).Params.Question[0]
	require.Equal(t, packs.ContentImage, img.Type)
	require.Len(t, img.MediaID, 64)
	aud := questionByPrice(t, content, 400).Params.Question[0]
	require.Equal(t, packs.ContentAudio, aud.Type)
	require.Len(t, aud.MediaID, 64)
	require.Equal(t, packs.Placement(""), aud.Placement, "default background placement is normalised to empty")
	require.Equal(t, packs.PlaceBackground, aud.EffectivePlacement())
	vid := questionByPrice(t, content, 500).Params.Question[0]
	require.Equal(t, packs.ContentVideo, vid.Type)
	require.Len(t, vid.MediaID, 64)
	q600 := questionByPrice(t, content, 600)
	require.True(t, q600.Params.Question[0].NoWait)
	require.False(t, q600.Params.Question[1].NoWait)
	q900 := questionByPrice(t, content, 900)
	require.Len(t, q900.Params.Answer, 1)
	require.Equal(t, packs.ContentImage, q900.Params.Answer[0].Type)
	require.Len(t, questionByPrice(t, content, 1200).Params.Question, 4)
	html := questionByPrice(t, content, 1500).Params.Question[0]
	require.Equal(t, packs.ContentHTML, html.Type)
	require.Len(t, html.MediaID, 64)

	extra := findTheme(t, p, "Additional")
	sel := questionByPrice(t, extra, 100)
	require.Equal(t, packs.AnswerSelect, sel.Params.AnswerType)
	require.Len(t, sel.Params.AnswerOptions, 4)
	require.Equal(t, "A", sel.Params.AnswerOptions[0].Label)
	require.Equal(t, "D", sel.Params.AnswerOptions[3].Label)
	require.Equal(t, "Option 3", sel.Params.AnswerOptions[2].Content[0].Text)
	require.Equal(t, []string{"C"}, sel.Right)
	num := questionByPrice(t, extra, 200)
	require.Equal(t, packs.AnswerNumber, num.Params.AnswerType)
	require.Equal(t, 5.0, num.Params.AnswerDeviation)
	require.Equal(t, []string{"100"}, num.Right)

	final := p.Rounds[1]
	require.Equal(t, packs.RoundFinal, final.Type)
	require.Len(t, final.Themes, 3)
	require.Equal(t, 0, final.Themes[0].Questions[0].Price)

	require.Equal(t, 10, rep.Stats.MediaImported, "7 images + audio + video + html")
	require.Equal(t, 0, rep.Stats.MediaMissing)
	require.Greater(t, rep.Stats.Bytes, int64(2_000_000))
	require.Len(t, p.MediaIDs(), 10)
	require.Equal(t, 1, rep.Count(CodeHTMLContent))
}

func TestSIGameTestNewGlobalAndFiles(t *testing.T) {
	store := newFakeStore()
	p, rep, err := importBytes(t, readFixture(t, "pack4-07.siq"), store, ImportOptions{})
	require.NoError(t, err)
	require.NotNil(t, p.Extra)
	require.Len(t, p.Extra.GlobalAuthors, 3)
	require.Equal(t, "f8fa07e3-eb01-4e55-ba35-2dcd545017cd", p.Extra.GlobalAuthors[0].ID)
	require.Equal(t, "Байрам", p.Extra.GlobalAuthors[0].Name)
	require.Equal(t, "Франкфурт-на-Майне", p.Extra.GlobalAuthors[1].City)
	require.Empty(t, entriesWithLevel(rep, LevelError))

	p2, rep2, err := importBytes(t, readFixture(t, "SIGameTestNew.siq"), newFakeStore(), ImportOptions{})
	require.NoError(t, err)
	require.Empty(t, entriesWithLevel(rep2, LevelError))
	htmlTheme := findTheme(t, p2, "HTML игры")
	require.Len(t, htmlTheme.Questions, 3)
	for _, q := range htmlTheme.Questions {
		found := false
		for _, it := range q.Params.Question {
			if it.Type == packs.ContentHTML {
				found = true
				require.Len(t, it.MediaID, 64, "percent-encoded html entries resolve")
			}
		}
		require.True(t, found, "html item expected in %q", q.Params.Question)
	}
	extra := findTheme(t, p2, "Дополнительно")
	require.Equal(t, int64(15000), questionByPrice(t, extra, 400).Params.AnswerDurationMs)
	point := questionByPrice(t, extra, 300)
	require.Equal(t, packs.AnswerPoint, point.Params.AnswerType)
	require.Equal(t, 0.05, point.Params.AnswerDeviation)
	require.Equal(t, []string{"0.46,0.7,1.33"}, point.Right)
}
