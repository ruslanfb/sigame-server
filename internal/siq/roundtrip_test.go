package siq

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundTripFixtures(t *testing.T) {
	fixtures := []string{"SIGameTestEn.siq", "SIGameTestNew.siq", "SIGameTest.siq", "1.siq", "test.siq", "pack4-07.siq", "Package5_7.siq", "packf-07.siq"}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			store1 := newFakeStore()
			p1, rep1, err := importBytes(t, readFixture(t, name), store1, ImportOptions{})
			require.NoError(t, err)
			require.Empty(t, entriesWithLevel(rep1, LevelError))

			var out bytes.Buffer
			require.NoError(t, Export(context.Background(), p1, store1, &out))

			store2 := newFakeStore()
			p2, rep2, err := importBytes(t, out.Bytes(), store2, ImportOptions{})
			require.NoError(t, err)
			require.Equal(t, 5, rep2.Version)
			require.Empty(t, entriesWithLevel(rep2, LevelError))
			require.Equal(t, 0, rep2.Count(CodeV4Upgraded))
			require.Equal(t, rep1.Stats.Questions, rep2.Stats.Questions)
			require.Equal(t, rep1.Stats.ByType, rep2.Stats.ByType)
			require.Equal(t, len(p1.MediaIDs()), rep2.Stats.MediaImported, "one entry per distinct media")
			require.Equal(t, rep1.Stats.Bytes, rep2.Stats.Bytes)

			if p1.SIQID != "" {
				require.Equal(t, p1.SIQID, p2.SIQID)
			} else {
				require.Equal(t, p1.ID, p2.SIQID, "a pack without SIQID is exported with its own id")
			}
			require.Equal(t, normalize(t, p1), normalize(t, p2))
			require.ElementsMatch(t, p1.MediaIDs(), p2.MediaIDs())
		})
	}
}

// TestRoundTripV4Synthetic covers the hand-written v4 fixture including a
// custom type with params, fractional durations and a missing media item.
func TestRoundTripV4Synthetic(t *testing.T) {
	store1 := newFakeStore()
	p1, _, err := importBytes(t, v4Archive(t), store1, ImportOptions{})
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, Export(context.Background(), p1, store1, &out))
	store2 := newFakeStore()
	p2, rep2, err := importBytes(t, out.Bytes(), store2, ImportOptions{})
	require.NoError(t, err)
	require.Empty(t, entriesWithLevel(rep2, LevelError))
	n1, n2 := normalize(t, p1), normalize(t, p2)
	// 2.5 s is written as 00:00:03 (whole seconds) by the v5 format.
	v := &n1.Rounds[0].Themes[1].Questions[6].Params.Question[0]
	require.Equal(t, int64(2500), v.DurationMs)
	v.DurationMs = 3000
	require.Equal(t, n1, n2)
}
