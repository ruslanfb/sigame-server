package ws

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	b, err := Encode("HELLO", 7, payload{Name: "x", N: 2})
	require.NoError(t, err)
	require.JSONEq(t, `{"t":"HELLO","seq":7,"p":{"name":"x","n":2}}`, string(b))

	e, err := Decode(b, 1024)
	require.NoError(t, err)
	require.Equal(t, "HELLO", e.T)
	require.EqualValues(t, 7, e.Seq)
	p, err := DecodePayload[payload](e)
	require.NoError(t, err)
	require.Equal(t, payload{Name: "x", N: 2}, p)
}

func TestEnvelopeNoPayload(t *testing.T) {
	b, err := Encode("PASS", 0, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"t":"PASS"}`, string(b))
	e, err := Decode(b, 1024)
	require.NoError(t, err)
	p, err := DecodePayload[struct{ X int }](e)
	require.NoError(t, err)
	require.Zero(t, p.X)
}

func TestDecodeErrors(t *testing.T) {
	_, err := Decode([]byte(`{"t":"hello"}`), 1024)
	require.True(t, errors.Is(err, ErrEnvelopeInvalid))
	_, err = Decode([]byte(`{"t":"HELLO","seq":-1}`), 1024)
	require.True(t, errors.Is(err, ErrEnvelopeInvalid))
	_, err = Decode([]byte(`not json`), 1024)
	require.True(t, errors.Is(err, ErrEnvelopeInvalid))
	_, err = Decode([]byte(`{"t":"HELLO"}`), 5)
	require.True(t, errors.Is(err, ErrEnvelopeTooLarge))
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(10, 3, 0)
	require.True(t, l.Allow(0))
	require.True(t, l.Allow(0))
	require.True(t, l.Allow(0))
	require.False(t, l.Allow(0), "burst exhausted")
	require.False(t, l.Allow(50), "half a token after 50 ms at 10/s")
	require.True(t, l.Allow(100), "one token refilled after 100 ms")
	require.False(t, l.Allow(100))
	require.True(t, l.Allow(1000))
	require.True(t, l.Allow(1000))
	require.True(t, l.Allow(1000), "capped at burst")
	require.False(t, l.Allow(1000))
}
