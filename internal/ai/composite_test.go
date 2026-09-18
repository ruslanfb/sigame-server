package ai

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func countingAI(calls *atomic.Int32, v Verdict, err error) Judge {
	return JudgeFunc(func(context.Context, Request) (Verdict, error) {
		calls.Add(1)
		return v, err
	})
}

func TestCompositeDeterministicTypesSkipAI(t *testing.T) {
	var calls atomic.Int32
	c := &Composite{AI: countingAI(&calls, Verdict{Right: true}, nil), Cache: NewCache(0)}
	ctx := context.Background()

	v, err := c.Judge(ctx, Request{QuestionID: "q1", AnswerType: AnswerNumber, Right: []string{"100"}, Deviation: 5, PlayerAnswer: "103"})
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, SourceNumber, v.Source)

	v, err = c.Judge(ctx, Request{QuestionID: "q1", AnswerType: AnswerSelect, Right: []string{"C"}, PlayerAnswer: "c"})
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, SourceSelect, v.Source)

	v, err = c.Judge(ctx, Request{QuestionID: "q1", AnswerType: AnswerPoint, Right: []string{"0.1,0.1"}, PlayerAnswer: "0.9,0.9"})
	require.NoError(t, err)
	require.False(t, v.Right)

	v, err = c.Judge(ctx, Request{QuestionID: "q1", AnswerType: AnswerClient, Right: []string{"x"}, PlayerAnswer: "+"})
	require.NoError(t, err)
	require.True(t, v.Right)

	// Exact text matches (right and known wrong) are decided without the AI too.
	v, err = c.Judge(ctx, Request{QuestionID: "q1", Right: []string{"Пушкин"}, PlayerAnswer: "Пушкин!"})
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, SourceExact, v.Source)

	v, err = c.Judge(ctx, Request{QuestionID: "q1", Right: []string{"Пушкин"}, Wrong: []string{"Лермонтов"}, PlayerAnswer: "лермонтов"})
	require.NoError(t, err)
	require.False(t, v.Right)
	require.Equal(t, SourceExact, v.Source)

	require.Equal(t, int32(0), calls.Load())
	require.Equal(t, 0, c.Cache.Len())
}

func TestCompositeAIVerdictAndCache(t *testing.T) {
	var calls atomic.Int32
	c := &Composite{AI: countingAI(&calls, Verdict{Right: true, Reason: "Фамилия поэта"}, nil), Cache: NewCache(0)}
	ctx := context.Background()
	req := Request{QuestionID: "q1", Right: []string{"Александр Пушкин"}, PlayerAnswer: "Пушкин"}

	v, err := c.Judge(ctx, req)
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, 1.0, v.Factor)
	require.Equal(t, SourceAI, v.Source)
	require.Equal(t, "Фамилия поэта", v.Reason)
	require.Equal(t, int32(1), calls.Load())

	// Same question, same normalized answer → cache hit.
	req.PlayerAnswer = "  ПУШКИН! "
	v2, err := c.Judge(ctx, req)
	require.NoError(t, err)
	require.Equal(t, v, v2)
	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, 1, c.Cache.Len())

	// Another question → AI again.
	req.QuestionID = "q2"
	_, err = c.Judge(ctx, req)
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())

	// No question ID → no caching.
	req.QuestionID = ""
	_, _ = c.Judge(ctx, req)
	_, _ = c.Judge(ctx, req)
	require.Equal(t, int32(4), calls.Load())
	require.Equal(t, 2, c.Cache.Len())
}

func TestCompositeWithoutCache(t *testing.T) {
	var calls atomic.Int32
	c := &Composite{AI: countingAI(&calls, Verdict{Right: true}, nil)}
	req := Request{QuestionID: "q1", Right: []string{"Александр Пушкин"}, PlayerAnswer: "Пушкин"}
	_, _ = c.Judge(context.Background(), req)
	_, _ = c.Judge(context.Background(), req)
	require.Equal(t, int32(2), calls.Load())
}

func TestCompositeFallbackOnAIError(t *testing.T) {
	var calls atomic.Int32
	c := &Composite{AI: countingAI(&calls, Verdict{}, errors.New("boom")), Cache: NewCache(0)}
	v, err := c.Judge(context.Background(), Request{QuestionID: "q1", Right: []string{"Лермонтов"}, PlayerAnswer: "Лермантов"})
	require.NoError(t, err)
	require.True(t, v.Right, "fuzzy accepts the typo")
	require.Equal(t, SourceFallback, v.Source)
	require.Contains(t, v.Reason, "AI unavailable (boom)")
	require.Equal(t, int32(1), calls.Load())

	// The fallback verdict is cached so identical answers stay consistent.
	v2, err := c.Judge(context.Background(), Request{QuestionID: "q1", Right: []string{"Лермонтов"}, PlayerAnswer: "лермантов"})
	require.NoError(t, err)
	require.Equal(t, v, v2)
	require.Equal(t, int32(1), calls.Load())
}

func TestCompositeFallbackOnAITimeout(t *testing.T) {
	slow := JudgeFunc(func(ctx context.Context, _ Request) (Verdict, error) {
		<-ctx.Done()
		return Verdict{}, ctx.Err()
	})
	c := &Composite{AI: slow, AITimeout: 20 * time.Millisecond}
	start := time.Now()
	v, err := c.Judge(context.Background(), Request{QuestionID: "q1", Right: []string{"Пушкин"}, PlayerAnswer: "Гоголь"})
	require.NoError(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
	require.False(t, v.Right)
	require.Equal(t, SourceFallback, v.Source)
	require.Contains(t, v.Reason, context.DeadlineExceeded.Error())
}

func TestCompositeUncertainAndFactorNormalization(t *testing.T) {
	ctx := context.Background()
	req := Request{QuestionID: "q1", Right: []string{"Александр Пушкин"}, PlayerAnswer: "Пушкин"}

	c := &Composite{AI: JudgeFunc(func(context.Context, Request) (Verdict, error) {
		return Verdict{Right: true, Uncertain: true, Factor: 1, Reason: "не уверен"}, nil
	})}
	v, err := c.Judge(ctx, req)
	require.NoError(t, err)
	require.True(t, v.Uncertain)
	require.False(t, v.Right)
	require.Equal(t, 0.0, v.Factor)
	require.Equal(t, SourceAI, v.Source)

	c = &Composite{AI: JudgeFunc(func(context.Context, Request) (Verdict, error) {
		return Verdict{Right: true, Factor: 0}, nil
	})}
	v, _ = c.Judge(ctx, req)
	require.Equal(t, 1.0, v.Factor, "right with factor 0 → full credit")

	c = &Composite{AI: JudgeFunc(func(context.Context, Request) (Verdict, error) {
		return Verdict{Right: true, Factor: 0.5}, nil
	})}
	v, _ = c.Judge(ctx, req)
	require.Equal(t, 0.5, v.Factor, "partial credit preserved")

	c = &Composite{AI: JudgeFunc(func(context.Context, Request) (Verdict, error) {
		return Verdict{Right: false, Factor: 1}, nil
	})}
	v, _ = c.Judge(ctx, req)
	require.Equal(t, 0.0, v.Factor, "wrong → factor 0")
}

func TestCompositeWithoutAI(t *testing.T) {
	c := &Composite{Cache: NewCache(0)}
	v, err := c.Judge(context.Background(), Request{QuestionID: "q1", Right: []string{"Лермонтов"}, PlayerAnswer: "Лермантов"})
	require.NoError(t, err)
	require.True(t, v.Right)
	require.Equal(t, SourceFuzzy, v.Source)
	require.Equal(t, 0, c.Cache.Len())
}

func TestCacheLRU(t *testing.T) {
	c := NewCache(2)
	c.Put("a", Verdict{Reason: "a"})
	c.Put("b", Verdict{Reason: "b"})
	c.Put("c", Verdict{Reason: "c"})
	_, ok := c.Get("a")
	require.False(t, ok, "a evicted")
	_, ok = c.Get("b")
	require.True(t, ok)
	c.Put("d", Verdict{Reason: "d"})
	_, ok = c.Get("c")
	require.False(t, ok, "c evicted after b was touched")
	v, ok := c.Get("b")
	require.True(t, ok)
	require.Equal(t, "b", v.Reason)
	require.Equal(t, 2, c.Len())

	c.Put("b", Verdict{Reason: "b2"})
	v, _ = c.Get("b")
	require.Equal(t, "b2", v.Reason)
	require.Equal(t, 2, c.Len())

	var nilCache *Cache
	nilCache.Put("x", Verdict{})
	_, ok = nilCache.Get("x")
	require.False(t, ok)
	require.Equal(t, 0, nilCache.Len())

	var zero Cache
	zero.Put("x", Verdict{Reason: "x"})
	_, ok = zero.Get("x")
	require.True(t, ok)

	require.Equal(t, "", CacheKey("", "x"))
	require.Equal(t, CacheKey("q", "Пушкин!"), CacheKey("q", " пушкин "))
}
