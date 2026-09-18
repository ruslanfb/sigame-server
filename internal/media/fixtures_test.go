package media

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// mediaDDL mirrors the migration owned by internal/db.
const mediaDDL = `CREATE TABLE media (id TEXT PRIMARY KEY, kind TEXT NOT NULL, mime TEXT NOT NULL, ext TEXT NOT NULL, size INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL DEFAULT 0, width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0,
  original_name TEXT NOT NULL DEFAULT '', ref_count INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);`

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(mediaDDL)
	require.NoError(t, err)
	return db
}

func newTestStore(t *testing.T, limits Limits, prober Prober) (*DiskStore, *sql.DB) {
	t.Helper()
	db := newTestDB(t)
	st, err := NewDiskStore(db, t.TempDir(), limits, prober, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	return st, db
}

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 11), 128, 255})
		}
	}
	return img
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, testImage(w, h)))
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, testImage(w, h), &jpeg.Options{Quality: 80}))
	return buf.Bytes()
}

func gifBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, gif.Encode(&buf, testImage(w, h), nil))
	return buf.Bytes()
}

// wavBytes builds a PCM 16-bit mono WAV of the given duration and sample rate.
func wavBytes(seconds float64, sampleRate int) []byte {
	samples := int(seconds * float64(sampleRate))
	data := make([]byte, samples*2) // silence
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate*2))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

func riff(fourcc, chunk string, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(4+8+len(payload)))
	b.WriteString(fourcc)
	b.WriteString(chunk)
	binary.Write(&b, binary.LittleEndian, uint32(len(payload)))
	b.Write(payload)
	return b.Bytes()
}

func webpVP8(w, h int) []byte {
	p := []byte{0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a}
	p = binary.LittleEndian.AppendUint16(p, uint16(w))
	p = binary.LittleEndian.AppendUint16(p, uint16(h))
	p = append(p, 0, 0, 0, 0)
	return riff("WEBP", "VP8 ", p)
}

func webpVP8L(w, h int) []byte {
	v := uint32(w-1) | uint32(h-1)<<14
	p := []byte{0x2f}
	p = binary.LittleEndian.AppendUint32(p, v)
	p = append(p, 0, 0, 0, 0)
	return riff("WEBP", "VP8L", p)
}

func webpVP8X(w, h int) []byte {
	p := []byte{0x10, 0, 0, 0}
	cw, ch := w-1, h-1
	p = append(p, byte(cw), byte(cw>>8), byte(cw>>16), byte(ch), byte(ch>>8), byte(ch>>16))
	p = append(p, 0, 0, 0, 0)
	return riff("WEBP", "VP8X", p)
}

func mp4Header(major string, compat ...string) []byte {
	size := 16 + 4*len(compat)
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(size))
	b.WriteString("ftyp")
	b.WriteString(major)
	binary.Write(&b, binary.BigEndian, uint32(0x200))
	for _, c := range compat {
		b.WriteString(c)
	}
	b.WriteString("\x00\x00\x00\x08free")
	return b.Bytes()
}

func ebmlHeader(docType string) []byte {
	body := []byte{0x42, 0x86, 0x81, 0x01, 0x42, 0xf7, 0x81, 0x01, 0x42, 0xf2, 0x81, 0x04, 0x42, 0xf3, 0x81, 0x08}
	body = append(body, 0x42, 0x82, byte(0x80|len(docType)))
	body = append(body, docType...)
	body = append(body, 0x42, 0x87, 0x81, 0x02, 0x42, 0x85, 0x81, 0x02)
	out := []byte{0x1a, 0x45, 0xdf, 0xa3, byte(0x80 | len(body))}
	out = append(out, body...)
	return append(out, 0x18, 0x53, 0x80, 0x67, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
}

func oggPage(packet []byte) []byte {
	var b bytes.Buffer
	b.WriteString("OggS")
	b.WriteByte(0)    // version
	b.WriteByte(0x02) // BOS
	b.Write(make([]byte, 8+4+4+4))
	b.WriteByte(1) // one segment
	b.WriteByte(byte(len(packet)))
	b.Write(packet)
	return b.Bytes()
}

func bmpHeader() []byte {
	b := []byte("BM")
	b = binary.LittleEndian.AppendUint32(b, 1078)
	b = append(b, 0, 0, 0, 0)
	b = binary.LittleEndian.AppendUint32(b, 54)
	b = binary.LittleEndian.AppendUint32(b, 40)
	return append(b, make([]byte, 36)...)
}

func mp3Frame() []byte {
	return append([]byte{0xff, 0xfb, 0x90, 0x00}, make([]byte, 100)...)
}
