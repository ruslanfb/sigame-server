package siq

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

const minimalContent = `<package name="n" version="5"><rounds/></package>`

func TestZipSafety(t *testing.T) {
	t.Run("not a zip", func(t *testing.T) {
		_, _, err := importBytes(t, []byte("this is not a zip archive at all"), newFakeStore(), ImportOptions{})
		require.ErrorIs(t, err, ErrBadZip)
	})
	t.Run("no content.xml", func(t *testing.T) {
		_, _, err := importBytes(t, buildZip(t, []zipEntry{{name: "Images/x.png", data: []byte("x")}}), newFakeStore(), ImportOptions{})
		require.ErrorIs(t, err, ErrNoContent)
	})
	t.Run("nested too deep", func(t *testing.T) {
		_, _, err := importBytes(t, buildZip(t, []zipEntry{{name: "a/b/content.xml", data: []byte(minimalContent)}}), newFakeStore(), ImportOptions{})
		require.ErrorIs(t, err, ErrNoContent)
	})
	t.Run("malformed xml", func(t *testing.T) {
		_, _, err := importBytes(t, buildZip(t, []zipEntry{{name: "content.xml", data: []byte("<package><rounds></package>")}}), newFakeStore(), ImportOptions{})
		require.ErrorIs(t, err, ErrBadXML)
	})
	for _, name := range []string{"../evil.png", "Images/../../evil.png", "/abs.png", "\\abs.png", "C:\\x.png", "Images\\..\\..\\x.png"} {
		t.Run("traversal "+name, func(t *testing.T) {
			data := buildZip(t, []zipEntry{{name: "content.xml", data: []byte(minimalContent)}, {name: name, data: []byte("x")}})
			_, _, err := importBytes(t, data, newFakeStore(), ImportOptions{})
			require.ErrorIs(t, err, ErrUnsafeEntry)
		})
	}
	t.Run("too many entries", func(t *testing.T) {
		entries := []zipEntry{{name: "content.xml", data: []byte(minimalContent)}}
		for i := 0; i < 10; i++ {
			entries = append(entries, zipEntry{name: "Images/" + string(rune('a'+i)) + ".png", data: []byte("x")})
		}
		_, _, err := importBytes(t, buildZip(t, entries), newFakeStore(), ImportOptions{MaxEntries: 5})
		require.ErrorIs(t, err, ErrTooManyEntries)
		_, _, err = importBytes(t, buildZip(t, entries), newFakeStore(), ImportOptions{MaxEntries: 11})
		require.NoError(t, err)
	})
	t.Run("total uncompressed", func(t *testing.T) {
		data := buildZip(t, []zipEntry{{name: "content.xml", data: []byte(minimalContent)}, {name: "Images/x.png", data: bytes.Repeat([]byte("ab"), 1000)}})
		_, _, err := importBytes(t, data, newFakeStore(), ImportOptions{MaxTotalUncompressed: 1000})
		require.ErrorIs(t, err, ErrTooLarge)
		_, _, err = importBytes(t, data, newFakeStore(), ImportOptions{MaxTotalUncompressed: 10000})
		require.NoError(t, err)
	})
	t.Run("compression ratio", func(t *testing.T) {
		bomb := bytes.Repeat([]byte{0}, 4<<20) // 4 MiB of zeros deflates ~1000:1
		data := buildZip(t, []zipEntry{{name: "content.xml", data: []byte(minimalContent)}, {name: "Images/bomb.png", data: bomb}})
		require.Less(t, len(data), 64<<10)
		_, _, err := importBytes(t, data, newFakeStore(), ImportOptions{})
		require.ErrorIs(t, err, ErrSuspiciousRatio)
		_, _, err = importBytes(t, data, newFakeStore(), ImportOptions{MaxRatio: 5000})
		require.NoError(t, err)
	})
	t.Run("content.xml size cap", func(t *testing.T) {
		huge := make([]byte, maxContentXMLSize+1)
		copy(huge, minimalContent)
		data := buildZip(t, []zipEntry{{name: "content.xml", data: huge, stored: true}})
		_, _, err := importBytes(t, data, newFakeStore(), ImportOptions{})
		require.ErrorIs(t, err, ErrTooLarge)
	})
	t.Run("cancelled context", func(t *testing.T) {
		content := `<package name="n" version="5"><rounds><round name="r"><themes><theme name="t"><questions>` +
			`<question price="100"><params><param name="question" type="content"><item type="image" isRef="True">x.png</item></param></params><right><answer>a</answer></right></question>` +
			`</questions></theme></themes></round></rounds></package>`
		data := buildZip(t, []zipEntry{{name: "content.xml", data: []byte(content)}, {name: "Images/x.png", data: []byte("x")}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := Import(ctx, bytes.NewReader(data), int64(len(data)), newFakeStore(), ImportOptions{})
		require.ErrorIs(t, err, context.Canceled)
	})
}
