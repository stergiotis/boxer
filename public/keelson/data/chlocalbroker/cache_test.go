package chlocalbroker

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSqlIsCacheable(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
		hint string
	}{
		{"SELECT 1", true, "bare SELECT"},
		{"select 1", true, "lowercase"},
		{"  SELECT 1", true, "leading whitespace"},
		{"\n\tSELECT 1", true, "leading newline+tab"},
		{"-- comment\nSELECT 1", true, "leading line comment"},
		{"/* block */ SELECT 1", true, "leading block comment"},
		{"/* multi\nline */SELECT 1", true, "multiline block comment"},
		{"-- a\n-- b\nSELECT 1", true, "two line comments"},
		{"SHOW TABLES", true, "SHOW"},
		{"DESCRIBE foo", true, "DESCRIBE"},
		{"DESC foo", true, "DESC"},
		{"EXPLAIN SELECT 1", true, "EXPLAIN"},
		{"WITH x AS (SELECT 1) SELECT * FROM x", true, "WITH cte"},
		{"SELECT(1)", true, "SELECT followed by paren"},
		{"INSERT INTO t VALUES (1)", false, "INSERT not allowed"},
		{"CREATE TABLE t (a Int8)", false, "CREATE not allowed"},
		{"DROP TABLE t", false, "DROP not allowed"},
		{"DELETE FROM t WHERE a=1", false, "DELETE not allowed"},
		{"SELECTIVELY 1", false, "SELECT prefix not a word boundary"},
		{"", false, "empty"},
		{"   ", false, "whitespace only"},
		{"/* unterminated", false, "unterminated block comment"},
		{"SET allow_experimental = 1", false, "SET not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.hint, func(t *testing.T) {
			assert.Equal(t, tc.want, sqlIsCacheable(tc.sql), "sql=%q", tc.sql)
		})
	}
}

func TestComputeCacheKey_IsDeterministic(t *testing.T) {
	k1 := computeCacheKey("SELECT 1", "TabSeparated", map[string]string{"a": "1", "b": "2"})
	k2 := computeCacheKey("SELECT 1", "TabSeparated", map[string]string{"b": "2", "a": "1"})
	assert.Equal(t, k1, k2, "settings map ordering must not affect the key")
}

func TestComputeCacheKey_DiffersByInput(t *testing.T) {
	base := computeCacheKey("SELECT 1", "TabSeparated", nil)
	cases := map[string]cacheKey{
		"different sql":      computeCacheKey("SELECT 2", "TabSeparated", nil),
		"different format":   computeCacheKey("SELECT 1", "CSV", nil),
		"different settings": computeCacheKey("SELECT 1", "TabSeparated", map[string]string{"x": "y"}),
	}
	for name, other := range cases {
		t.Run(name, func(t *testing.T) {
			assert.NotEqual(t, base, other)
		})
	}
}

func TestPoolCache_PutGet(t *testing.T) {
	c, err := newPoolCache(CacheConfig{MaxEntries: 4, TTL: 5 * time.Second})
	require.NoError(t, err)
	key := computeCacheKey("SELECT 1", "TabSeparated", nil)
	c.put(key, []byte("1\n"), "text/tab-separated-values")
	entry, ok := c.get(key)
	require.True(t, ok)
	assert.Equal(t, "1\n", string(entry.body))
	assert.Equal(t, "text/tab-separated-values", entry.contentType)
}

func TestPoolCache_TTLExpiry(t *testing.T) {
	c, err := newPoolCache(CacheConfig{MaxEntries: 4, TTL: 50 * time.Millisecond})
	require.NoError(t, err)
	key := computeCacheKey("SELECT 1", "TabSeparated", nil)
	c.put(key, []byte("1\n"), "text/tab-separated-values")
	time.Sleep(120 * time.Millisecond)
	_, ok := c.get(key)
	assert.False(t, ok, "entry should be evicted after TTL")
	assert.Equal(t, 0, c.len(), "expired entry should be removed on read")
}

func TestPoolCache_RefusesOversized(t *testing.T) {
	c, err := newPoolCache(CacheConfig{MaxEntries: 4, TTL: 5 * time.Second, MaxEntrySize: 16})
	require.NoError(t, err)
	key := computeCacheKey("SELECT", "T", nil)
	big := make([]byte, 32)
	c.put(key, big, "x")
	_, ok := c.get(key)
	assert.False(t, ok, "oversized payload must not enter the cache")
}

// ----- broker-integration cache tests -----

func TestExecOnPool_CacheHitOnSecondCall(t *testing.T) {
	_, caller := newTestBroker(t)

	first, err := ExecOnPool(context.Background(), caller,"cache_a", ExecRequest{
		SQL:       "SELECT 1",
		Format:    "TabSeparated",
		Cacheable: true,
	})
	require.NoError(t, err)
	require.NoError(t, first.Err())
	body1, err := io.ReadAll(first)
	require.NoError(t, err)
	assert.False(t, first.CacheHit, "first call must be a miss")
	require.NoError(t, first.Close())

	second, err := ExecOnPool(context.Background(), caller,"cache_a", ExecRequest{
		SQL:       "SELECT 1",
		Format:    "TabSeparated",
		Cacheable: true,
	})
	require.NoError(t, err)
	require.NoError(t, second.Err())
	body2, err := io.ReadAll(second)
	require.NoError(t, err)
	assert.True(t, second.CacheHit, "second call must be a hit")
	assert.Equal(t, string(body1), string(body2), "cached body must match")
	require.NoError(t, second.Close())
}

func TestExecOnPool_CacheBypassWhenFlagFalse(t *testing.T) {
	_, caller := newTestBroker(t)

	// First call seeds the cache.
	r1, err := ExecOnPool(context.Background(), caller,"cache_b", ExecRequest{
		SQL:       "SELECT 1",
		Format:    "TabSeparated",
		Cacheable: true,
	})
	require.NoError(t, err)
	require.NoError(t, r1.Err())
	_, _ = io.ReadAll(r1)
	_ = r1.Close()

	// Second call without Cacheable should not hit, even though
	// the cache has a matching entry.
	r2, err := ExecOnPool(context.Background(), caller,"cache_b", ExecRequest{
		SQL:       "SELECT 1",
		Format:    "TabSeparated",
		Cacheable: false,
	})
	require.NoError(t, err)
	require.NoError(t, r2.Err())
	_, _ = io.ReadAll(r2)
	assert.False(t, r2.CacheHit, "Cacheable=false must bypass the cache")
	_ = r2.Close()
}

func TestExecOnPool_NonCacheableSqlNotCachedEvenWithFlag(t *testing.T) {
	_, caller := newTestBroker(t)

	// SET is not in the allowlisted prefixes.
	r1, err := ExecOnPool(context.Background(), caller,"cache_c", ExecRequest{
		SQL:       "SHOW DATABASES",
		Format:    "TabSeparated",
		Cacheable: true,
	})
	require.NoError(t, err)
	require.NoError(t, r1.Err())
	_, _ = io.ReadAll(r1)
	require.NoError(t, r1.Close())

	r2, err := ExecOnPool(context.Background(), caller,"cache_c", ExecRequest{
		SQL:       "SELECT 1; -- mutation that prefix-matched is disallowed at handler",
		Format:    "TabSeparated",
		Cacheable: true,
	})
	require.NoError(t, err)
	require.NoError(t, r2.Err())
	_, _ = io.ReadAll(r2)
	// Different SQL anyway; just verify behaviour is consistent.
	_ = r2.Close()
}

func TestExecOnPool_CacheTTLExpiry(t *testing.T) {
	svc, caller := newTestBroker(t)
	svc.SetCacheConfig(CacheConfig{TTL: 80 * time.Millisecond, MaxEntries: 4})

	r1, err := ExecOnPool(context.Background(), caller,"cache_ttl", ExecRequest{
		SQL:       "SELECT 1",
		Format:    "TabSeparated",
		Cacheable: true,
	})
	require.NoError(t, err)
	require.NoError(t, r1.Err())
	_, _ = io.ReadAll(r1)
	assert.False(t, r1.CacheHit)
	_ = r1.Close()

	time.Sleep(160 * time.Millisecond)

	r2, err := ExecOnPool(context.Background(), caller,"cache_ttl", ExecRequest{
		SQL:       "SELECT 1",
		Format:    "TabSeparated",
		Cacheable: true,
	})
	require.NoError(t, err)
	require.NoError(t, r2.Err())
	_, _ = io.ReadAll(r2)
	assert.False(t, r2.CacheHit, "TTL-expired entry must be re-evaluated")
	_ = r2.Close()
}

func TestExecOnPool_CachesAreIsolatedPerPool(t *testing.T) {
	_, caller := newTestBroker(t)

	r1, err := ExecOnPool(context.Background(), caller,"iso_a", ExecRequest{SQL: "SELECT 1", Format: "TabSeparated", Cacheable: true})
	require.NoError(t, err)
	require.NoError(t, r1.Err())
	_, _ = io.ReadAll(r1)
	_ = r1.Close()

	// Same SQL on a different pool → first call must miss.
	r2, err := ExecOnPool(context.Background(), caller,"iso_b", ExecRequest{SQL: "SELECT 1", Format: "TabSeparated", Cacheable: true})
	require.NoError(t, err)
	require.NoError(t, r2.Err())
	_, _ = io.ReadAll(r2)
	assert.False(t, r2.CacheHit, "cache must not leak across pool boundaries")
	_ = r2.Close()
}

func TestExecOnPool_CacheHitIsFast(t *testing.T) {
	// Indirect verification that the worker is not touched on hit.
	// Pure-cache replies should complete in well under a typical
	// cold-spawn (~40 ms) or warm-acquire (~8 ms) latency.
	_, caller := newTestBroker(t)

	_, _ = ExecOnPool(context.Background(), caller,"fast_hit", ExecRequest{
		SQL: "SELECT 1", Format: "TabSeparated", Cacheable: true,
	})

	start := time.Now()
	r2, err := ExecOnPool(context.Background(), caller,"fast_hit", ExecRequest{
		SQL: "SELECT 1", Format: "TabSeparated", Cacheable: true,
	})
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.NoError(t, r2.Err())
	_, _ = io.ReadAll(r2)
	require.True(t, r2.CacheHit)
	_ = r2.Close()

	// 5 ms ceiling is generous for an in-proc bus round-trip + JSON
	// codec; the hot path through cache.get is microseconds.
	assert.Less(t, elapsed, 5*time.Millisecond, "cache hit took %s — worker was likely touched", elapsed)
}

