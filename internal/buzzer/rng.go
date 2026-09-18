package buzzer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
)

// hmacBytes derives a labelled 32-byte value from the arm seed. All randomness
// in the package (arm jitter, arm id, tie lotteries) comes from here, so a
// decision can be replayed from the seed recorded in Result.Seed.
func hmacBytes(seed []byte, label string) []byte {
	h := hmac.New(sha256.New, seed)
	h.Write([]byte(label))
	return h.Sum(nil)
}

// unitFloat returns a deterministic value in [0, 1) for the label.
func unitFloat(seed []byte, label string) float64 {
	b := hmacBytes(seed, label)
	v := binary.BigEndian.Uint64(b[:8]) >> 11 // 53 random bits
	return float64(v) / (1 << 53)
}

// armID derives the 128-bit arm nonce from the seed.
func armID(seed []byte) string {
	return hex.EncodeToString(hmacBytes(seed, "armId")[:16])
}

// phi is the standard normal CDF.
func phi(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// weightedPick returns the index chosen by u ∈ [0,1) over the weights.
func weightedPick(weights []float64, u float64) int {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return int(u * float64(len(weights)))
	}
	acc := 0.0
	for i, w := range weights {
		acc += w / total
		if u < acc {
			return i
		}
	}
	return len(weights) - 1
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
