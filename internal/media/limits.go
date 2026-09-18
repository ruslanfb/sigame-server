package media

// Limits bounds the accepted upload size per media kind and toggles risky
// formats. A limit <= 0 disables the size check for that kind.
type Limits struct {
	MaxImage int64 // bytes
	MaxAudio int64 // bytes
	MaxVideo int64 // bytes
	MaxHTML  int64 // bytes
	AllowSVG bool  // SVG can carry scripts; disabled by default
}

// DefaultLimits returns the production defaults: 20 MiB images, 100 MiB
// audio, 512 MiB video, 1 MiB HTML, SVG disabled.
func DefaultLimits() Limits {
	return Limits{
		MaxImage: 20 << 20,
		MaxAudio: 100 << 20,
		MaxVideo: 512 << 20,
		MaxHTML:  1 << 20,
		AllowSVG: false,
	}
}

// Max returns the byte limit for kind (0 = unlimited).
func (l Limits) Max(kind Kind) int64 {
	switch kind {
	case KindImage:
		return l.MaxImage
	case KindAudio:
		return l.MaxAudio
	case KindVideo:
		return l.MaxVideo
	case KindHTML:
		return l.MaxHTML
	}
	return 0
}
