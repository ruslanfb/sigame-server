package media

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSniff(t *testing.T) {
	tests := []struct {
		name     string
		head     []byte
		fileName string
		kind     Kind
		mime     string
		ext      string
		err      error
	}{
		{"png", pngBytes(t, 3, 2), "", KindImage, "image/png", "png", nil},
		{"jpeg", jpegBytes(t, 3, 2), "", KindImage, "image/jpeg", "jpg", nil},
		{"gif", gifBytes(t, 3, 2), "", KindImage, "image/gif", "gif", nil},
		{"webp", webpVP8(4, 4), "", KindImage, "image/webp", "webp", nil},
		{"bmp", bmpHeader(), "", KindImage, "image/bmp", "bmp", nil},
		{"bmp-like text", []byte("BMW is a car maker"), "", "", "", "", ErrUnsupported},
		{"svg plain", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "", KindImage, "image/svg+xml", "svg", nil},
		{"svg xml prolog", []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!DOCTYPE svg>\n<svg/>"), "", KindImage, "image/svg+xml", "svg", nil},
		{"mp3 id3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00\xff\xfb"), "", KindAudio, "audio/mpeg", "mp3", nil},
		{"mp3 frame fffb", mp3Frame(), "", KindAudio, "audio/mpeg", "mp3", nil},
		{"mp3 frame fff3", []byte{0xff, 0xf3, 0x90, 0x00, 0, 0}, "", KindAudio, "audio/mpeg", "mp3", nil},
		{"mp3 frame fff2", []byte{0xff, 0xf2, 0x90, 0x00, 0, 0}, "", KindAudio, "audio/mpeg", "mp3", nil},
		{"mp3 bad bitrate", []byte{0xff, 0xfb, 0xf0, 0x00, 0, 0}, "", "", "", "", ErrUnsupported},
		{"ogg vorbis", oggPage([]byte("\x01vorbis\x00")), "", KindAudio, "audio/ogg", "ogg", nil},
		{"ogg opus", oggPage([]byte("OpusHead")), "song.ogg", KindAudio, "audio/ogg", "ogg", nil},
		{"ogg theora", oggPage([]byte("\x80theora")), "", KindVideo, "video/ogg", "ogg", nil},
		{"ogg by ogv name", oggPage([]byte("\x01vorbis\x00")), "clip.ogv", KindVideo, "video/ogg", "ogg", nil},
		{"wav", wavBytes(0.01, 8000), "", KindAudio, "audio/wav", "wav", nil},
		{"flac", []byte("fLaC\x00\x00\x00\x22"), "", KindAudio, "audio/flac", "flac", nil},
		{"m4a major brand", mp4Header("M4A ", "M4A ", "isom", "iso2"), "", KindAudio, "audio/mp4", "m4a", nil},
		{"m4a compat brand", mp4Header("isom", "isom", "M4A "), "", KindAudio, "audio/mp4", "m4a", nil},
		{"mp4 isom", mp4Header("isom", "isom", "iso2", "avc1", "mp41"), "", KindVideo, "video/mp4", "mp4", nil},
		{"mp4 isom named m4a", mp4Header("isom", "isom", "iso2", "mp41"), "track.M4A", KindAudio, "audio/mp4", "m4a", nil},
		{"mp4 mp42", mp4Header("mp42", "mp42", "isom"), "", KindVideo, "video/mp4", "mp4", nil},
		{"m4v", mp4Header("M4V ", "M4V ", "mp42"), "x.m4a", KindVideo, "video/mp4", "mp4", nil},
		{"mov qt", mp4Header("qt  ", "qt  "), "clip.mov", KindVideo, "video/mp4", "mp4", nil},
		{"webm", ebmlHeader("webm"), "", KindVideo, "video/webm", "webm", nil},
		{"mkv", ebmlHeader("matroska"), "", KindVideo, "video/x-matroska", "mkv", nil},
		{"ebml truncated", []byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42}, "", KindVideo, "video/webm", "webm", nil},
		{"html doctype", []byte("<!DOCTYPE html><html><body>hi</body></html>"), "", KindHTML, "text/html; charset=utf-8", "html", nil},
		{"html fragment", []byte("  \n<div class=\"q\">question</div>"), "", KindHTML, "text/html; charset=utf-8", "html", nil},
		{"html custom tag by name", []byte("<question>text</question>"), "q.html", KindHTML, "text/html; charset=utf-8", "html", nil},
		{"html custom tag by htm name", []byte("<question>text</question>"), "dir/q.HTM", KindHTML, "text/html; charset=utf-8", "html", nil},
		{"xml by html name", []byte("<?xml version=\"1.0\"?><root/>"), "q.html", KindHTML, "text/html; charset=utf-8", "html", nil},
		{"custom tag no name", []byte("<question>text</question>"), "q.txt", "", "", "", ErrUnsupported},
		{"text plain", []byte("just some text"), "notes.txt", "", "", "", ErrUnsupported},
		{"text plain named html", []byte("just some text"), "notes.html", "", "", "", ErrUnsupported},
		{"xml", []byte("<?xml version=\"1.0\"?><root/>"), "", "", "", "", ErrUnsupported},
		{"zip", []byte("PK\x03\x04\x14\x00"), "pack.siq", "", "", "", ErrUnsupported},
		{"pdf", []byte("%PDF-1.7\n"), "", "", "", "", ErrUnsupported},
		{"avi", riff("AVI ", "LIST", make([]byte, 8)), "", "", "", "", ErrUnsupported},
		{"binary", []byte{0x00, 0x01, 0x02, 0x03}, "", "", "", "", ErrUnsupported},
		{"empty", nil, "x.png", "", "", "", ErrUnsupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, mime, ext, err := Sniff(tc.head, tc.fileName)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.kind, kind)
			require.Equal(t, tc.mime, mime)
			require.Equal(t, tc.ext, ext)
		})
	}
}

func TestSniffTruncatesHead(t *testing.T) {
	head := append(pngBytes(t, 2, 2), make([]byte, 2*SniffLen)...)
	kind, _, _, err := Sniff(head, "")
	require.NoError(t, err)
	require.Equal(t, KindImage, kind)
}

func TestEBMLVint(t *testing.T) {
	v, n := ebmlVint([]byte{0x81}, false)
	require.Equal(t, uint64(1), v)
	require.Equal(t, 1, n)

	v, n = ebmlVint([]byte{0x42, 0x82}, true)
	require.Equal(t, uint64(0x4282), v)
	require.Equal(t, 2, n)

	v, n = ebmlVint([]byte{0x40, 0x02}, false)
	require.Equal(t, uint64(2), v)
	require.Equal(t, 2, n)

	_, n = ebmlVint([]byte{0x00}, false)
	require.Equal(t, 0, n)
	_, n = ebmlVint([]byte{0x40}, false) // needs two bytes
	require.Equal(t, 0, n)
}
