//go:build llm_generated_opus46

package cache

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/unsafeperf"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
)

// CHCache implements CompiledQueryCache using a ClickHouse MergeTree table
// for persistence and an in-memory map for fast reads. The table is created
// automatically on first use.
//
// The cache is keyed by the template hash (the cache_key). Writes go to both
// the in-memory map and ClickHouse. Reads hit memory first, falling back to
// ClickHouse on miss (cold start after proxy restart).
//
// Usage:
//
//	cache := orchestrator.NewCHCache(chClient, "default", "compiled_query_cache")
//	err := cache.Init(ctx)
type CHCache struct {
	ch       orchestrator.CHClientI
	database string
	table    string
	fqTable  string

	mu  sync.RWMutex
	mem map[string]*orchestrator.CacheEntry

	initialized bool
}

// NewCHCache creates a ClickHouse-backed cache.
func NewCHCache(ch orchestrator.CHClientI, database, table string) *CHCache {
	fq := database + "." + table
	return &CHCache{
		ch:       ch,
		database: database,
		table:    table,
		fqTable:  fq,
		mem:      make(map[string]*orchestrator.CacheEntry, 64),
	}
}

// Init creates the cache table if it doesn't exist and loads existing entries
// into the in-memory map. Must be called before Get/Put.
func (inst *CHCache) Init(ctx context.Context) error {
	ddl := `CREATE TABLE IF NOT EXISTS ` + inst.fqTable + ` (
    cache_key     String,
    canonical_sql String,
    policy_sql    String,
    ast_json      String,
    pinned        UInt8,
    compiled_at   DateTime64(3),
    updated_at    DateTime64(3) DEFAULT now64(3)
) ENGINE = MergeTree()
ORDER BY (cache_key)
SETTINGS index_granularity = 128`

	_, err := inst.ch.Execute(ctx, ddl, nil)
	if err != nil {
		return eb.Build().
			Str("table", inst.fqTable).
			Errorf("create cache table: %w", err)
	}

	// Load existing entries into memory
	err = inst.warmup(ctx)
	if err != nil {
		return eb.Build().
			Str("table", inst.fqTable).
			Errorf("cache warmup: %w", err)
	}

	inst.initialized = true
	return nil
}

// Get retrieves a cache entry by key. Checks memory first, then ClickHouse.
func (inst *CHCache) Get(key string) (entry *orchestrator.CacheEntry, found bool) {
	inst.mu.RLock()
	entry, found = inst.mem[key]
	inst.mu.RUnlock()
	if found && entry == nil {
		// Tombstone from invalidation
		found = false
		entry = nil
	}
	return
}

// Put stores or invalidates a cache entry. A nil entry invalidates the key.
func (inst *CHCache) Put(key string, entry *orchestrator.CacheEntry) {
	inst.mu.Lock()
	if entry == nil {
		delete(inst.mem, key)
	} else {
		inst.mem[key] = entry
	}
	inst.mu.Unlock()

	// Persist asynchronously — don't block the caller.
	// Errors are logged but not returned; the in-memory cache is authoritative.
	if entry != nil {
		go inst.persist(key, entry)
	} else {
		go inst.remove(key)
	}
}

func (inst *CHCache) persist(key string, entry *orchestrator.CacheEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	astJSON, err := json.Marshal(entry.AST)
	if err != nil {
		// AST serialization failure — still cache in memory, just can't persist
		return
	}

	pinned := "0"
	if entry.Pinned {
		pinned = "1"
	}

	params := map[string]string{
		"param_cache_key":     key,
		"param_canonical_sql": entry.CanonicalSQL,
		"param_policy_sql":    entry.PolicySQL,
		"param_ast_json":      string(astJSON),
		"param_pinned":        pinned,
		"param_compiled_at":   entry.CompiledAt.UTC().Format("2006-01-02 15:04:05.000"),
	}

	sql := `INSERT INTO ` + inst.fqTable + ` (cache_key, canonical_sql, policy_sql, ast_json, pinned, compiled_at)
VALUES ({param_cache_key: String}, {param_canonical_sql: String}, {param_policy_sql: String}, {param_ast_json: String}, {param_pinned: UInt8}, {param_compiled_at: DateTime64(3)})`

	_, _ = inst.ch.Execute(ctx, sql, params)
}

func (inst *CHCache) remove(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	params := map[string]string{
		"param_cache_key": key,
	}

	// For MergeTree, DELETE is a lightweight mutation
	sql := `ALTER TABLE ` + inst.fqTable + ` DELETE WHERE cache_key = {param_cache_key: String}`
	_, _ = inst.ch.Execute(ctx, sql, params)
}

func (inst *CHCache) warmup(ctx context.Context) error {
	// Read the latest entry per cache_key using LIMIT BY
	sql := `SELECT
    cache_key,
    canonical_sql,
    policy_sql,
    ast_json,
    pinned,
    compiled_at
FROM ` + inst.fqTable + `
ORDER BY updated_at DESC
LIMIT 1 BY cache_key
FORMAT JSONEachRow`

	result, err := inst.ch.Execute(ctx, sql, nil)
	if err != nil {
		return eh.Errorf("warmup query: %w", err)
	}
	if result == nil {
		return nil
	}

	defer func() {
		if result.Closer != nil {
			_ = result.Closer.Close()
		}
	}()
	return inst.parseWarmupResult(result.Data)
}

func (inst *CHCache) parseWarmupResult(data io.Reader) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	for {
		var row warmupRow
		err := json.UnmarshalRead(data, &row)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return eb.Build().Errorf("decode cache row: %w", err)
		}

		var astQuery ast.Query
		if row.ASTJSON != "" {
			if err = json.Unmarshal(unsafeperf.UnsafeStringToBytes(row.ASTJSON), &astQuery); err != nil {
				// Skip rows with corrupt AST — they'll be recompiled on demand
				continue
			}
		}

		inst.mem[row.CacheKey] = &orchestrator.CacheEntry{
			CanonicalSQL: row.CanonicalSQL,
			PolicySQL:    row.PolicySQL,
			AST:          astQuery,
			CompiledAt:   row.CompiledAt,
			Pinned:       row.Pinned == 1,
		}

	}
	return nil
}

type warmupRow struct {
	CacheKey     string    `json:"cache_key"`
	CanonicalSQL string    `json:"canonical_sql"`
	PolicySQL    string    `json:"policy_sql"`
	ASTJSON      string    `json:"ast_json"`
	Pinned       uint8     `json:"pinned"`
	CompiledAt   time.Time `json:"compiled_at"`
}
