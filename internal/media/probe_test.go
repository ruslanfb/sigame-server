package media

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeProber records calls and returns a fixed Info or error.
type fakeProber struct {
	mu    sync.Mutex
	paths []string
	info  Info
	err   error
}

func (f *fakeProber) Probe(_ context.Context, path string) (Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path)
	return f.info, f.err
}

func (f *fakeProber) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func TestNopProber(t *testing.T) {
	info, err := NopProber{}.Probe(context.Background(), "/nonexistent")
	require.NoError(t, err)
	require.Equal(t, Info{}, info)
}

func TestImageSize(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		mime string
		w, h int
	}{
		{"png", pngBytes(t, 17, 9), "image/png", 17, 9},
		{"jpeg", jpegBytes(t, 33, 21), "image/jpeg", 33, 21},
		{"gif", gifBytes(t, 5, 7), "image/gif", 5, 7},
		{"webp vp8", webpVP8(640, 480), "image/webp", 640, 480},
		{"webp vp8l", webpVP8L(1, 1), "image/webp", 1, 1},
		{"webp vp8l big", webpVP8L(16383, 300), "image/webp", 16383, 300},
		{"webp vp8x", webpVP8X(1920, 1080), "image/webp", 1920, 1080},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h, err := imageSize(bytes.NewReader(tc.data), tc.mime)
			require.NoError(t, err)
			require.Equal(t, tc.w, w)
			require.Equal(t, tc.h, h)
		})
	}

	t.Run("errors", func(t *testing.T) {
		_, _, err := imageSize(bytes.NewReader([]byte("nope")), "image/png")
		require.Error(t, err)
		_, _, err = imageSize(bytes.NewReader(bmpHeader()), "image/bmp")
		require.Error(t, err)
		_, _, err = imageSize(bytes.NewReader(riff("WEBP", "VP8 ", []byte{1, 2, 3})), "image/webp")
		require.Error(t, err)
		_, _, err = imageSize(bytes.NewReader(riff("WEBP", "ALPH", make([]byte, 12))), "image/webp")
		require.Error(t, err)
		_, _, err = imageSize(bytes.NewReader([]byte("RIFF")), "image/webp")
		require.Error(t, err)
	})
}

func TestParseFFProbe(t *testing.T) {
	out := []byte(`{
		"streams": [
			{"codec_type": "video", "codec_name": "mjpeg", "width": 300, "height": 300, "disposition": {"attached_pic": 1}},
			{"codec_type": "audio", "codec_name": "mp3", "duration": "12.5"},
			{"codec_type": "video", "codec_name": "h264", "width": 1280, "height": 720}
		],
		"format": {"duration": "12.345678"}
	}`)
	info, err := parseFFProbe(out)
	require.NoError(t, err)
	require.Equal(t, Info{DurationMs: 12346, Width: 1280, Height: 720, Codec: "h264"}, info)

	audioOnly := []byte(`{"streams":[{"codec_type":"audio","codec_name":"aac","duration":"3.25"}],"format":{}}`)
	info, err = parseFFProbe(audioOnly)
	require.NoError(t, err)
	require.Equal(t, Info{DurationMs: 3250, Codec: "aac"}, info)

	_, err = parseFFProbe([]byte("not json"))
	require.Error(t, err)

	require.Equal(t, int64(0), secondsToMs(""))
	require.Equal(t, int64(0), secondsToMs("N/A"))
	require.Equal(t, int64(0), secondsToMs("-1"))
	require.Equal(t, int64(1000), secondsToMs("1.0004"))
}

func TestFFProbeMissingBinary(t *testing.T) {
	p := FFProbe{Path: filepath.Join(t.TempDir(), "no-such-ffprobe"), Timeout: time.Second}
	_, err := p.Probe(context.Background(), "whatever")
	require.Error(t, err)
}

// TestFFProbeIntegration runs the real ffprobe when it is installed.
func TestFFProbeIntegration(t *testing.T) {
	path, ok := DetectFFProbe()
	if !ok {
		t.Skip("ffprobe not installed")
	}
	wav := filepath.Join(t.TempDir(), "silence.wav")
	require.NoError(t, os.WriteFile(wav, wavBytes(1, 44100), 0o644))

	p := FFProbe{Path: path, Timeout: 10 * time.Second}
	info, err := p.Probe(context.Background(), wav)
	require.NoError(t, err)
	require.Equal(t, int64(1000), info.DurationMs)
	require.Equal(t, "pcm_s16le", info.Codec)
	require.Equal(t, 0, info.Width)

	pngPath := filepath.Join(t.TempDir(), "img.png")
	require.NoError(t, os.WriteFile(pngPath, pngBytes(t, 12, 34), 0o644))
	info, err = p.Probe(context.Background(), pngPath)
	require.NoError(t, err)
	require.Equal(t, 12, info.Width)
	require.Equal(t, 34, info.Height)

	_, err = p.Probe(context.Background(), filepath.Join(t.TempDir(), "missing.mp3"))
	require.Error(t, err)

	// A store wired with the real prober fills DurationMs for audio.
	st, _ := newTestStore(t, DefaultLimits(), p)
	m, err := st.Put(context.Background(), bytes.NewReader(wavBytes(1, 44100)), PutOptions{OriginalName: "silence.wav"})
	require.NoError(t, err)
	require.Equal(t, KindAudio, m.Kind)
	require.Equal(t, int64(1000), m.DurationMs)
}
