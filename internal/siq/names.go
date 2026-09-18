package siq

import (
	"archive/zip"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Media category folders (SIPackages CollectionNames).
const (
	folderImages = "Images"
	folderAudio  = "Audio"
	folderVideo  = "Video"
	folderHTML   = "Html"
)

// folderForKind maps a content type to its archive folder.
func folderForKind(kind string) string {
	switch kind {
	case "image":
		return folderImages
	case "audio":
		return folderAudio
	case "video":
		return folderVideo
	case "html":
		return folderHTML
	}
	return ""
}

// canonicalFolder returns the canonical folder name for a directory name of
// any case, or "" if it is not a media folder.
func canonicalFolder(dir string) string {
	switch strings.ToLower(dir) {
	case "images":
		return folderImages
	case "audio":
		return folderAudio
	case "video":
		return folderVideo
	case "html":
		return folderHTML
	}
	return ""
}

// unescapeDataString implements System.Uri.UnescapeDataString semantics: every
// valid %XX sequence is decoded to a byte, everything else (including '+') is
// kept verbatim. Invalid sequences are left as they are.
func unescapeDataString(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// cp866Table maps bytes 0x80..0xFF of code page 866 (DOS Cyrillic) to runes.
var cp866Table = [128]rune{
	// 0x80-0x8F: А..П
	0x0410, 0x0411, 0x0412, 0x0413, 0x0414, 0x0415, 0x0416, 0x0417, 0x0418, 0x0419, 0x041A, 0x041B, 0x041C, 0x041D, 0x041E, 0x041F,
	// 0x90-0x9F: Р..Я
	0x0420, 0x0421, 0x0422, 0x0423, 0x0424, 0x0425, 0x0426, 0x0427, 0x0428, 0x0429, 0x042A, 0x042B, 0x042C, 0x042D, 0x042E, 0x042F,
	// 0xA0-0xAF: а..п
	0x0430, 0x0431, 0x0432, 0x0433, 0x0434, 0x0435, 0x0436, 0x0437, 0x0438, 0x0439, 0x043A, 0x043B, 0x043C, 0x043D, 0x043E, 0x043F,
	// 0xB0-0xBF: box drawing
	0x2591, 0x2592, 0x2593, 0x2502, 0x2524, 0x2561, 0x2562, 0x2556, 0x2555, 0x2563, 0x2551, 0x2557, 0x255D, 0x255C, 0x255B, 0x2510,
	// 0xC0-0xCF
	0x2514, 0x2534, 0x252C, 0x251C, 0x2500, 0x253C, 0x255E, 0x255F, 0x255A, 0x2554, 0x2569, 0x2566, 0x2560, 0x2550, 0x256C, 0x2567,
	// 0xD0-0xDF
	0x2568, 0x2564, 0x2565, 0x2559, 0x2558, 0x2552, 0x2553, 0x256B, 0x256A, 0x2518, 0x250C, 0x2588, 0x2584, 0x258C, 0x2590, 0x2580,
	// 0xE0-0xEF: р..я
	0x0440, 0x0441, 0x0442, 0x0443, 0x0444, 0x0445, 0x0446, 0x0447, 0x0448, 0x0449, 0x044A, 0x044B, 0x044C, 0x044D, 0x044E, 0x044F,
	// 0xF0-0xFF: Ё ё Є є Ї ї Ў ў ° ∙ · √ № ¤ ■ NBSP
	0x0401, 0x0451, 0x0404, 0x0454, 0x0407, 0x0457, 0x040E, 0x045E, 0x00B0, 0x2219, 0x00B7, 0x221A, 0x2116, 0x00A4, 0x25A0, 0x00A0,
}

// decodeCP866 interprets s as raw cp866 bytes and returns the UTF-8 string.
func decodeCP866(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(cp866Table[c-0x80])
		}
	}
	return b.String()
}

// encodeCP866 is the inverse of decodeCP866 (used by tests to build legacy
// archives). Runes without a cp866 mapping become '?'.
func encodeCP866(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 {
			b.WriteByte(byte(r))
			continue
		}
		found := false
		for i, t := range cp866Table {
			if t == r {
				b.WriteByte(byte(0x80 + i))
				found = true
				break
			}
		}
		if !found {
			b.WriteByte('?')
		}
	}
	return b.String()
}

func hasHighBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// exactVariants returns the spellings under which a name is indexed for an
// exact (case-sensitive) match: raw, percent-decoded, each in NFC and NFD,
// each with and without a leading '@'.
func exactVariants(name string) []string {
	base := []string{name}
	if dec := unescapeDataString(name); dec != name {
		base = append(base, dec)
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range base {
		for _, t := range []string{s, strings.TrimPrefix(s, "@")} {
			add(t)
			add(norm.NFC.String(t))
			add(norm.NFD.String(t))
		}
	}
	return out
}

// looseKey folds a name for a case-insensitive, normalisation-insensitive match.
func looseKey(name string) string {
	return strings.ToLower(norm.NFC.String(name))
}

// mediaIndex resolves file names referenced from content.xml to zip entries.
// Entries are indexed per folder (Images/Audio/Video/Html, "" for the archive
// root, "*" for any other folder) under every spelling variant so that packs
// with percent-encoded names, NFD names (macOS), cp866 names (old Windows
// zippers), a stray leading '@', wrong folders or wrong case still load.
type mediaIndex struct {
	exact map[string]map[string]*zip.File
	loose map[string]map[string]*zip.File
}

func newMediaIndex() *mediaIndex {
	return &mediaIndex{exact: map[string]map[string]*zip.File{}, loose: map[string]map[string]*zip.File{}}
}

// add indexes one entry. folder is the canonical folder ("" root, "*" other),
// base is the entry base name as stored in the zip.
func (ix *mediaIndex) add(folder, base string, nonUTF8 bool, f *zip.File) {
	names := []string{base}
	if nonUTF8 && hasHighBytes(base) {
		names = append(names, decodeCP866(base))
	}
	if ix.exact[folder] == nil {
		ix.exact[folder] = map[string]*zip.File{}
		ix.loose[folder] = map[string]*zip.File{}
	}
	for _, n := range names {
		for _, v := range exactVariants(n) {
			if _, ok := ix.exact[folder][v]; !ok {
				ix.exact[folder][v] = f
			}
			k := looseKey(v)
			if _, ok := ix.loose[folder][k]; !ok {
				ix.loose[folder][k] = f
			}
		}
	}
}

// resolve finds ref for the given canonical folder. It returns the entry and
// the folder it was actually found in ("" root, "*" other) so that the caller
// can report misplaced files.
func (ix *mediaIndex) resolve(folder, ref string) (*zip.File, string) {
	ref = strings.TrimSpace(ref)
	// Tolerate a category prefix inside the reference ("Images/x.png").
	if i := strings.IndexAny(ref, "/\\"); i > 0 && i < len(ref)-1 {
		if canonicalFolder(ref[:i]) != "" {
			ref = ref[i+1:]
		}
	}
	probes := exactVariants(ref)
	order := []string{folder}
	for _, f := range []string{folderImages, folderAudio, folderVideo, folderHTML, "", "*"} {
		if f != folder {
			order = append(order, f)
		}
	}
	for _, fo := range order {
		m := ix.exact[fo]
		if m == nil {
			continue
		}
		for _, p := range probes {
			if f, ok := m[p]; ok {
				return f, fo
			}
		}
	}
	for _, fo := range order {
		m := ix.loose[fo]
		if m == nil {
			continue
		}
		for _, p := range probes {
			if f, ok := m[looseKey(p)]; ok {
				return f, fo
			}
		}
	}
	return nil, ""
}

// sanitizeEntryName makes a file name safe for a zip entry written by Export:
// base name only, no '/', '\\', '%', '#', '?' or control characters, no
// leading '@', NFC-normalised and trimmed.
func sanitizeEntryName(name string) string {
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	name = norm.NFC.String(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\' || r == '%' || r == '#' || r == '?':
			continue
		case r < 0x20 || r == 0x7F:
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	name = strings.TrimLeft(name, "@")
	name = strings.TrimSpace(name)
	if len(name) > 200 {
		// keep the extension when cutting an absurdly long name
		ext := ""
		if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= 10 {
			ext = name[i:]
		}
		cut := name[:200-len(ext)]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		name = cut + ext
	}
	return name
}
