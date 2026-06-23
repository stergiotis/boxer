package chlocalbroker

import (
	"sort"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"lukechampine.com/blake3"
)

// DefaultCacheMaxEntries is the upper bound on retained results per
// pool. LRU-evicted when exceeded.
const DefaultCacheMaxEntries = 256

// DefaultCacheMaxEntrySize refuses to cache results whose body
// exceeds this many bytes — keeps one outlier from monopolising the
// cache.
const DefaultCacheMaxEntrySize = 1 << 20 // 1 MiB

// DefaultCacheTTL bounds staleness when the underlying data source
// changes without a flush; lazy on read.
const DefaultCacheTTL = 60 * time.Second

// CacheConfig parameterises per-pool result caches. Zero values fall
// back to the Default* constants via withCacheDefaults.
type CacheConfig struct {
	// MaxEntries is the upper bound on retained results. LRU-evicted
	// when exceeded.
	MaxEntries int
	// MaxEntrySize refuses to cache a result whose body exceeds this
	// many bytes. Set to 0 to defer to DefaultCacheMaxEntrySize.
	MaxEntrySize int
	// TTL ages out entries on read (lazy invalidation).
	TTL time.Duration
}

func (inst CacheConfig) withDefaults() (cfg CacheConfig) {
	cfg = inst
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultCacheMaxEntries
	}
	if cfg.MaxEntrySize <= 0 {
		cfg.MaxEntrySize = DefaultCacheMaxEntrySize
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultCacheTTL
	}
	return
}

type cacheKey [32]byte

type cacheEntry struct {
	body        []byte
	contentType string
	storedAt    time.Time
}

// poolCache is a per-pool result LRU. blake3-keyed; TTL-bounded on read.
type poolCache struct {
	cfg CacheConfig
	mu  sync.Mutex
	lru *lru.Cache[cacheKey, *cacheEntry]
}

func newPoolCache(cfg CacheConfig) (c *poolCache, err error) {
	cfg = cfg.withDefaults()
	l, lruErr := lru.New[cacheKey, *cacheEntry](cfg.MaxEntries)
	if lruErr != nil {
		err = lruErr
		return
	}
	c = &poolCache{cfg: cfg, lru: l}
	return
}

// get returns the cache entry if present and not yet expired. Stale
// entries are removed on read.
func (inst *poolCache) get(key cacheKey) (entry *cacheEntry, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	e, present := inst.lru.Get(key)
	if !present {
		return
	}
	if time.Since(e.storedAt) > inst.cfg.TTL {
		inst.lru.Remove(key)
		return
	}
	entry = e
	ok = true
	return
}

// put stores body in the cache. Caller MUST pass a stable slice (no
// aliasing with the pool buffer the broker drained into). Refuses to
// cache entries above MaxEntrySize.
func (inst *poolCache) put(key cacheKey, body []byte, contentType string) {
	if len(body) > inst.cfg.MaxEntrySize {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.lru.Add(key, &cacheEntry{
		body:        body,
		contentType: contentType,
		storedAt:    time.Now(),
	})
}

// len reports current entry count. For tests + telemetry.
func (inst *poolCache) len() (n int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n = inst.lru.Len()
	return
}

// computeCacheKey is blake3(sql || 0x00 || format || 0x00 ||
// canonicalised settings). Settings keys are sorted so the same
// logical request always hashes identically.
func computeCacheKey(sql string, format string, settings map[string]string) (key cacheKey) {
	h := blake3.New(32, nil)
	_, _ = h.Write([]byte(sql))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(format))
	_, _ = h.Write([]byte{0})
	if len(settings) > 0 {
		keys := make([]string, 0, len(settings))
		for k := range settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = h.Write([]byte(k))
			_, _ = h.Write([]byte{1})
			_, _ = h.Write([]byte(settings[k]))
			_, _ = h.Write([]byte{1})
		}
	}
	sum := h.Sum(nil)
	copy(key[:], sum)
	return
}

// foldInputTables derives a new cache key from a base key and a
// request's InputTables, so a cached result never outlives a changed
// input under unchanged SQL (ADR-0094 §SD5). Returns base unchanged
// when there are no input tables. Table names are sorted so map
// iteration order does not perturb the key; the full table bytes are
// folded in (not just a length) because that is what makes the key
// faithful to the data the query actually saw.
func foldInputTables(base cacheKey, inputTables map[string][]byte) (key cacheKey) {
	if len(inputTables) == 0 {
		key = base
		return
	}
	names := make([]string, 0, len(inputTables))
	for name := range inputTables {
		names = append(names, name)
	}
	sort.Strings(names)
	h := blake3.New(32, nil)
	_, _ = h.Write(base[:])
	for _, name := range names {
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(inputTables[name])
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	copy(key[:], sum)
	return
}

// cacheableSQLPrefixes is the set of opening keywords whose SQL is
// (a) read-only by construction, (b) most likely to be deterministic
// in a clickhouse-local scratch context. Caller's `Cacheable` flag
// must also be true; this prefix gate is the broker's
// non-bypassable safety net for "don't cache a CREATE / INSERT
// / DROP".
var cacheableSQLPrefixes = []string{
	"SELECT",
	"SHOW",
	"DESCRIBE",
	"DESC",
	"EXPLAIN",
	"WITH",
}

// sqlIsCacheable returns true if the SQL begins with one of the
// allowlisted prefixes after stripping leading whitespace and line
// / block comments. Conservative — anything ambiguous is "not
// cacheable" and gets the worker path.
func sqlIsCacheable(sql string) (ok bool) {
	s := stripSQLNoise(sql)
	if s == "" {
		return
	}
	upper := strings.ToUpper(s)
	for _, p := range cacheableSQLPrefixes {
		if !strings.HasPrefix(upper, p) {
			continue
		}
		// Ensure the prefix is followed by non-identifier rune so
		// `SELECTIVELY` does not match `SELECT`.
		if len(upper) == len(p) {
			ok = true
			return
		}
		ch := upper[len(p)]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '(' || ch == '/' || ch == '-' {
			ok = true
			return
		}
	}
	return
}

// stripSQLNoise discards leading whitespace + `--` line comments +
// `/* ... */` block comments, returning the suffix that begins with
// the first SQL keyword. Best-effort; an unterminated `/*` returns
// the empty string (treated as not cacheable).
func stripSQLNoise(sql string) (rest string) {
	rest = sql
	for {
		// Trim leading whitespace.
		for len(rest) > 0 {
			ch := rest[0]
			if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
				rest = rest[1:]
				continue
			}
			break
		}
		if len(rest) == 0 {
			return
		}
		// Line comment.
		if strings.HasPrefix(rest, "--") {
			if i := strings.IndexByte(rest, '\n'); i >= 0 {
				rest = rest[i+1:]
				continue
			}
			rest = ""
			return
		}
		// Block comment.
		if strings.HasPrefix(rest, "/*") {
			i := strings.Index(rest, "*/")
			if i < 0 {
				rest = ""
				return
			}
			rest = rest[i+2:]
			continue
		}
		return
	}
}
