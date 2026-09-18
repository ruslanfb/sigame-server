package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // register decoder for image.DecodeConfig
	_ "image/jpeg" // register decoder for image.DecodeConfig
	_ "image/png"  // register decoder for image.DecodeConfig
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Info is what a Prober learns about a media file.
type Info struct {
	DurationMs int64  // 0 when unknown (images, HTML, no ffprobe)
	Width      int    // first video/image stream, 0 when unknown
	Height     int    // first video/image stream, 0 when unknown
	Codec      string // codec_name of the primary stream, "" when unknown
}

// Prober extracts duration and dimensions from a media file on disk.
type Prober interface {
	Probe(ctx context.Context, path string) (Info, error)
}

// NopProber knows nothing; the store then falls back to stdlib image decoders.
type NopProber struct{}

// Probe implements Prober.
func (NopProber) Probe(context.Context, string) (Info, error) { return Info{}, nil }

// FFProbe runs the ffprobe binary. Zero Timeout means no additional deadline
// beyond the caller's context.
type FFProbe struct {
	Path    string
	Timeout time.Duration
}

// DetectFFProbe looks up ffprobe on PATH.
func DetectFFProbe() (string, bool) {
	p, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", false
	}
	return p, true
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Duration    string `json:"duration"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe implements Prober by running
// ffprobe -v error -print_format json -show_format -show_streams <path>.
func (p FFProbe) Probe(ctx context.Context, path string) (Info, error) {
	bin := p.Path
	if bin == "" {
		bin = "ffprobe"
	}
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return Info{}, fmt.Errorf("media: ffprobe: %w: %s", err, bytes.TrimSpace(exitErr.Stderr))
		}
		return Info{}, fmt.Errorf("media: ffprobe: %w", err)
	}
	return parseFFProbe(out)
}

func parseFFProbe(out []byte) (Info, error) {
	var res ffprobeOutput
	if err := json.Unmarshal(out, &res); err != nil {
		return Info{}, fmt.Errorf("media: ffprobe: parse output: %w", err)
	}
	var info Info
	info.DurationMs = secondsToMs(res.Format.Duration)
	audioCodec := ""
	for _, s := range res.Streams {
		if info.DurationMs == 0 {
			info.DurationMs = secondsToMs(s.Duration)
		}
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic == 1 || info.Width != 0 {
				continue
			}
			info.Width, info.Height, info.Codec = s.Width, s.Height, s.CodecName
		case "audio":
			if audioCodec == "" {
				audioCodec = s.CodecName
			}
		}
	}
	if info.Codec == "" {
		info.Codec = audioCodec
	}
	return info, nil
}

func secondsToMs(s string) int64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0
	}
	return int64(math.Round(f * 1000))
}

// imageSizeFile opens path and decodes its dimensions without ffprobe.
func imageSizeFile(path, mime string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	return imageSize(f, mime)
}

// imageSize decodes only the header of a PNG/JPEG/GIF (stdlib) or WebP
// (manual VP8/VP8L/VP8X parsing) stream and returns width and height.
func imageSize(r io.Reader, mime string) (int, int, error) {
	switch mime {
	case "image/png", "image/jpeg", "image/gif":
		cfg, _, err := image.DecodeConfig(r)
		if err != nil {
			return 0, 0, fmt.Errorf("media: decode %s: %w", mime, err)
		}
		return cfg.Width, cfg.Height, nil
	case "image/webp":
		return webpSize(r)
	}
	return 0, 0, fmt.Errorf("media: no header decoder for %s", mime)
}

// webpSize parses the RIFF/WEBP container: "VP8 " (lossy key frame),
// "VP8L" (lossless) or "VP8X" (extended, canvas size).
func webpSize(r io.Reader) (int, int, error) {
	var hdr [30]byte
	n, err := io.ReadFull(r, hdr[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return 0, 0, fmt.Errorf("media: webp: %w", err)
	}
	b := hdr[:n]
	if len(b) < 16 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, errors.New("media: webp: bad RIFF header")
	}
	payload := b[20:]
	switch string(b[12:16]) {
	case "VP8 ":
		// 3-byte frame tag, 3-byte start code 9d 01 2a, then 14-bit w/h.
		if len(payload) < 10 || payload[3] != 0x9d || payload[4] != 0x01 || payload[5] != 0x2a {
			return 0, 0, errors.New("media: webp: bad VP8 key frame")
		}
		w := int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff)
		h := int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff)
		return w, h, nil
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f {
			return 0, 0, errors.New("media: webp: bad VP8L signature")
		}
		v := binary.LittleEndian.Uint32(payload[1:5])
		return int(v&0x3fff) + 1, int((v>>14)&0x3fff) + 1, nil
	case "VP8X":
		if len(payload) < 10 {
			return 0, 0, errors.New("media: webp: short VP8X chunk")
		}
		w := int(payload[4]) | int(payload[5])<<8 | int(payload[6])<<16
		h := int(payload[7]) | int(payload[8])<<8 | int(payload[9])<<16
		return w + 1, h + 1, nil
	}
	return 0, 0, fmt.Errorf("media: webp: unknown chunk %q", b[12:16])
}
