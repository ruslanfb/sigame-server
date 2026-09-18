package siq

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sigame/internal/packs"
)

const v4ContentXML = `<?xml version="1.0" encoding="utf-8"?>
<package name="Тестовый пакет SIGame" version="4" id="a16f96e7-4616-47d0-8652-aee5196094af" restriction="12+" date="07.07.2022" publisher="Группа" difficulty="5" logo="@sigame_-_ava_2019.png" language="ru-RU" xmlns="http://vladimirkhil.com/ygpackage3.0.xsd">
  <tags><tag>Тема пакета 1</tag></tags>
  <info><authors><author>Vladimir Khil</author></authors><comments>Комментарий
вторая строка</comments></info>
  <rounds>
    <round name="1-й раунд">
      <info><sources><source>Источник раунда</source></sources></info>
      <themes>
        <theme name="Вопросы разных типов">
          <questions>
            <question price="100"><scenario><atom>Это текст вопроса</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="200"><type name="auction" /><scenario><atom>Вопрос со ставкой</atom></scenario><right><answer>Ответ</answer></right><wrong><answer>Неверно</answer></wrong></question>
            <question price="600"><type name="bagcat"><param name="theme">Новая тема</param><param name="cost">[100;500]</param><param name="self">false</param><param name="knows">before</param></type><scenario><atom>Кот до</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="700"><type name="bagcat"><param name="theme">-</param><param name="cost">700</param><param name="self">false</param><param name="knows">never</param></type><scenario><atom>Не задаётся</atom></scenario><right><answer /></right></question>
            <question price="800"><type name="sponsored" /><scenario><atom>Вопрос без риска</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="-1"><right /></question>
            <question price="500"><type name="bagcat"><param name="theme">Тема</param><param name="cost">[100;1000]/200</param><param name="self">true</param><param name="knows">after</param></type><scenario><atom>Кот</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="400"><type name="cat"><param name="theme">Кошачья</param><param name="cost">0</param></type><scenario><atom>Старый кот</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="300"><type name="myType"><param name="foo">bar</param></type><scenario><atom>Свой тип</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="900"><type name="bagcat"><param name="theme">X</param><param name="cost">garbage</param></type><scenario><atom>Плохая стоимость</atom></scenario><right><answer>Ответ</answer></right></question>
          </questions>
        </theme>
        <theme name="Контент">
          <questions>
            <question price="200"><scenario><atom type="say">Устный комментарий</atom><atom time="8">Текст на экране 8 секунд</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="600"><scenario><atom time="-1">Текст</atom><atom type="voice">@sample-3s.mp3</atom></scenario><right><answer>Текст + звук</answer></right></question>
            <question price="800"><scenario><atom type="image">https://vladimirkhil.com/images/svoyak-soft.jpg</atom></scenario><right><answer>Внешняя ссылка</answer></right></question>
            <question price="900"><scenario><atom>Медиа в ответе</atom><atom type="marker" /><atom type="image">@sample-boat-400x300.png</atom><atom type="marker" /><atom>Ещё</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="1200"><scenario><atom time="35" type="html">https://www.youtube.com/embed/a3ICNMQW7Ok?controls=0&amp;autoplay=1</atom></scenario><right><answer>HTML</answer></right></question>
            <question price="1300"><scenario><atom type="image">@nope.png</atom><atom>Текст после</atom></scenario><right><answer>Пропавшее медиа</answer></right></question>
            <question price="1400"><scenario><atom time="2.5" type="video">@clip.mp4</atom></scenario><right><answer>Видео</answer></right></question>
          </questions>
        </theme>
      </themes>
    </round>
    <round name="Финал" type="final"><themes><theme name="Тема 1"><questions><question price="0"><scenario><atom>Вопрос финала 1</atom></scenario><right><answer>Ответ</answer></right></question></questions></theme></themes></round>
  </rounds>
</package>`

const v4AuthorsXML = `<?xml version="1.0" encoding="utf-8"?><Authors xmlns:xsd="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><Author id="ae9f7eb2-6091-4b34-97a1-0f74ad193d57"><Name>Иван</Name><SecondName /><Surname>Петров</Surname><Country>Россия</Country><City /></Author></Authors>`

func v4Archive(t *testing.T) []byte {
	return buildZip(t, []zipEntry{
		{name: "content.xml", data: append(append([]byte{}, utf8BOM...), []byte(v4ContentXML)...)},
		{name: "Texts/authors.xml", data: []byte(v4AuthorsXML)},
		{name: "Texts/sources.xml", data: []byte(`<?xml version="1.0" encoding="utf-8"?><Sources />`)},
		{name: "Images/sigame_-_ava_2019.png", data: []byte("PNG-LOGO"), stored: true},
		{name: "Images/sample-boat-400x300.png", data: []byte("PNG-BOAT"), stored: true},
		{name: "Audio/sample-3s.mp3", data: []byte("MP3-DATA"), stored: true},
		{name: "Video/clip.mp4", data: []byte("MP4-DATA"), stored: true},
		{name: "[Content_Types].xml", data: []byte(ContentTypesXML)},
	})
}

func TestImportV4Upgrade(t *testing.T) {
	store := newFakeStore()
	p, rep, err := importBytes(t, v4Archive(t), store, ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, 4, rep.Version)
	require.Equal(t, 1, rep.Count(CodeV4Upgraded))

	require.Equal(t, "Тестовый пакет SIGame", p.Name)
	require.Equal(t, "a16f96e7-4616-47d0-8652-aee5196094af", p.SIQID)
	require.Equal(t, "Комментарий\nвторая строка", p.Info.Comments)
	require.NotEmpty(t, p.LogoMediaID)
	require.NotNil(t, p.Extra)
	require.Len(t, p.Extra.GlobalAuthors, 1)
	require.Equal(t, "Петров", p.Extra.GlobalAuthors[0].Surname)
	require.Empty(t, p.Extra.GlobalSources)

	types := findTheme(t, p, "Вопросы разных типов")
	require.Len(t, types.Questions, 10)

	simple := questionByPrice(t, types, 100)
	require.Equal(t, packs.QSimple, simple.Type)
	require.Equal(t, []packs.ContentItem{{Type: packs.ContentText, Text: "Это текст вопроса"}}, simple.Params.Question)

	stake := questionByPrice(t, types, 200)
	require.Equal(t, packs.QStake, stake.Type)
	require.Equal(t, []string{"Неверно"}, stake.Wrong)

	before := questionByPrice(t, types, 600)
	require.Equal(t, packs.QSecretPublicPrice, before.Type)
	require.Equal(t, "Новая тема", before.Params.Theme)
	require.Equal(t, &packs.NumberSet{Min: 100, Max: 500, Step: 0}, before.Params.Price)
	require.Equal(t, packs.SelectExceptCurrent, before.Params.SelectionMode)
	require.Equal(t, "Кот до", before.Params.Question[0].Text)

	never := questionByPrice(t, types, 700)
	require.Equal(t, packs.QSecretNoQuestion, never.Type)
	require.Empty(t, never.Params.Theme, "theme is dropped for knows=never")
	require.Equal(t, &packs.NumberSet{Min: 700, Max: 700, Step: 0}, never.Params.Price)
	require.Empty(t, never.Params.Question, "scenario is discarded for secretNoQuestion")
	require.Equal(t, []string{""}, never.Right)

	require.Equal(t, packs.QNoRisk, questionByPrice(t, types, 800).Type)

	empty := questionByPrice(t, types, -1)
	require.True(t, empty.IsEmpty())
	require.Equal(t, packs.QSimple, empty.Type)
	require.Equal(t, []string{""}, empty.Right)

	after := questionByPrice(t, types, 500)
	require.Equal(t, packs.QSecret, after.Type)
	require.Equal(t, &packs.NumberSet{Min: 100, Max: 1000, Step: 200}, after.Params.Price)
	require.Equal(t, packs.SelectAny, after.Params.SelectionMode)

	cat := questionByPrice(t, types, 400)
	require.Equal(t, packs.QSecret, cat.Type)
	require.Equal(t, "Кошачья", cat.Params.Theme)
	require.Equal(t, &packs.NumberSet{}, cat.Params.Price)
	require.Equal(t, packs.SelectExceptCurrent, cat.Params.SelectionMode)

	custom := questionByPrice(t, types, 300)
	require.Equal(t, packs.QuestionType("myType"), custom.Type)
	require.False(t, custom.Type.IsKnown())
	require.Equal(t, []packs.Param{{Name: "foo", Value: "bar"}}, custom.Extra)
	require.Equal(t, 1, rep.Count(CodeUnknownQuestionType))

	bad := questionByPrice(t, types, 900)
	require.Equal(t, packs.QSecret, bad.Type)
	require.Equal(t, &packs.NumberSet{Min: 900, Max: 900}, bad.Params.Price, "unparsable cost falls back to the nominal price")
	require.Equal(t, 1, rep.Count(CodeInvalidNumberSet))

	content := findTheme(t, p, "Контент")
	say := questionByPrice(t, content, 200)
	require.Equal(t, packs.PlaceReplic, say.Params.Question[0].Placement)
	require.Equal(t, packs.ContentText, say.Params.Question[0].Type)
	require.Equal(t, int64(8000), say.Params.Question[1].DurationMs)

	voice := questionByPrice(t, content, 600)
	require.True(t, voice.Params.Question[0].NoWait)
	require.Equal(t, int64(0), voice.Params.Question[0].DurationMs)
	require.Equal(t, packs.ContentAudio, voice.Params.Question[1].Type)
	require.Len(t, voice.Params.Question[1].MediaID, 64)
	require.Equal(t, packs.PlaceBackground, voice.Params.Question[1].EffectivePlacement())

	ext := questionByPrice(t, content, 800)
	require.Equal(t, packs.ContentImage, ext.Params.Question[0].Type)
	require.Equal(t, "https://vladimirkhil.com/images/svoyak-soft.jpg", ext.Params.Question[0].URL)
	require.Empty(t, ext.Params.Question[0].MediaID)

	marker := questionByPrice(t, content, 900)
	require.Len(t, marker.Params.Question, 1)
	require.Len(t, marker.Params.Answer, 2, "only the first marker splits; later markers are ignored")
	require.Equal(t, packs.ContentImage, marker.Params.Answer[0].Type)
	require.Len(t, marker.Params.Answer[0].MediaID, 64)
	require.Equal(t, "Ещё", marker.Params.Answer[1].Text)

	html := questionByPrice(t, content, 1200)
	require.Equal(t, packs.ContentHTML, html.Params.Question[0].Type)
	require.Equal(t, int64(35000), html.Params.Question[0].DurationMs)
	require.Contains(t, html.Params.Question[0].URL, "youtube.com/embed")

	missing := questionByPrice(t, content, 1300)
	require.Len(t, missing.Params.Question, 1, "the missing media item is dropped")
	require.Equal(t, "Текст после", missing.Params.Question[0].Text)
	require.Equal(t, 1, rep.Stats.MediaMissing)
	errs := entriesWithLevel(rep, LevelError)
	require.Len(t, errs, 1)
	require.Equal(t, CodeMediaMissing, errs[0].Code)
	require.Equal(t, "/rounds/0/themes/1/questions/5/params/question/0", errs[0].Path)

	video := questionByPrice(t, content, 1400)
	require.Equal(t, int64(2500), video.Params.Question[0].DurationMs, "fractional atom time keeps millisecond precision")

	require.Equal(t, packs.RoundFinal, p.Rounds[1].Type)
	require.Equal(t, 4, rep.Stats.MediaImported)
	require.Equal(t, map[string]int{"simple": 6, "stake": 1, "secretPublicPrice": 1, "secretNoQuestion": 1, "noRisk": 1, "secret": 3, "myType": 1, "html": 0}["stake"], rep.Stats.ByType["stake"])
	require.Equal(t, 3, rep.Stats.ByType["secret"])
	require.Equal(t, 1, rep.Stats.ByType["myType"])
}

// TestImportVersionDetection covers a missing version attribute and a mixed
// document (v5 <params> next to a v4 <type>).
func TestImportVersionDetection(t *testing.T) {
	noVersion := `<package name="x"><rounds><round name="r"><themes><theme name="t"><questions>
	<question price="100"><scenario><atom>q</atom></scenario><right><answer>a</answer></right></question>
	</questions></theme></themes></round></rounds></package>`
	_, rep, err := importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte(noVersion)}}), newFakeStore(), ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, 4, rep.Version, "no version + <scenario> means v4")

	noVersionV5 := `<package name="x"><rounds><round name="r"><themes><theme name="t"><questions>
	<question price="100"><params><param name="question" type="content"><item>q</item></param></params><right><answer>a</answer></right></question>
	</questions></theme></themes></round></rounds></package>`
	_, rep, err = importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte(noVersionV5)}}), newFakeStore(), ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, 5, rep.Version)

	mixed := `<package name="x" version="5" xmlns="https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd"><rounds><round name="r"><themes><theme name="t"><questions>
	<question price="100"><type name="bagcat"><param name="theme">T</param><param name="cost">[10;20]/5</param><param name="self">true</param></type>
	<params><param name="question" type="content"><item>q</item></param></params><right><answer>a</answer></right></question>
	<question price="200" type="auction"><params><param name="question" type="content"><item>q</item></param><param name="myParam">v</param></params><right><answer>a</answer></right></question>
	</questions></theme></themes></round></rounds></package>`
	p, rep, err := importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte(mixed)}}), newFakeStore(), ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, 5, rep.Version)
	q := p.Rounds[0].Themes[0].Questions[0]
	require.Equal(t, packs.QSecret, q.Type)
	require.Equal(t, "T", q.Params.Theme)
	require.Equal(t, &packs.NumberSet{Min: 10, Max: 20, Step: 5}, q.Params.Price)
	require.Equal(t, packs.SelectAny, q.Params.SelectionMode)
	require.Equal(t, "q", q.Params.Question[0].Text)
	q2 := p.Rounds[0].Themes[0].Questions[1]
	require.Equal(t, packs.QStake, q2.Type, "legacy names in the type attribute are mapped")
	require.Equal(t, []packs.Param{{Name: "myParam", Value: "v"}}, q2.Extra)
	require.Equal(t, 1, rep.Count(CodeUnknownParam))

	newer := `<package name="x" version="6"><rounds/></package>`
	_, rep, err = importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte(newer)}}), newFakeStore(), ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, 5, rep.Version)
	require.Equal(t, 1, rep.Count(CodeUnsupportedVersion))
}

func TestImportScriptAndPrefixedNamespace(t *testing.T) {
	xmlDoc := `<?xml version="1.0" encoding="utf-8"?>
<s:package xmlns:s="https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd" name="ns" version="5">
<s:rounds><s:round name="r" type="table"><s:themes><s:theme name="t"><s:questions>
<s:question price="100" type="custom">
<s:params><s:param name="question" type="content"><s:item>Q</s:item></s:param></s:params>
<s:script>
<s:step type="showContent"><s:param name="content" type="content"><s:item>Question text in script</s:item></s:param></s:step>
<s:step type="askAnswer"><s:param name="mode">button</s:param><s:param name="fallbackRefId" isRef="True">right</s:param></s:step>
<s:step type="accept"><s:param name="answer" type="content"><s:item>Answer in script</s:item></s:param></s:step>
</s:script>
<s:right><s:answer>A</s:answer></s:right></s:question>
</s:questions></s:theme></s:themes></s:round></s:rounds></s:package>`
	p, rep, err := importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte(xmlDoc)}}), newFakeStore(), ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, packs.RoundStandard, p.Rounds[0].Type)
	q := p.Rounds[0].Themes[0].Questions[0]
	require.Equal(t, packs.QCustom, q.Type)
	require.Len(t, q.Script, 3)
	require.Equal(t, "askAnswer", q.Script[1].Type)
	require.Equal(t, packs.Param{Name: "mode", Value: "button"}, q.Script[1].Params[0])
	require.Equal(t, packs.Param{Name: "fallbackRefId", IsRef: true, Value: "right"}, q.Script[1].Params[1])
	require.Equal(t, "content", q.Script[0].Params[0].Type)
	require.Equal(t, "Question text in script", q.Script[0].Params[0].Items[0].Text)
	require.Equal(t, 1, rep.Count(CodeCustomScript))
}

func TestImportV4GroupMarkersDropped(t *testing.T) {
	content := `<package name="x" version="4"><rounds><round name="r"><themes><theme name="t"><questions>
	<question price="100"><scenario><atom>Q</atom><atom type="groupStart"/><atom>opt1</atom><atom>opt2</atom><atom type="groupEnd"/></scenario><right><answer>opt1</answer></right></question>
	</questions></theme></themes></round></rounds></package>`
	p, rep, err := importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte(content)}}), newFakeStore(), ImportOptions{})
	require.NoError(t, err)
	items := p.Rounds[0].Themes[0].Questions[0].Params.Question
	require.Len(t, items, 3)
	for _, it := range items {
		require.Equal(t, packs.ContentText, it.Type)
		require.NotEmpty(t, it.Text)
	}
	require.Equal(t, 2, rep.Count(CodeMediaUnsupported))
	require.Empty(t, entriesWithLevel(rep, LevelError))
}
