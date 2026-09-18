package siq

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"

	"sigame/internal/packs"
)

func TestUnescapeDataString(t *testing.T) {
	cases := map[string]string{
		"plain.png":    "plain.png",
		"my%20pic.png": "my pic.png",
		"a+b.png":      "a+b.png",
		"a%2Bb.png":    "a+b.png",
		"100%25.png":   "100%.png",
		"bad%zz.png":   "bad%zz.png",
		"trail%2":      "trail%2",
		"%D0%A1%D0%BD%D0%B8%D0%BC%D0%BE%D0%BA6.PNG": "Снимок6.PNG",
		"[alliance]%20%20King%20of%20Baking.jpg":    "[alliance]  King of Baking.jpg",
	}
	for in, want := range cases {
		require.Equal(t, want, unescapeDataString(in), in)
	}
}

func TestCP866(t *testing.T) {
	s := "Кот и Ёж №1.jpg"
	enc := encodeCP866(s)
	require.NotEqual(t, s, enc)
	require.Equal(t, s, decodeCP866(enc))
	require.Equal(t, "ascii.png", decodeCP866("ascii.png"))
	require.Equal(t, "А", decodeCP866("\x80"))
	require.Equal(t, "я", decodeCP866("\xEF"))
	require.Equal(t, "ё", decodeCP866("\xF1"))
}

func TestParseDuration(t *testing.T) {
	cases := map[string]int64{
		"00:00:08":     8000,
		"00:01:30":     90000,
		"01:00:00":     3600000,
		"00:00:02.500": 2500,
		"1.00:00:00":   86400000,
		"0:05":         300000,
		"35":           35000,
		"2.5":          2500,
	}
	for in, want := range cases {
		got, ok := parseDuration(in)
		require.True(t, ok, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "abc", "1:2:3:4", "-5", "-00:00:01"} {
		_, ok := parseDuration(bad)
		require.False(t, ok, bad)
	}
	require.Equal(t, "00:00:08", formatDuration(8000))
	require.Equal(t, "00:00:03", formatDuration(2500))
	require.Equal(t, "01:01:01", formatDuration(3661000))
	require.Equal(t, "", formatDuration(400))
}

func TestParseV4Cost(t *testing.T) {
	cases := map[string]xmlNumberSet{
		"700":               {Minimum: "700", Maximum: "700", Step: "0"},
		"0":                 {Minimum: "0", Maximum: "0", Step: "0"},
		"[100;500]":         {Minimum: "100", Maximum: "500", Step: "0"},
		"[100;1000]/200":    {Minimum: "100", Maximum: "1000", Step: "200"},
		" [ 10 ; 20 ] / 5 ": {Minimum: "10", Maximum: "20", Step: "5"},
	}
	for in, want := range cases {
		got, ok := parseV4Cost(in)
		require.True(t, ok, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "abc", "[1;2", "-5", "[a;b]"} {
		_, ok := parseV4Cost(bad)
		require.False(t, ok, bad)
	}
}

func TestSanitizeEntryName(t *testing.T) {
	require.Equal(t, "pic.png", sanitizeEntryName("dir/sub\\pic.png"))
	require.Equal(t, "100.png", sanitizeEntryName("100%.png"))
	require.Equal(t, "ab.png", sanitizeEntryName("a#b?.png"))
	require.Equal(t, "ctl.png", sanitizeEntryName("c\x01t\x7fl.png"))
	require.Equal(t, "track.mp3", sanitizeEntryName("@track.mp3"))
	require.Equal(t, norm.NFC.String("Ёлка.png"), sanitizeEntryName(norm.NFD.String("Ёлка.png")))
	require.Equal(t, "", sanitizeEntryName("   "))
}

// TestMediaNameResolution is the table of real-world file-name quirks.
func TestMediaNameResolution(t *testing.T) {
	nfd := norm.NFD.String("Ёлка.png")
	nfc := norm.NFC.String("Ёлка.png")
	require.NotEqual(t, nfd, nfc)
	cases := []struct {
		name    string
		entry   zipEntry
		itemXML string // <item .../> referencing the file
		wantErr bool
		warn    string // expected warning code, if any
	}{
		{name: "raw", entry: zipEntry{name: "Images/plain.png"}, itemXML: `<item type="image" isRef="True">plain.png</item>`},
		{name: "percent-encoded entry", entry: zipEntry{name: "Images/%D0%A1%D0%BD%D0%B8%D0%BC%D0%BE%D0%BA6.PNG"}, itemXML: `<item type="image" isRef="True">Снимок6.PNG</item>`},
		{name: "space encoded", entry: zipEntry{name: "Images/my%20pic.png"}, itemXML: `<item type="image" isRef="True">my pic.png</item>`},
		{name: "space raw", entry: zipEntry{name: "Images/my pic.png"}, itemXML: `<item type="image" isRef="True">my pic.png</item>`},
		{name: "encoded ref", entry: zipEntry{name: "Images/my pic.png"}, itemXML: `<item type="image" isRef="True">my%20pic.png</item>`},
		{name: "plus stays plus", entry: zipEntry{name: "Images/a+b.png"}, itemXML: `<item type="image" isRef="True">a+b.png</item>`},
		{name: "plus encoded", entry: zipEntry{name: "Images/a%2Bb.png"}, itemXML: `<item type="image" isRef="True">a+b.png</item>`},
		{name: "brackets and spaces", entry: zipEntry{name: "Images/[alliance]%20%20King%20of%20Baking.jpg"}, itemXML: `<item type="image" isRef="True">[alliance]  King of Baking.jpg</item>`},
		{name: "NFD entry NFC ref", entry: zipEntry{name: "Images/" + nfd}, itemXML: `<item type="image" isRef="True">` + nfc + `</item>`},
		{name: "NFC entry NFD ref", entry: zipEntry{name: "Images/" + nfc}, itemXML: `<item type="image" isRef="True">` + nfd + `</item>`},
		{name: "at-prefixed entry", entry: zipEntry{name: "Audio/@track.mp3"}, itemXML: `<item type="audio" isRef="True">track.mp3</item>`},
		{name: "at-prefixed ref in v5", entry: zipEntry{name: "Audio/track.mp3"}, itemXML: `<item type="audio">@track.mp3</item>`},
		{name: "wrong folder", entry: zipEntry{name: "Images/song.mp3"}, itemXML: `<item type="audio" isRef="True">song.mp3</item>`, warn: CodeExtraFolderEntry},
		{name: "root entry", entry: zipEntry{name: "pic.png"}, itemXML: `<item type="image" isRef="True">pic.png</item>`, warn: CodeExtraFolderEntry},
		{name: "unknown folder", entry: zipEntry{name: "Media/pic.png"}, itemXML: `<item type="image" isRef="True">pic.png</item>`, warn: CodeExtraFolderEntry},
		{name: "case-insensitive", entry: zipEntry{name: "Images/Photo.JPG"}, itemXML: `<item type="image" isRef="True">photo.jpg</item>`},
		{name: "folder case", entry: zipEntry{name: "images/photo.jpg"}, itemXML: `<item type="image" isRef="True">photo.jpg</item>`},
		{name: "cp866 without utf8 flag", entry: zipEntry{name: "Images/" + encodeCP866("Кот.jpg"), nonUTF8: true}, itemXML: `<item type="image" isRef="True">Кот.jpg</item>`},
		{name: "cp866 percent-encoded", entry: zipEntry{name: "Images/" + encodeCP866("Кот%20и%20пёс.jpg"), nonUTF8: true}, itemXML: `<item type="image" isRef="True">Кот и пёс.jpg</item>`},
		{name: "ref with folder prefix", entry: zipEntry{name: "Images/x.png"}, itemXML: `<item type="image" isRef="True">Images/x.png</item>`},
		{name: "html", entry: zipEntry{name: "Html/nature_trivia%20(2).html"}, itemXML: `<item type="html" isRef="True">nature_trivia (2).html</item>`},
		{name: "missing", entry: zipEntry{name: "Images/other.png"}, itemXML: `<item type="image" isRef="True">absent.png</item>`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := `<package name="n" version="5"><rounds><round name="r"><themes><theme name="t"><questions>` +
				`<question price="100"><params><param name="question" type="content">` + tc.itemXML + `</param></params><right><answer>a</answer></right></question>` +
				`</questions></theme></themes></round></rounds></package>`
			entry := tc.entry
			entry.data = []byte("DATA-" + tc.name)
			store := newFakeStore()
			p, rep, err := importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte(content)}, entry}), store, ImportOptions{})
			require.NoError(t, err)
			items := p.Rounds[0].Themes[0].Questions[0].Params.Question
			if tc.wantErr {
				require.Empty(t, items)
				require.Equal(t, 1, rep.Count(CodeMediaMissing))
				require.Equal(t, 1, rep.Stats.MediaMissing)
				return
			}
			require.Len(t, items, 1, "%v", rep.Entries)
			require.Len(t, items[0].MediaID, 64)
			require.Equal(t, 1, rep.Stats.MediaImported)
			require.Empty(t, entriesWithLevel(rep, LevelError))
			warnings := 0
			for _, e := range entriesWithLevel(rep, LevelWarning) {
				if e.Code == CodeExtraFolderEntry {
					warnings++
				}
			}
			if tc.warn != "" {
				require.Equal(t, 1, warnings, "%v", rep.Entries)
			} else {
				require.Equal(t, 0, rep.Count(CodeExtraFolderEntry), "%v", rep.Entries)
			}
			require.Len(t, store.puts, 1)
			require.Equal(t, packs.ContentType(store.puts[0].ExpectedKind), items[0].Type)
		})
	}
}

func TestLeadingFolderAndLogo(t *testing.T) {
	content := `<package name="n" version="5" logo="@logo.png"><rounds><round name="r"><themes><theme name="t"><questions>` +
		`<question price="100"><params><param name="question" type="content"><item type="image" isRef="True">x.png</item></param></params><right><answer>a</answer></right></question>` +
		`</questions></theme></themes></round></rounds></package>`
	store := newFakeStore()
	p, rep, err := importBytes(t, buildZip(t, []zipEntry{
		{name: "pack/Content.XML", data: []byte(content)},
		{name: "pack/Images/x.png", data: []byte("X")},
		{name: "pack/Images/logo.png", data: []byte("LOGO")},
		{name: "pack/junk/readme.txt", data: []byte("hi")},
	}), store, ImportOptions{})
	require.NoError(t, err)
	require.Len(t, p.LogoMediaID, 64)
	require.Len(t, p.Rounds[0].Themes[0].Questions[0].Params.Question[0].MediaID, 64)
	require.Equal(t, 2, rep.Stats.MediaImported)
	require.Equal(t, 1, rep.Count(CodeExtraFolderEntry), "junk entry reported once")

	// Same-content media is stored once and referenced twice.
	content2 := `<package name="n" version="5" logo="https://example.com/logo.png"><rounds><round name="r"><themes><theme name="t"><questions>` +
		`<question price="100"><params><param name="question" type="content"><item type="image" isRef="True">a.png</item><item type="image" isRef="True">b.png</item></param></params><right><answer>a</answer></right></question>` +
		`</questions></theme></themes></round></rounds></package>`
	store = newFakeStore()
	p, rep, err = importBytes(t, buildZip(t, []zipEntry{
		{name: "content.xml", data: []byte(content2)},
		{name: "Images/a.png", data: []byte("SAME")},
		{name: "Images/b.png", data: []byte("SAME")},
	}), store, ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/logo.png", p.LogoURL)
	require.Empty(t, p.LogoMediaID)
	q := p.Rounds[0].Themes[0].Questions[0]
	require.Len(t, q.Params.Question, 2)
	require.Equal(t, q.Params.Question[0].MediaID, q.Params.Question[1].MediaID)
	require.Len(t, p.MediaIDs(), 1)
	require.Equal(t, 2, rep.Stats.MediaImported, "counted per archive entry")
}

func TestStoreRejections(t *testing.T) {
	content := `<package name="n" version="5"><rounds><round name="r"><themes><theme name="t"><questions>` +
		`<question price="100"><params><param name="question" type="content">` +
		`<item type="image" isRef="True">big.png</item><item type="image" isRef="True">weird.xyz</item><item type="image" isRef="True">ok.png</item><item type="image" isRef="True">big.png</item>` +
		`</param></params><right><answer>a</answer></right></question>` +
		`</questions></theme></themes></round></rounds></package>`
	store := newFakeStore()
	store.maxSize = 10
	store.rejectExt = map[string]bool{"xyz": true}
	p, rep, err := importBytes(t, buildZip(t, []zipEntry{
		{name: "content.xml", data: []byte(content)},
		{name: "Images/big.png", data: []byte("0123456789ABCDEF")},
		{name: "Images/weird.xyz", data: []byte("x")},
		{name: "Images/ok.png", data: []byte("ok")},
	}), store, ImportOptions{})
	require.NoError(t, err)
	items := p.Rounds[0].Themes[0].Questions[0].Params.Question
	require.Len(t, items, 1)
	require.Len(t, items[0].MediaID, 64)
	require.Equal(t, 2, rep.Count(CodeMediaTooLarge), "reported for every reference")
	require.Equal(t, 1, rep.Count(CodeMediaUnsupported))
	require.Equal(t, 3, rep.Stats.MediaMissing)
	require.Equal(t, 1, rep.Stats.MediaImported)
	require.Len(t, store.puts, 3, "a rejected file is not retried")
}
