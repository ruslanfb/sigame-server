package ai

import (
	"container/list"
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultCacheSize is the number of verdicts kept by NewCache(0).
const DefaultCacheSize = 10_000

// Composite is the judge the room uses. Non-text answer types are decided
// deterministically; text answers that equal an accepted (or known wrong)
// answer are decided without the AI; everything else goes to the AI judge,
// bounded by AITimeout, with the fuzzy judge as fallback (Source "fallback")
// when the AI is unavailable. AI/fallback verdicts are cached per
// (QuestionID, normalized answer) so that identical answers in one question
// receive identical verdicts.
type Composite struct {
	Fuzzy     FuzzyJudge
	AI        Judge         // nil = AI disabled
	Cache     *Cache        // nil = no caching
	AITimeout time.Duration // default DefaultTimeout
	Logger    *slog.Logger  // default slog.Default()
}

// Judge implements Judge. It never returns an error for text answers when the
// AI fails; the fallback verdict carries Source "fallback" and the cause in Reason.
func (c *Composite) Judge(ctx context.Context, req Request) (Verdict, error) {
	if !IsText(req.AnswerType) {
		return c.Fuzzy.Judge(ctx, req)
	}
	if v, ok := ExactMatch(req); ok {
		return v, nil
	}
	if c.AI == nil {
		return c.Fuzzy.Judge(ctx, req)
	}
	key := CacheKey(req.QuestionID, req.PlayerAnswer)
	if key != "" {
		if v, ok := c.Cache.Get(key); ok {
			return v, nil
		}
	}
	v := c.judgeAI(ctx, req)
	if key != "" {
		c.Cache.Put(key, v)
	}
	return v, nil
}

func (c *Composite) judgeAI(ctx context.Context, req Request) Verdict {
	timeout := c.AITimeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	aiCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	v, err := c.AI.Judge(aiCtx, req)
	if err != nil {
		c.logger().Warn("ai: judge unavailable, using fuzzy fallback", "questionId", req.QuestionID, "err", err)
		fb, _ := c.Fuzzy.Judge(ctx, req)
		fb.Source = SourceFallback
		fb.Reason = "AI unavailable (" + err.Error() + "); " + fb.Reason
		return fb
	}
	v.Source = SourceAI
	if v.Uncertain {
		v.Right = false
		v.Factor = 0
	}
	if v.Right && (v.Factor <= 0 || v.Factor > 1) {
		v.Factor = 1
	}
	if !v.Right {
		v.Factor = 0
	}
	return v
}

func (c *Composite) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

// CacheKey builds the cache key; empty when questionID is empty.
func CacheKey(questionID, answer string) string {
	if questionID == "" {
		return ""
	}
	return questionID + "\x00" + Normalize(answer)
}

// Cache is a small mutex-protected LRU of verdicts. The zero value and a nil
// *Cache are usable (nil never hits).
type Cache struct {
	mu    sync.Mutex
	cap   int
	ll    *list.List
	items map[string]*list.Element
}

type cacheEntry struct {
	key string
	v   Verdict
}

// NewCache returns an LRU cache holding up to capacity verdicts (0 → DefaultCacheSize).
func NewCache(capacity int) *Cache {
	if capacity <= 0 {
		capacity = DefaultCacheSize
	}
	return &Cache{cap: capacity, ll: list.New(), items: make(map[string]*list.Element)}
}

// Get returns the cached verdict and marks it recently used.
func (c *Cache) Get(key string) (Verdict, bool) {
	if c == nil {
		return Verdict{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return Verdict{}, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*cacheEntry).v, true
}

// Put stores a verdict, evicting the least recently used entry when full.
func (c *Cache) Put(key string, v Verdict) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = make(map[string]*list.Element)
		c.ll = list.New()
	}
	if c.cap <= 0 {
		c.cap = DefaultCacheSize
	}
	if el, ok := c.items[key]; ok {
		el.Value.(*cacheEntry).v = v
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&cacheEntry{key: key, v: v})
	for c.ll.Len() > c.cap {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.items, last.Value.(*cacheEntry).key)
	}
}

// Len returns the number of cached verdicts.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}
