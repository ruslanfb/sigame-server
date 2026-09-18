package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/bits"
	"net/http"
	"path"
	"strings"
)

// SniffLen is the number of leading bytes Sniff needs to classify content.
const SniffLen = 4096

// Sniff classifies content by its leading bytes (at most SniffLen are
// inspected). originalName is only a hint for genuinely ambiguous containers
// (MP4 audio vs video, Ogg audio vs video, HTML that DetectContentType sees as
// plain text). It returns the coarse Kind, the canonical MIME type and the
// canonical extension (without dot), or ErrUnsupported.
//
// SVG is reported as image/svg+xml with ext "svg"; callers decide whether to
// accept it (see Limits.AllowSVG).
func Sniff(head []byte, originalName string) (kind Kind, mime, ext string, err error) {
	if len(head) > SniffLen {
		head = head[:SniffLen]
	}
	if len(head) == 0 {
		return "", "", "", fmt.Errorf("%w: empty content", ErrUnsupported)
	}
	nameExt := strings.ToLower(strings.TrimPrefix(path.Ext(strings.ReplaceAll(originalName, "\\", "/")), "."))

	if k, m, e, ok := sniffBinary(head, nameExt); ok {
		return k, m, e, nil
	}
	if k, m, e, ok := sniffText(head, nameExt); ok {
		return k, m, e, nil
	}
	detected := http.DetectContentType(head)
	return "", "", "", fmt.Errorf("%w: %s", ErrUnsupported, detected)
}

// sniffBinary checks explicit magic numbers for the supported binary formats.
func sniffBinary(b []byte, nameExt string) (Kind, string, string, bool) {
	has := func(prefix string) bool { return bytes.HasPrefix(b, []byte(prefix)) }
	at := func(off int, s string) bool {
		return len(b) >= off+len(s) && string(b[off:off+len(s)]) == s
	}

	switch {
	case has("\x89PNG\r\n\x1a\n"):
		return KindImage, "image/png", "png", true
	case has("\xff\xd8\xff"):
		return KindImage, "image/jpeg", "jpg", true
	case has("GIF87a"), has("GIF89a"):
		return KindImage, "image/gif", "gif", true
	case has("RIFF") && at(8, "WEBP"):
		return KindImage, "image/webp", "webp", true
	case has("RIFF") && at(8, "WAVE"):
		return KindAudio, "audio/wav", "wav", true
	case has("BM") && isBMP(b):
		return KindImage, "image/bmp", "bmp", true
	case has("fLaC"):
		return KindAudio, "audio/flac", "flac", true
	case has("ID3"), isMP3Frame(b):
		return KindAudio, "audio/mpeg", "mp3", true
	case has("OggS"):
		if bytes.Contains(b, []byte("theora")) || nameExt == "ogv" {
			return KindVideo, "video/ogg", "ogg", true
		}
		return KindAudio, "audio/ogg", "ogg", true
	case at(4, "ftyp"):
		if isMP4Audio(b, nameExt) {
			return KindAudio, "audio/mp4", "m4a", true
		}
		return KindVideo, "video/mp4", "mp4", true
	case has("\x1a\x45\xdf\xa3"):
		if strings.EqualFold(ebmlDocType(b), "matroska") {
			return KindVideo, "video/x-matroska", "mkv", true
		}
		return KindVideo, "video/webm", "webm", true
	}
	return "", "", "", false
}

// sniffText recognises SVG and HTML; everything else textual is unsupported.
func sniffText(b []byte, nameExt string) (Kind, string, string, bool) {
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), " \t\r\n\f")
	if len(trimmed) == 0 || trimmed[0] != '<' {
		return "", "", "", false
	}
	lower := bytes.ToLower(trimmed)
	if isSVG(lower) {
		return KindImage, "image/svg+xml", "svg", true
	}
	ct := http.DetectContentType(b)
	if strings.HasPrefix(ct, "text/html") {
		return KindHTML, "text/html; charset=utf-8", "html", true
	}
	if (nameExt == "html" || nameExt == "htm") &&
		(strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "text/xml")) {
		return KindHTML, "text/html; charset=utf-8", "html", true
	}
	return "", "", "", false
}

func isSVG(lower []byte) bool {
	if bytes.HasPrefix(lower, []byte("<svg")) {
		return true
	}
	if bytes.HasPrefix(lower, []byte("<?xml")) || bytes.HasPrefix(lower, []byte("<!doctype svg")) {
		return bytes.Contains(lower, []byte("<svg"))
	}
	return false
}

// isBMP validates the DIB header size field to avoid classifying arbitrary
// "BM..." text as a bitmap.
func isBMP(b []byte) bool {
	if len(b) < 18 {
		return false
	}
	switch binary.LittleEndian.Uint32(b[14:18]) {
	case 12, 40, 52, 56, 64, 108, 124:
		return true
	}
	return false
}

// isMP3Frame matches an MPEG audio Layer III frame header (any MPEG version)
// with a valid bitrate and sample-rate index. Covers 0xFFFB/0xFFF3/0xFFF2.
func isMP3Frame(b []byte) bool {
	if len(b) < 3 || b[0] != 0xff {
		return false
	}
	if b[1]&0xe6 != 0xe2 { // sync 111, version xx, layer 01 (III)
		return false
	}
	bitrate := b[2] >> 4
	sample := (b[2] >> 2) & 3
	return bitrate != 0 && bitrate != 0xf && sample != 3
}

var mp4AudioBrands = map[string]bool{"M4A ": true, "M4B ": true, "M4P ": true}

// isMP4Audio decides audio/mp4 vs video/mp4 from the ftyp brands; the file
// extension breaks the tie for generic brands (isom, mp42, ...).
func isMP4Audio(b []byte, nameExt string) bool {
	if len(b) < 12 {
		return false
	}
	major := string(b[8:12])
	if mp4AudioBrands[major] {
		return true
	}
	if strings.HasPrefix(major, "qt") || major == "M4V " {
		return false
	}
	end := int(binary.BigEndian.Uint32(b[0:4]))
	if end < 16 || end > len(b) {
		end = len(b)
	}
	for i := 16; i+4 <= end; i += 4 {
		if mp4AudioBrands[string(b[i:i+4])] {
			return true
		}
	}
	return nameExt == "m4a"
}

// ebmlDocType parses the EBML header and returns its DocType ("webm",
// "matroska") or "" when it cannot be determined.
func ebmlDocType(b []byte) string {
	pos := 4 // after 1A 45 DF A3
	size, n := ebmlVint(b[pos:], false)
	if n == 0 {
		return ""
	}
	pos += n
	end := pos + int(size)
	if size > uint64(len(b)) || end > len(b) {
		end = len(b)
	}
	for pos < end {
		id, n := ebmlVint(b[pos:], true)
		if n == 0 {
			return ""
		}
		pos += n
		sz, n := ebmlVint(b[pos:], false)
		if n == 0 || sz > uint64(len(b)) || pos+n+int(sz) > len(b) {
			return ""
		}
		pos += n
		if id == 0x4282 {
			return string(b[pos : pos+int(sz)])
		}
		pos += int(sz)
	}
	return ""
}

// ebmlVint reads an EBML variable-size integer. Element IDs keep their length
// marker bit (keepMarker=true); sizes have it stripped.
func ebmlVint(b []byte, keepMarker bool) (val uint64, n int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	n = bits.LeadingZeros8(b[0]) + 1
	if n > len(b) {
		return 0, 0
	}
	val = uint64(b[0])
	if !keepMarker {
		val &= (1 << (8 - uint(n))) - 1
	}
	for i := 1; i < n; i++ {
		val = val<<8 | uint64(b[i])
	}
	return val, n
}
