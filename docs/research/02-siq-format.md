# SIQ (SIGame package) format — importer/exporter specification

Scope: the `.siq` container and `content.xml` in **version 4** (legacy, `ygpackage3.0.xsd`, still the majority of packs in the wild) and **version 5** (current, `siq_5.xsd`). Everything below is derived from the primary sources: the XSDs and the `SIPackages` C# reference implementation in `VladimirKhil/SI` (the code, not the XSD, is what actually decides what loads), the project wiki, the canonical demo packages `SIGameTest.siq` (v4) and `SIGameTestNew.siq` (v5) shipped as unit-test fixtures, and the official TypeScript serializer in `VladimirKhil/SIOnline`. Where a rule comes from observed behavior rather than documentation, it is marked *(observed)*.

Local copies used for this research (all under the scratchpad): `sirepo/` (sparse clone of VladimirKhil/SI: `assets/`, `src/Common/SIPackages`, `src/Common/SIEngine.Core`, `src/SIQuester/SIQuester.ViewModel`, `test/`), `siwiki/` (clone of the SI wiki), `samples/` and `thirdparty/` (downloaded packs and parser sources).

---

## 1. Container: the ZIP archive

A `.siq` is a plain ZIP (rename to `.zip` to inspect). The reference reader (`ZipSIPackageContainer`, `SIDocument`) treats it as follows.

| Entry | Required | Notes |
|---|---|---|
| `content.xml` | yes | Package metadata + all rounds/themes/questions. Written with `CompressionLevel.Optimal` (deflate). UTF-8 **with BOM**, `<?xml version="1.0" encoding="utf-8"?>` declaration, no indentation *(observed in every official pack; strip the BOM before parsing)*. |
| `Images/` `Audio/` `Video/` `Html/` | no | One folder per media category (`CollectionNames`: `Images`, `Audio`, `Video`, `Html`). Media entries are written **stored (NoCompression)**. Mapping content type → folder: `image→Images`, `audio→Audio`, `video→Video`, `html→Html`. |
| `Texts/authors.xml`, `Texts/sources.xml` | no (v4 only) | Global author/source records referenced by `@guid` links. Since v5 they live in `<global>` inside `content.xml`; the loader migrates them (`SIDocument.Upgrade`) and the saver writes `Texts/*.xml` only if the legacy lists are non-empty (i.e. never for a v5 document). Older packs contain empty stubs: `<?xml version="1.0" encoding="utf-8"?><Authors xmlns:xsd=... xmlns:xsi=... />`. |
| `[Content_Types].xml` | no | Open-Packaging-Conventions leftover from the original `System.IO.Packaging` implementation. The wiki: "exists only for backward compatibility. There is no need to use it." Current SIPackages neither reads nor writes it. Observed bodies: `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="si/xml" /></Types>` (most packs; very old ones use `si/old`); packs saved by older SIQuester add `<Default Extension="mp3" ContentType="si/audio" />`, `jpg/png/gif/jpeg → si/image`, etc. **Exporter: emit the one-line `si/xml` variant for maximum compatibility with old clients; importer: ignore.** |
| `quality.marker` | no | Zero-byte file. Presence ⇒ `Package.HasQualityControl = true` ("maximum size 150 MB; limited media file size and extensions; forbidden external links"). Written/deleted on save. |

Anything else is ignored; the server-side extractor (`ZipExtractorExtensions`) keeps only `content.xml`, `quality.marker` and files whose directory is `Images|Audio|Video|Html|Texts`, and re-names media to hashes ("guarantees that we never use user-provided file names").

Enforced/advisory limits (`PackageLimits`, applied by the game server when loading): tags/authors/sources per item 10, rounds 50, themes per round 30, questions per theme 30, script steps 30, parameters 30, content items per parameter 30, generic text length 350 chars, content item value length 1500 chars (longer strings are truncated with `…`). SIContentService defaults: max package 100 MB (150 MB with quality marker).

### 1.1 Media file naming and URL-encoding (the important quirk)

* **Write path (reference impl):** entry name = `{Category}/{Uri.EscapeUriString(fileName)}` — the source literally says `// TODO: do not escape after everyone updates to the new version`. `Uri.EscapeUriString` percent-encodes (UTF-8 bytes) every character except RFC 2396 *unreserved* (`A-Z a-z 0-9 - _ . ! ~ * ' ( )`) **and** it leaves URI *reserved* characters untouched (`; / ? : @ & = + $ , #` and `[ ]`). So `Снимок6.PNG` → `%D0%A1%D0%BD%D0%B8%D0%BC%D0%BE%D0%BA6.PNG`, a space → `%20`, `%` → `%25`, but `(8)`, `[alliance]`, `+`, `&`, `#` stay as-is. Observed entry: `Images/[alliance]%20%20King%20of%20Baking%20Kim%20Tak%20Goo%20E17-0-47-21-574.jpg` referenced from XML as `[alliance]  King of Baking Kim Tak Goo E17-0-47-21-574.jpg`. The XML always holds the **decoded** name. The wiki: "File names inside archive may be encoded in URI-format… This encoding is applied for backward compatibility and it is not mandatory" — newer packs (e.g. `SIGameTestNew.siq`) store raw names.
* **Read path:** the loader builds, per category, a map `Uri.UnescapeDataString(entry.Name) → entry.Name` and resolves XML references through it; `SIDocument.TryGetMedia` additionally retries with `Uri.EscapeUriString(link)`. `UnescapeDataString` decodes all `%XX` but does **not** turn `+` into space.
* **Importer rules that survive real-world packs** *(observed in donebd/SIGame, kkqr77/siqeditor, Spring3/siq2json)*: (1) index every entry under both its raw name and its percent-decoded name; (2) match case-insensitively and after Unicode NFC normalization (macOS-made zips are NFD); (3) tolerate a leading `@` left inside the entry name (`Audio/@track.mp3`); (4) tolerate entries in the wrong folder or at the root; (5) old zips without the UTF-8 flag (bit 11) have cp866-encoded names — siq2json decodes with `iconv-lite` cp866 before `decodeURIComponent`; (6) never fail the whole pack on one missing file.
* **Exporter recommendation:** store raw UTF-8 names with the ZIP UTF-8 flag set (as current SIQuester does), avoid `/`, `\`, `%`, `#`, `?` in names, or — for maximum compatibility with 2013-era clients — apply the `EscapeUriString` rule exactly. Never double-encode.

### 1.2 The `@` prefix

* **v4:** inside `<atom>` text of type `image|voice|video|html`, `@name.ext` means "file in the matching folder of this package"; a value without `@` is an external URL (`Atom.IsLink => text[0]=='@'`). Same for `package/@logo`.
* **v5:** content items use `isRef="True"` instead of `@`; the only place `@` survives is the `logo` attribute (`logo="@file.png"` = `Images/file.png`, otherwise treated as external URL) and author/source links `@guid` / `@guid#tail` (`#tail` = specification such as page number, e.g. `@7ab08cfa-...#с.256`).

### 1.3 `<files>` hashes (v5, optional)

`<files><file name="Images/x.png" hash="…SHA-256 HEX UPPERCASE…"/>…</files>` — key is `{Category}/{decodedFileName}`, sorted ordinally; computed on save for every media entry. Purpose: media de-duplication/reporting. Not required for loading.

---

## 2. `content.xml` — common structure (both versions)

Hierarchy: `package → rounds/round → themes/theme → questions/question`. The reader is namespace-agnostic (matches on `LocalName`), so a missing/wrong `xmlns` still loads. Every level may carry `<info>`. Authors and sources **inherit downward** (a round without authors has the package's authors).

**Root `<package>` attributes** (writer order: `name, version, id, restriction, date, publisher, contactUri, difficulty, logo, language`, then `xmlns`):

| attr | v4 | v5 | Semantics / writer rule |
|---|---|---|---|
| `name` | req | req | Package title |
| `version` | req (`4`) | req (`5`) | Double; reader rejects `> 5` (`UnsupportedPackageVersionException`); `< 5` triggers in-memory upgrade; writer always emits `5` |
| `id` | req (xsd) | opt | GUID string; written if non-empty |
| `restriction` | opt | opt | Free text, e.g. `12+`, `18+` |
| `date` | opt | opt | Free text (`31.12.2014`, `2005 год`); SIQuester writes `dd.MM.yyyy` |
| `publisher` | opt | opt | |
| `contactUri` | — | opt | v5 addition |
| `difficulty` | opt | opt | 0–10 (default 5); written only when > 0 |
| `logo` | opt | opt | `@file` in `Images/` or external URL |
| `language` | opt | opt | e.g. `ru-RU`, `en-US` |
| `generator` | — | opt (xsd) | tool name; not written by SIPackages |
| `xmlns` | `http://vladimirkhil.com/ygpackage3.0.xsd` | `https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd` | |

**Child element order as written by SIPackages v5:** `tags` → `global` → `files` → `info` → `rounds`. v4 XSD order: `tags`, `info`, `rounds` (v4 `info` and `rounds` mandatory in the XSD; in practice the reader tolerates absence).

* `<tags><tag>Cinema</tag>…</tags>` — free-text package themes.
* `<info>`: `<authors><author>…</author></authors>`, `<sources><source>…</source></sources>`, `<comments>…</comments>`, **v5 only** `<showmanComments>…</showmanComments>` (shown only to the host), `<extension>` (reserved; ignored by the current reader). In v4 XSD `authors/sources/comments` are all mandatory (empty elements are common); the writer in v5 omits `<info>` entirely when empty.
* Author/source strings are either plain text or links `@id` (author) / `@id#tail` (source) into the global records.
* **v5 `<global>`** (replaces `Texts/*.xml`):
  ```xml
  <global>
    <Authors><Author id="ae9f7eb2-6091-4b34-97a1-0f74ad193d57"><Name/><SecondName/><Surname/><Country/><City/></Author></Authors>
    <Sources><Source id="7ab08cfa-7f68-4fd4-a400-f4ac26b33a9d"><Author/><Title/><Year>2004</Year><Publish/><City/></Source></Sources>
  </global>
  ```
  (`Year` is an int; all fields optional in practice; the same element bodies are used in v4 `Texts/authors.xml` / `sources.xml` with roots `<Authors>` / `<Sources>`.)
* `<rounds><round name="…" type="…"><info/><themes><theme name="…"><info/><questions><question …/></questions></theme></themes></round></rounds>`
  * `round/@type`: `standart` (sic — default, **omitted on write**) or `final`. Reserved future aliases (wiki "Future renamings"): `standart→table`, `final→themeList`. Final round semantics: players remove themes one by one until one is left; only the **first** question of each theme is played; its price is ignored (everyone stakes).
  * `question/@price`: int (v4 XSD says unsignedInt). `price="-1"` = placeholder/empty question (`Question.InvalidPrice`); final-round questions usually use `0`.
  * `<right><answer>…</answer>*</right>` — required; the reader inserts one empty answer if none exist, so `<right><answer /></right>` round-trips. `<wrong><answer>…</answer>*</wrong>` — optional (used for bot/host validation).

---

## 3. Version 4 question model (`ygpackage3.0.xsd`)

```xml
<question price="300">
  <info><authors><author>…</author></authors><sources><source>…</source></sources><comments>…</comments></info>
  <type name="bagcat">
    <param name="theme">Новая тема</param>
    <param name="cost">[100;1000]/200</param>
    <param name="self">true</param>
    <param name="knows">after</param>
  </type>
  <scenario>
    <atom type="say">Устный комментарий</atom>
    <atom time="8">Текст на экране 8 секунд</atom>
    <atom time="-1" type="image">@sample-boat-400x300.png</atom>
    <atom type="voice">@sample-3s.mp3</atom>
    <atom type="marker" />
    <atom type="video">@answer.mp4</atom>
  </scenario>
  <right><answer>Ответ</answer><answer>Альтернативный ответ</answer></right>
  <wrong><answer>Неверная версия</answer></wrong>
</question>
```

* `<type name="…">` with `<param name="…">value</param>` children. Missing `<type>` ⇒ `simple`. Well-known v4 types (official registry `https://vladimirkhil.com/content/docs/QuestionsTypes.xml`, mirrored at `/si/qtypes`):
  | v4 type | Params | Rules |
  |---|---|---|
  | `simple` | — | Button question; first to press answers; wrong ⇒ −price and others may press |
  | `auction` | — | Sequential stakes starting at nominal; all-in only tops a higher stake; highest stake plays alone, price = stake |
  | `cat` | `theme`, `cost` | "Кот в мешке": must be given to another player; theme & cost unknown until given |
  | `bagcat` | `theme`, `cost`, `self` (`true|false`), `knows` (`before|after|never`) | Generalised cat. `cost`: `N>0` fixed; `0` = choose the round's min or max price; `[a;b]/h` = pick from a..b step h; `[a;b]` = only a or b. `knows=never` ⇒ no question, the cost is simply credited |
  | `sponsored` | — | "Без риска": only the opener answers; right ⇒ +2×nominal, wrong ⇒ 0 |
  | any other | arbitrary | custom type, played manually |
* `<scenario>` = ordered `<atom>` list. `type` ∈ XSD enum `text`(default, attribute omitted) `say` (host replic) `image` `voice` (audio) `video` `marker`; the reader also accepts `audio` and `html` (used with external URLs, e.g. a YouTube embed with `time="35"`). `time` = integer seconds the fragment stays on screen; **`time="-1"` means "do not wait, play together with the next atom"**. `marker` splits question atoms (before) from answer atoms (after) — multiple markers: only the first counts. Media atoms: `@name` = package file, otherwise external URL.
* Obsolete intermediate syntax `<atom type="groupStart"/>…<atom type="groupEnd"/>` (multiple-choice options) existed briefly before v5 and is not handled by current SIPackages.

## 4. Version 5 question model (`siq_5.xsd`)

```xml
<question price="300" type="secret">
  <info>…</info>
  <params>
    <param name="theme">Новая тема</param>
    <param name="price" type="numberSet"><numberSet minimum="100" maximum="1000" step="200" /></param>
    <param name="selectionMode">any</param>
    <param name="question" type="content">
      <item placement="replic">Устный комментарий</item>
      <item duration="00:00:08">Текст на экране 8 секунд</item>
      <item type="image" isRef="True" waitForFinish="False">sample-boat-400x300.png</item>
      <item type="audio" isRef="True" placement="background">sample-3s.mp3</item>
    </param>
    <param name="answer" type="content">
      <item type="video" isRef="True">answer.mp4</item>
    </param>
  </params>
  <right><answer>Ответ</answer></right>
  <wrong><answer>Неверная версия</answer></wrong>
</question>
```

* `question/@type` string: `""` (absent = default/simple), `stake`, `stakeAll`, `secret`, `secretPublicPrice`, `secretNoQuestion`, `noRisk`, `forAll`, or any custom name. Reserved aliases (constants exist, treated as replacements): `simple`/`withButton` for default, `forYourself` for `noRisk`. Legacy names `auction`, `cat`, `bagcat`, `sponsored` are converted on load (see §6) and must not be written.
* `<params>` is **always written** (even `<params/>`); `<script>` is optional (only for custom step-by-step play); deprecated `<type>`/`<scenario>` are still accepted by the XSD/reader for v4 content.
* **Parameter (`<param name="…" type="…">`) types** (`StepParameterTypes`): `simple` (default, attribute omitted, text value), `content` (list of `<item>`), `group` (nested `<param>`s), `numberSet` (`<numberSet minimum maximum step/>`). A `<param>` that is an **empty element** (`<param name="x"/>`) is skipped by the reference reader — always emit a value. `isRef="True"` on a `simple` param means "reference to another parameter by name" (used in scripts only).
* **Content `<item>`** (`ContentItem`): text value or file name/URL. Attributes and writer rules:
  | attr | values | default | written when |
  |---|---|---|---|
  | `type` | `text` `image` `audio` `video` `html` | `text` | ≠ text |
  | `isRef` | `True`/`False` (parsed case-insensitively) | `False` | true — value is a file name in the type's folder; false ⇒ value is inline text or an external URL |
  | `placement` | `screen` (table/main screen: text, image, video, html), `replic` (host says it: text only), `background` (invisible: audio only) | `background` for `audio`, else `screen` | when explicitly set and ≠ default; for `audio` the writer always emits `placement="background"` |
  | `duration` | `hh:mm:ss` (XSD `\d{2}:\d{2}:\d{2}`, parsed with `TimeSpan.TryParse`) | `00:00:00` (= auto) | ≠ 0 |
  | `waitForFinish` | `True`/`False` | `True` | false — item is played together with the following item (multiple images on screen, text + background audio) |
  Items of one parameter play sequentially unless `waitForFinish="False"`.
* **Well-known question parameters** (`QuestionParameterNames`):
  | name | type | meaning |
  |---|---|---|
  | `question` | content | mandatory body |
  | `answer` | content | optional media/text shown as the answer (replaces v4 post-marker atoms); text answers stay in `<right>` |
  | `theme` | simple | secret-question theme override |
  | `price` | numberSet | secret-question price range: `min=max=N` fixed; `min=max=0` choose round min/max; `min<max, step>0` values `min, min+step … ≤ max`; `step=0` or `step=max-min` only the two extremes |
  | `selectionMode` | simple | `any` (may keep it) / `exceptCurrent` (must give to someone else) |
  | `answerType` | simple | `text` (default, param absent), `select`, `number`, `point`, `client` |
  | `answerOptions` | group | for `select`: nested `<param name="A" type="content"><item>…</item></param>` … labels `A`,`B`,… (`index<26 ? 'A'+index : "A"+n`); engine uses only the first item of each option and requires ≥ 2; `<right><answer>C</answer>` holds the **label** |
  | `answerDeviation` | simple | `number`: integer ± tolerance around the integer in `<right>`; `point`: double tolerance for a point chosen on the last image of `question` (right-answer string format not verified here; kkqr77/siqeditor writes `x,y,ratio`) |
  | `answerDuration` | simple | integer seconds for the answer phase (`askAnswer` step duration) |
* **Question types and required params (v5)** — from wiki *Question types* + `ScriptsLibrary` (each built-in type is literally a script):
  | type | extra params | play rules |
  |---|---|---|
  | `""`/`simple` | — | setAnswerType → showContent(question) → askAnswer(mode=button) → showContent(answer, fallback=right) |
  | `stake` | — | setAnswerer(mode=stake, select=highest, stakeVisibility=visible) → question → askAnswer(direct) |
  | `stakeAll` | — | setAnswerer(mode=stake, select=allPossible, stakeVisibility=hidden): everyone stakes and answers hidden; each wins/loses own stake |
  | `secret` | `theme`, `price`, `selectionMode` | setAnswerer(byCurrent, select=@selectionMode) → setTheme → announcePrice → setPrice(mode=select) → question → askAnswer(direct) |
  | `secretPublicPrice` | same | theme + price announced **before** the answerer is chosen |
  | `secretNoQuestion` | `price`, `selectionMode` | setAnswerer → setTheme → announcePrice → setPrice → **accept** (money credited, no content) |
  | `noRisk` | — | setAnswerer(mode=current) → setPrice(mode=multiply ×2; wrong = 0) → question → askAnswer(direct) |
  | `forAll` | — | setAnswerer(mode=all): everyone answers hidden for ± nominal |
  | custom | any simple params | not auto-played; host plays manually |
* **`<script>`** (rare, "reserved for very complex scenarios"): `<script><step type="…"><param …/></step>…</script>`; step types `setAnswerType, showContent (default, attribute omitted), askAnswer, setAnswerer, announcePrice, setPrice, setTheme, accept`; step params `content, mode, select, stakeVisibility, fallbackRefId, type, options, duration`; values: `askAnswer.mode=button|direct`; `setAnswerer.mode=stake|current|byCurrent|all`, `.select=allPossible|highest|any|exceptCurrent`, `.stakeVisibility=visible|hidden`; `setPrice.mode=multiply|select`; `setAnswerType.type=text|number|point|select|client`. A step param may be a reference: `<param name="content" isRef="True">question</param>`; `<param name="fallbackRefId" isRef="True">right</param>` = use the first right answer when the referenced param is missing. Example from the unit tests:
  ```xml
  <script>
    <step type="showContent"><param name="content" type="content"><item>Question text in script</item></param></step>
    <step type="accept"><param name="answer" type="content"><item>Answer in script</item></param></step>
  </script>
  ```

## 5. Complete examples

**v4 (excerpt of the canonical demo `SIGameTest.siq`):**
```xml
<?xml version="1.0" encoding="utf-8"?>
<package name="Тестовый пакет SIGame" version="4" id="a16f96e7-4616-47d0-8652-aee5196094af" restriction="12+" date="07.07.2022" publisher="Группа" difficulty="5" logo="@sigame_-_ava_2019.png" language="ru-RU" xmlns="http://vladimirkhil.com/ygpackage3.0.xsd">
  <tags><tag>Тема пакета 1</tag></tags>
  <info><authors><author>Vladimir Khil</author></authors><comments>…</comments></info>
  <rounds>
    <round name="1-й раунд">
      <info><sources><source>Источник раунда</source></sources></info>
      <themes>
        <theme name="Вопросы разных типов">
          <questions>
            <question price="100"><scenario><atom>Это текст вопроса</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="200"><type name="auction" /><scenario><atom>Вопрос со ставкой</atom></scenario><right><answer>Ответ</answer></right><wrong><answer>Неверно</answer></wrong></question>
            <question price="600"><type name="bagcat"><param name="theme">Новая тема</param><param name="cost">[100;500]</param><param name="self">false</param><param name="knows">before</param></type><scenario><atom>…</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="700"><type name="bagcat"><param name="theme">-</param><param name="cost">700</param><param name="self">false</param><param name="knows">never</param></type><scenario><atom>…</atom></scenario><right><answer /></right></question>
            <question price="800"><type name="sponsored" /><scenario><atom>Вопрос без риска</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="-1"><right /></question>
            <question price="600"><scenario><atom time="-1">Текст</atom><atom type="voice">@sample-3s.mp3</atom></scenario><right><answer>Текст + звук</answer></right></question>
            <question price="800"><scenario><atom type="image">https://vladimirkhil.com/images/svoyak-soft.jpg</atom></scenario><right><answer>Внешняя ссылка</answer></right></question>
            <question price="900"><scenario><atom>Медиа в ответе</atom><atom type="marker" /><atom type="image">@sample-boat-400x300.png</atom></scenario><right><answer>Ответ</answer></right></question>
            <question price="1200"><scenario><atom time="35" type="html">https://www.youtube.com/embed/a3ICNMQW7Ok?controls=0&amp;autoplay=1</atom></scenario><right><answer>HTML</answer></right></question>
          </questions>
        </theme>
      </themes>
    </round>
    <round name="Финал" type="final"><themes><theme name="Тема 1"><questions><question price="0"><scenario><atom>Вопрос финала 1</atom></scenario><right><answer>Ответ</answer></right></question></questions></theme></themes></round>
  </rounds>
</package>
```

**v5 (excerpt of the canonical demo `SIGameTestNew.siq`; ZIP also contains `Images/…`, `Audio/sample-3s.mp3`, `Video/…`, `Html/test.html`, `quality.marker`):**
```xml
<?xml version="1.0" encoding="utf-8"?>
<package name="Тестовый пакет SIGame" version="5" id="a16f96e7-4616-47d0-8652-aee5196094af" restriction="12+" date="07.07.2022" publisher="Группа" contactUri="https://mysite.ru" difficulty="5" logo="@sigame_-_ava_2019.png" language="ru-RU" xmlns="https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd">
  <tags><tag>Тема пакета 1</tag></tags>
  <info><authors><author>Vladimir Khil</author></authors><comments>…</comments></info>
  <rounds>
    <round name="1">
      <themes>
        <theme name="Вопросы разных типов">
          <questions>
            <question price="100"><params><param name="question" type="content"><item>Это текст вопроса</item></param></params><right><answer>Ответ</answer></right></question>
            <question price="200" type="stake"><params><param name="question" type="content"><item>Вопрос со ставкой</item></param></params><right><answer>Ответ</answer></right><wrong><answer>Неверно</answer></wrong></question>
            <question price="500" type="secret"><params><param name="theme">Новая тема</param><param name="price" type="numberSet"><numberSet minimum="100" maximum="1000" step="200" /></param><param name="selectionMode">any</param><param name="question" type="content"><item>…</item></param></params><right><answer>Ответ</answer></right></question>
            <question price="600" type="secretPublicPrice"><params><param name="theme">Новая тема</param><param name="price" type="numberSet"><numberSet minimum="100" maximum="500" step="400" /></param><param name="selectionMode">exceptCurrent</param><param name="question" type="content"><item>…</item></param></params><right><answer>Ответ</answer></right></question>
            <question price="700" type="secretNoQuestion"><params><param name="price" type="numberSet"><numberSet minimum="700" maximum="700" step="0" /></param><param name="selectionMode">exceptCurrent</param></params><right><answer /></right></question>
            <question price="800" type="noRisk"><params><param name="question" type="content"><item>…</item></param></params><right><answer>Ответ</answer></right></question>
            <question price="-1"><params /><right /></question>
            <question price="900" type="custom"><params><param name="question" type="content"><item>Свой тип</item></param></params><right><answer>Ответ</answer></right></question>
            <question price="1000" type="stakeAll"><params><param name="question" type="content"><item>…</item></param></params><right><answer>Ответ</answer></right></question>
            <question price="1100" type="forAll"><params><param name="question" type="content"><item>…</item></param></params><right><answer>Ответ</answer></right></question>
          </questions>
        </theme>
        <theme name="Контент вопросов">
          <questions>
            <question price="200"><params><param name="question" type="content"><item placement="replic">Устный текст</item><item duration="00:00:08">Второй фрагмент, 8 секунд</item></param></params><right><answer>Ответ</answer></right></question>
            <question price="400"><params><param name="question" type="content"><item type="audio" isRef="True" placement="background">sample-3s.mp3</item></param></params><right><answer>Аудио</answer></right></question>
            <question price="600"><params><param name="question" type="content"><item waitForFinish="False">Текст</item><item type="audio" isRef="True" placement="background">sample-3s.mp3</item></param></params><right><answer>Текст + звук</answer></right></question>
            <question price="900"><params><param name="question" type="content"><item>Медиа в ответе</item></param><param name="answer" type="content"><item type="image" isRef="True">sample-boat-400x300.png</item></param></params><right><answer>Текстовый ответ для проверки</answer></right></question>
            <question price="1200"><params><param name="question" type="content"><item type="image" isRef="True" waitForFinish="False">a.jpeg</item><item type="image" isRef="True" waitForFinish="False">b.jpeg</item><item type="image" isRef="True">c.jpeg</item></param></params><right><answer>Несколько изображений</answer></right></question>
            <question price="1500"><params><param name="question" type="content"><item type="html" isRef="True">test.html</item></param></params><right><answer>Ответ</answer></right></question>
          </questions>
        </theme>
        <theme name="Дополнительно">
          <questions>
            <question price="100"><params><param name="question" type="content"><item>Вопрос с вариантами ответа</item></param><param name="answerType">select</param><param name="answerOptions" type="group"><param name="A" type="content"><item>Вариант 1</item></param><param name="B" type="content"><item>Вариант 2</item></param><param name="C" type="content"><item>Вариант 3</item></param><param name="D" type="content"><item>Вариант 4</item></param></param></params><right><answer>C</answer></right></question>
            <question price="200"><params><param name="question" type="content"><item>Вопрос с числовым ответом</item></param><param name="answerType">number</param><param name="answerDeviation">5</param></params><right><answer>100</answer></right></question>
          </questions>
        </theme>
      </themes>
    </round>
    <round name="Финал" type="final">…</round>
  </rounds>
</package>
```

## 6. v4 → v5 migration (exact algorithm of `Question.Upgrade`, applied on load when `version < 5`)

1. `price == -1` ⇒ type `""`, stop.
2. Type mapping: `simple`→`""`; `auction`→`stake`; `sponsored`→`noRisk`; `cat`→`secret` (as `bagcat` with `knows=after`, `self=false`); `bagcat` → by `knows`: `after`/missing→`secret`, `before`→`secretPublicPrice`, `never`→`secretNoQuestion`. Params: `theme`→`theme` (not for `never`), `cost` string → `price` numberSet (`N` → min=max=N step 0; regex `\[(\d+);(\d+)\](/(\d+))?`, step defaults to max−min), `self=true`→`selectionMode=any`, `self=false`/missing→`exceptCurrent`; `knows` dropped. Any other type name is kept and each `<param>` becomes a simple param. For `secretNoQuestion` the scenario is discarded.
3. Scenario → `question` content param; atoms after the first `marker` → `answer` content param (only if non-empty). Per atom: `say`→`text`+`placement=replic`; `voice`→`audio`+`placement=background`; other types as-is; text starting with `@` ⇒ `isRef=true`, value without `@`; `time=-1` ⇒ `waitForFinish=false`, duration 0; `time=N` ⇒ `duration=N s`.
4. `Texts/authors.xml` + `sources.xml` → `<global>`.

**Exporter to v4 (downgrade)** is the inverse: `stake→auction`, `noRisk→sponsored`, `secret/secretPublicPrice/secretNoQuestion → bagcat` with `knows=after|before|never`, `self = (selectionMode==any)`, `cost` = `N` / `[a;b]` / `[a;b]/h`; items → atoms (`replic`→`say`, `audio`→`voice`, `isRef` ⇒ prepend `@`, `waitForFinish=false` ⇒ `time="-1"`, `duration` ⇒ `time` seconds), `answer` items after `<atom type="marker"/>`; `stakeAll`, `forAll`, `answerType=select/number/point`, `showmanComments`, `contactUri`, `Html/` refs have **no v4 equivalent** (emit as custom type / drop with a warning). Recommendation: write v5 only; SIGame has loaded v5 since 2023 and every active client (SIGame desktop, SIOnline, SIQuester) reads both.

## 7. Importer / exporter checklist

* Parse with a namespace-agnostic XML reader; strip BOM; treat all attribute values as strings; ignore unknown elements/attributes instead of failing (the official reader does).
* Accept `question/@type` **and** `question/type/@name`; accept `<params>` **and** `<scenario>` in the same document (mixed packs exist after partial edits).
* Booleans: accept `true/True/false/False`; **emit `True`/`False`** (what SIQuester, SIOnline, siqeditor, next-game write).
* `duration`: emit `hh:mm:ss`; accept anything `TimeSpan.TryParse` accepts.
* Always emit `<params>` and `<right>` (with at least `<answer/>`); omit `type="text"`, `type="simple"`, `placement="screen"`, `round type="standart"`, `difficulty="0"`.
* Media: ZIP entry per file under `Images/Audio/Video/Html`, stored uncompressed, UTF-8 names (or `EscapeUriString`-encoded for legacy); reference by bare file name with `isRef="True"`; external media = absolute URL with `isRef` absent.
* Round-trip unknown params verbatim (SIOnline's serializer shows the pattern: preserve `_attributes`/`_children` for unknown `<param>`s).

## 8. Existing parsers / libraries

| Project | Lang / license | siq support | Notes |
|---|---|---|---|
| [VladimirKhil/SI → `src/Common/SIPackages`](https://github.com/VladimirKhil/SI/tree/master/src/Common/SIPackages) | C# / MIT | read v4+v5, write v5 | Reference implementation (`SIDocument`, `Package`, `Question`, `ContentItem`, `ScriptsLibrary`); README shows `dotnet add package SIPackages` |
| [VladimirKhil/SIOnline → `src/model/siquester`](https://github.com/VladimirKhil/SIOnline/tree/master/src/model/siquester) | TypeScript / GPL-3.0, active 2026-09 | read v4+v5 (`packageLoader.ts` incl. v4 upgrade & `[a;b]/h` parsing), write v5 (`packageSerializer.ts`), ZIP via JSZip (`packageExporter.ts`) | Official web client; serializer omits `showmanComments`/`global`/`files` |
| [OpenQuester → `client/packages/siq_file`](https://github.com/OpenQuester/OpenQuester/tree/main/client/packages/siq_file) | Dart / MIT, active | read v4+v5 (`content_xml_parser.dart`, `siq_archive_parser.dart`), MD5 media hashing | plus [`docs/specs/siq-compatibility-matrix.md`](https://github.com/OpenQuester/OpenQuester/blob/main/docs/specs/siq-compatibility-matrix.md) — good template for an import report |
| [minmaxmean/sigma → `siq/reader.go`](https://github.com/minmaxmean/sigma) | Go / no license, 2025 | read v4+v5 via `encoding/xml` structs | only Go implementation found |
| [zodo/jeoshow → `engine/src/siq/xml-parser.ts`](https://github.com/zodo/jeoshow) | TypeScript / Unlicense, 2026-03 | read v4+v5 incl. `answerOptions`; media lookup with `encodeURI` | Telegram mini-app |
| [kkqr77/siqeditor](https://github.com/kkqr77/siqeditor) | TypeScript / MIT, active 2026-09 | browser editor; converts v4→v5 in memory; writes v5 with SHA-256 `<files>`; name-variant matching | good source of real-world file-name quirks |
| [donebd/SIGame → `src/services/SiqService.ts`](https://github.com/donebd/SIGame/blob/main/src/services/SiqService.ts) | TypeScript / MIT, 2026-05 | tolerant reader v4+v5 (select options, secret modes) | most exhaustive media-name fallback heuristics |
| [hryhola/next-game](https://github.com/hryhola/next-game) (`shared/lib/siqPackDraft.ts`, `tools/siq-cli`) | TypeScript / GPL-3.0, 2026-04 | write v5 | |
| [Spring3/siq2json](https://github.com/Spring3/siq2json) (npm `siq2json`) | TS / no license, 2023 | read v4 → JSON | handles cp866 zip names + `decodeURIComponent` |
| [Lgmrszd/sigame_tools](https://github.com/Lgmrszd/sigame_tools) | Python / MIT, 2022 | read/write v4 (`datatypes.py`), `.siq ⇄ .jsiq.zip` | |
| [Zverik/si_convert](https://github.com/Zverik/si_convert) (PyPI `si-convert` 1.1.0, 2024-03) | Python | YAML → v4 `.siq` | README: "does NOT support SIQ v5" |
| [Gosunov/pysipack](https://github.com/Gosunov/pysipack), [R2Rprogpower/svoyak_game_builder](https://github.com/R2Rprogpower/svoyak_game_builder), [lonsdale228/sigame_package_generator](https://github.com/lonsdale228/sigame_package_generator) | Python | write v5 (string templates / JSON→siq + validator) | |
| [opensi/opensi-editor](https://github.com/opensi/opensi-editor) | Rust / MIT+Apache-2.0, active | v4 ✔, v5 "in progress" | native + web editor |
| [qekaqeka/sitool](https://github.com/qekaqeka/sitool) | Rust, 2026-03 | embeds `siq_5.xsd` | |
| [Fooxboy/MyOwnGame](https://github.com/Fooxboy/MyOwnGame) (`SiqPackageParser.cs`), [arZamalyutdinov/sigame-parser-unofficial](https://github.com/arZamalyutdinov/sigame-parser-unofficial) (Kotlin), [GTmAster/SiGamePackOptimizer](https://github.com/GTmAster/SiGamePackOptimizer) (C#, image optimizer) | misc | v4 readers | |
| [VityaSchel/SIPacker](https://github.com/VityaSchel/SIPacker) | JS / MIT, 33★, unmaintained | web builder writing v4 (`ygpackage3.0` xmlns, `[Content_Types].xml`, `Texts/*`) | |
| [kharkovdenys/sigame-server](https://github.com/kharkovdenys/sigame-server) | TS / MIT, 2023 | fast-xml-parser + adm-zip; reads only name/date/authors | |
| PyPI `siq` | — | **unrelated** (medical-imaging super-resolution) | do not use |

Pack storages: official SIStorage at `vladimirkhil.com/si/storage` ([VladimirKhil/SIStorage](https://github.com/VladimirKhil/SIStorage): PostgreSQL metadata service, NuGet `VKhil.SIStorage.Client`, npm `sistorage-client`); [SIContentService](https://github.com/VladimirKhil/SIContentService) (uploads/extracts packs with hashed file names; serves `content.xml` only to the game server); community sites [sibrowser.ru](https://www.sibrowser.ru/en) (fed from a VK group), [sigame.ru](https://sigame.ru) (scraped by [`sigame-packs-api`](https://github.com/VityaSchel/sigame-packs-api)), Steam Workshop for [SIGame on Steam](https://store.steampowered.com/app/3553500/SIGame/).

## Sources
- [SIQ file format (version 4) — SI wiki](https://github.com/VladimirKhil/SI/wiki/SIQ-file-format-(version-4))
- [SIQ file format version 5 — SI wiki](https://github.com/VladimirKhil/SI/wiki/SIQ-file-format-version-5)
- [Question types — SI wiki](https://github.com/VladimirKhil/SI/wiki/Question-types)
- [Migrating SIQ file from version 4 to version 5 — SI wiki](https://github.com/VladimirKhil/SI/wiki/Migrating-SIQ-file-from-version-4-to-version-5)
- [Future renamings in package specification — SI wiki](https://github.com/VladimirKhil/SI/wiki/Future-renamings-in-package-specification)
- [Спецификация формата .siq — SI wiki (redirects to v4 page)](https://github.com/VladimirKhil/SI/wiki/%D0%A1%D0%BF%D0%B5%D1%86%D0%B8%D1%84%D0%B8%D0%BA%D0%B0%D1%86%D0%B8%D1%8F-%D1%84%D0%BE%D1%80%D0%BC%D0%B0%D1%82%D0%B0-.siq)
- [siq_5.xsd](https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd), [ygpackage3.1.xsd](https://github.com/VladimirKhil/SI/blob/master/assets/ygpackage3.1.xsd)
- [SIPackages source (SIDocument.cs, Package.cs, Question.cs, ContentItem.cs, StepParameter.cs, ScriptsLibrary.cs, Core/*.cs, Containers/ZipSIPackageContainer.cs)](https://github.com/VladimirKhil/SI/tree/master/src/Common/SIPackages), [SIPackages README](https://github.com/VladimirKhil/SI/blob/master/src/Common/SIPackages/README.md)
- [SIPackages unit-test fixtures (SIGameTest.siq, SIGameTestNew.siq, test.siq, AdvancedTypesTest.xml, ScriptTest.xml, ContentPlacementTest.xml, GlobalDataTest.xml)](https://github.com/VladimirKhil/SI/tree/master/test/Common/SIPackages.Tests)
- [SIEngine.Core/QuestionEngine.cs](https://github.com/VladimirKhil/SI/blob/master/src/Common/SIEngine.Core/QuestionEngine.cs), [SIQuester QuestionViewModel.cs / QuestionsGenerator.cs / IndexLabelHelper.cs](https://github.com/VladimirKhil/SI/tree/master/src/SIQuester/SIQuester.ViewModel)
- [QuestionsTypes.xml (v4 registry)](https://vladimirkhil.com/content/docs/QuestionsTypes.xml), [Реестр типов вопросов SIGame](https://vladimirkhil.com/si/qtypes)
- [Issue #61 "кодировка файлов в архиве *.siq"](https://github.com/VladimirKhil/SI/issues/61)
- [SIGame Package Format Evolution — Vladimir Khil (LinkedIn)](https://www.linkedin.com/pulse/sigame-package-format-evolution-vladimir-khil-okxof)
- [Uri.EscapeUriString — Microsoft Learn](https://learn.microsoft.com/en-us/dotnet/api/system.uri.escapeuristring)
- [SIOnline packageLoader.ts / packageSerializer.ts / packageExporter.ts / package.ts](https://github.com/VladimirKhil/SIOnline/tree/master/src/model/siquester)
- [SIStorage](https://github.com/VladimirKhil/SIStorage), [SIContentService](https://github.com/VladimirKhil/SIContentService)
- [OpenQuester siq_file + compatibility matrix](https://github.com/OpenQuester/OpenQuester), [minmaxmean/sigma](https://github.com/minmaxmean/sigma), [zodo/jeoshow](https://github.com/zodo/jeoshow), [kkqr77/siqeditor](https://github.com/kkqr77/siqeditor), [donebd/SIGame](https://github.com/donebd/SIGame), [hryhola/next-game](https://github.com/hryhola/next-game), [Spring3/siq2json](https://github.com/Spring3/siq2json), [Lgmrszd/sigame_tools](https://github.com/Lgmrszd/sigame_tools), [si-convert on PyPI](https://pypi.org/project/si-convert/), [Gosunov/pysipack](https://github.com/Gosunov/pysipack), [R2Rprogpower/svoyak_game_builder](https://github.com/R2Rprogpower/svoyak_game_builder), [opensi/opensi-editor](https://github.com/opensi/opensi-editor), [qekaqeka/sitool](https://github.com/qekaqeka/sitool), [VityaSchel/SIPacker](https://github.com/VityaSchel/SIPacker), [VityaSchel/sigame-packs-api](https://github.com/VityaSchel/sigame-packs-api), [kharkovdenys/sigame-server](https://github.com/kharkovdenys/sigame-server), [GTmAster/SiGamePackOptimizer](https://github.com/GTmAster/SiGamePackOptimizer), [Fooxboy/MyOwnGame](https://github.com/Fooxboy/MyOwnGame), [arZamalyutdinov/sigame-parser-unofficial](https://github.com/arZamalyutdinov/sigame-parser-unofficial), [siq on PyPI (unrelated)](https://pypi.org/project/siq/)
- [SIBrowser](https://www.sibrowser.ru/en), [Где искать и куда выкладывать паки SIGame — DTF](https://dtf.ru/id614518/4735717-gde-vykladyvat-paki-sigame), [SIGame on Steam](https://store.steampowered.com/app/3553500/SIGame/), [SIQ file extension — filext](https://filext.com/file-extension/SIQ)

## Key facts
- A .siq is a plain ZIP: content.xml (required, UTF-8 with BOM, deflate) + Images/ Audio/ Video/ Html/ (media stored uncompressed) + optional legacy Texts/authors.xml & sources.xml (v4), [Content_Types].xml (OPC leftover, ignored by current code) and quality.marker (0-byte flag).
- Media ZIP entry names are written with Uri.EscapeUriString (spaces/% and all non-ASCII percent-encoded as UTF-8; RFC 2396 unreserved chars and reserved chars like ()[]+&#;/ left as-is); the XML always references the decoded name; readers map Uri.UnescapeDataString(entry) -> entry and retry with the escaped form. Encoding is optional in new packs.
- '@' prefix: in v4 an atom text/logo starting with '@' means a file inside the package (else external URL); in v5 items use isRef="True" and '@' survives only in package/@logo and author/source links (@guid, @guid#tail).
- content.xml hierarchy package>rounds>round>themes>theme>questions>question; every level may have <info> (authors/sources/comments, v5 also showmanComments); authors/sources inherit downward; round/@type is 'standart' (default, omitted) or 'final'.
- v4 question: <type name=simple|auction|cat|bagcat|sponsored|custom> with <param name=theme|cost|self|knows>, <scenario><atom type=text|say|image|voice|video|marker(+audio,html accepted) time=seconds (-1 = don't wait)>; cost syntax N | [a;b] | [a;b]/h | 0 (=round min/max).
- v5 question: type attribute ''|stake|stakeAll|secret|secretPublicPrice|secretNoQuestion|noRisk|forAll|custom; <params> with param types simple|content|group|numberSet; well-known params question, answer, theme, price(numberSet min/max/step), selectionMode(any|exceptCurrent), answerType(text|select|number|point|client), answerOptions(group of content params labelled A,B,C...; right answer = label), answerDeviation, answerDuration.
- v5 <item> attributes: type text|image|audio|video|html (default text), isRef True/False, placement screen|replic|background (audio defaults to background and is always written), duration hh:mm:ss, waitForFinish True/False (False = play together with next item). Empty <param/> elements are skipped by the reference reader.
- v4->v5 upgrade (Question.Upgrade): auction->stake, sponsored->noRisk, cat/bagcat->secret|secretPublicPrice|secretNoQuestion by knows=after|before|never, cost->price numberSet, self->selectionMode, atoms before marker->question param, after marker->answer param, say->text+replic, voice->audio+background, time=-1->waitForFinish=False.
- Built-in question types are literally scripts (ScriptsLibrary): steps setAnswerType/showContent/askAnswer/setAnswerer/announcePrice/setPrice/setTheme/accept with modes button|direct, stake|current|byCurrent|all, highest|allPossible|any|exceptCurrent, visible|hidden, multiply|select; <script> in XML is reserved for custom play.
- Reference implementations: C# SIPackages (VladimirKhil/SI, MIT) and TypeScript packageLoader/packageSerializer in VladimirKhil/SIOnline (GPL-3.0); other parsers: OpenQuester (Dart), minmaxmean/sigma (Go), zodo/jeoshow + kkqr77/siqeditor + donebd/SIGame + hryhola/next-game (TS), siq2json (v4 only), sigame_tools/si-convert/pysipack (Python), opensi-editor/sitool (Rust). PyPI 'siq' is unrelated.
- Server-side limits (PackageLimits): 50 rounds, 30 themes/round, 30 questions/theme, 30 items/param, text 350 chars, content value 1500 chars, 10 tags/authors/sources; SIContentService max pack 100 MB (150 MB with quality.marker).

## Sources
- https://github.com/VladimirKhil/SI/wiki/SIQ-file-format-(version-4)
- https://github.com/VladimirKhil/SI/wiki/SIQ-file-format-version-5
- https://github.com/VladimirKhil/SI/wiki/Question-types
- https://github.com/VladimirKhil/SI/wiki/Migrating-SIQ-file-from-version-4-to-version-5
- https://github.com/VladimirKhil/SI/wiki/Future-renamings-in-package-specification
- https://github.com/VladimirKhil/SI/wiki/%D0%A1%D0%BF%D0%B5%D1%86%D0%B8%D1%84%D0%B8%D0%BA%D0%B0%D1%86%D0%B8%D1%8F-%D1%84%D0%BE%D1%80%D0%BC%D0%B0%D1%82%D0%B0-.siq
- https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd
- https://github.com/VladimirKhil/SI/blob/master/assets/ygpackage3.1.xsd
- https://github.com/VladimirKhil/SI/tree/master/src/Common/SIPackages
- https://github.com/VladimirKhil/SI/blob/master/src/Common/SIPackages/README.md
- https://github.com/VladimirKhil/SI/tree/master/test/Common/SIPackages.Tests
- https://github.com/VladimirKhil/SI/blob/master/src/Common/SIEngine.Core/QuestionEngine.cs
- https://github.com/VladimirKhil/SI/tree/master/src/SIQuester/SIQuester.ViewModel
- https://vladimirkhil.com/content/docs/QuestionsTypes.xml
- https://vladimirkhil.com/si/qtypes
- https://github.com/VladimirKhil/SI/issues/61
- https://www.linkedin.com/pulse/sigame-package-format-evolution-vladimir-khil-okxof
- https://learn.microsoft.com/en-us/dotnet/api/system.uri.escapeuristring
- https://github.com/VladimirKhil/SIOnline/tree/master/src/model/siquester
- https://github.com/VladimirKhil/SIStorage
- https://github.com/VladimirKhil/SIContentService
- https://github.com/OpenQuester/OpenQuester
- https://github.com/OpenQuester/OpenQuester/blob/main/docs/specs/siq-compatibility-matrix.md
- https://github.com/minmaxmean/sigma
- https://github.com/zodo/jeoshow
- https://github.com/kkqr77/siqeditor
- https://github.com/donebd/SIGame
- https://github.com/hryhola/next-game
- https://github.com/Spring3/siq2json
- https://github.com/Lgmrszd/sigame_tools
- https://pypi.org/project/si-convert/
- https://github.com/Gosunov/pysipack
- https://github.com/R2Rprogpower/svoyak_game_builder
- https://github.com/opensi/opensi-editor
- https://github.com/qekaqeka/sitool
- https://github.com/VityaSchel/SIPacker
- https://github.com/VityaSchel/sigame-packs-api
- https://github.com/kharkovdenys/sigame-server
- https://github.com/GTmAster/SiGamePackOptimizer
- https://github.com/Fooxboy/MyOwnGame
- https://github.com/arZamalyutdinov/sigame-parser-unofficial
- https://pypi.org/project/siq/
- https://www.sibrowser.ru/en
- https://dtf.ru/id614518/4735717-gde-vykladyvat-paki-sigame
- https://store.steampowered.com/app/3553500/SIGame/
- https://filext.com/file-extension/SIQ
