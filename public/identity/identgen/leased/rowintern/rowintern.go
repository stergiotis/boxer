// Package rowintern is a keyed id generator whose key-to-id mapping lives on
// the one ClickHouse server a deployment runs (ADR-0405 §SD3): the same
// natural key resolves to the same tagged id from every process and across
// restarts. It is the internalizing counterpart of rowalloc's lease row,
// and the "heavier, coordinating" authority leased.Internalizer leaves out
// on purpose: there the mapping is one instance's, here it is the server's.
//
// # The directory
//
// One table holds a row per key that was ever resolved: the stream's tag
// value, a scope, the key and the id. A key that is not there is given an
// id drawn from a sequential generator of the stream and inserted; then the
// key is read back, and what the read returns is the answer.
//
// # Which row is the mapping
//
// Two processes can both find a key missing and both insert a row for it,
// under two ids. The mapping is the row at the lowest block position
// (_block_number, _block_offset), which the rowcas table settings persist
// and merges keep. On one server a part's block number is taken and the
// part made visible under one lock, so a row that is visible has every
// lower-numbered row visible beside it: a reader that has inserted and
// then reads sees the row that will be the lowest for good. The id of the
// row that lost is never handed out; it is burnt, as a lost lease's block
// is (ADR-0319 §SD4). This is the rule rowalloc's lease row rests on, and
// it holds for one server only (ADR-0319 §SD1).
//
// # What the directory is
//
// State. The ids it holds are in rows elsewhere, and nothing else says
// which key each belongs to: a directory dropped and created again gives
// every key a new id. It is backed up, restored and moved with the tables
// whose ids it maps.
package rowintern

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/rowcas"
)

// DatabaseName is the default database, the runtime's own.
const DatabaseName = "boxer"

// TableName is the directory's bare name.
const TableName = "idkey"

// DefaultMaxCached is how many resolved keys an [Internalizer] remembers
// before it forgets them all and asks the server again.
const DefaultMaxCached = 1 << 16

// resolveChunk bounds the keys one statement names.
const resolveChunk = 512

// Layout names the database the directory lives in. The zero value is the
// runtime's own.
type Layout struct {
	Database string
}

// DatabaseName is the layout's database, defaulting to [DatabaseName].
func (inst Layout) DatabaseName() (db string) {
	db = inst.Database
	if db == "" {
		db = DatabaseName
	}
	return
}

// Table is the qualified directory table reference.
func (inst Layout) Table() (name string) { return inst.DatabaseName() + "." + TableName }

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	scopePattern      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
)

func (inst Layout) validate() (err error) {
	if !identifierPattern.MatchString(inst.DatabaseName()) {
		err = eb.Build().Str("database", inst.DatabaseName()).Errorf("the directory's database is not a plain identifier")
	}
	return
}

// ProvisionSQL is what provisioning runs, in order: the database, the table
// with the block-position settings the mapping rule needs, and the one
// ALTER that puts them on a table created without. Every statement is
// idempotent.
func ProvisionSQL(layout Layout) (stmts []string) {
	return []string{
		"CREATE DATABASE IF NOT EXISTS " + layout.DatabaseName(),
		"CREATE TABLE IF NOT EXISTS " + layout.Table() + " (" +
			"tag UInt32 COMMENT 'the claimed tag value of the stream the id belongs to', " +
			"scope LowCardinality(String) COMMENT 'whose keys these are: a key is unique within a scope', " +
			"key String COMMENT 'the natural key', " +
			"id UInt64 COMMENT 'the tagged id the key resolves to, in the row at the lowest block position'" +
			") ENGINE = MergeTree ORDER BY (tag, scope, key)" +
			" SETTINGS index_granularity = 8192" + rowcas.TableSettingsTail +
			" COMMENT 'natural key to tagged id, one mapping per key (ADR-0405 §SD3)'",
		rowcas.AlterTableSettingsSQL(layout.Table()),
	}
}

// ProvisionIn creates the directory; idempotent.
func ProvisionIn(ctx context.Context, exec recordstore.ExecutorI, layout Layout) (err error) {
	if err = layout.validate(); err != nil {
		return
	}
	for _, s := range ProvisionSQL(layout) {
		if err = exec.Exec(ctx, s); err != nil {
			return eb.Build().Str("table", layout.Table()).Errorf("provision id directory: %w", err)
		}
	}
	return
}

// Internalizer is the keyed generator of one stream and one scope over a
// provisioned directory. It is safe for concurrent use. It remembers the
// keys it resolved, so a key costs the server once per process.
type Internalizer struct {
	exec       recordstore.ExecutorI
	layout     Layout
	scope      string
	candidates identifier.IdGeneratorI
	tag        identifier.IdTag

	mu        sync.Mutex
	cache     map[string]identifier.TaggedId
	maxCached int
}

var _ identifier.IdGeneratorI = (*Internalizer)(nil)

// New builds the keyed generator for scope over the directory in layout.
//
// scope says whose keys these are — a store, a writer — and keeps the keys
// of two writers apart: a natural key is unique within its writer, not
// across them. candidates mints the ids new keys are given. It must be a
// sequential generator of one stream whose every id is unique on the
// server — a leased sequence — and never one that falls back to ids unique
// only within a process: an id written into the directory is every
// process's. An id it hands out under another tag than its own is refused.
// The executor stays the caller's.
func New(exec recordstore.ExecutorI, layout Layout, scope string, candidates identifier.IdGeneratorI) (inst *Internalizer, err error) {
	if exec == nil {
		return nil, eh.Errorf("rowintern: executor is nil")
	}
	if candidates == nil {
		return nil, eh.Errorf("rowintern: no generator to draw new ids from")
	}
	if err = layout.validate(); err != nil {
		return nil, err
	}
	if !scopePattern.MatchString(scope) {
		return nil, eb.Build().Str("scope", scope).Errorf("rowintern: a scope is a name: letters, digits, '_', '.', '-', starting with a letter")
	}
	tag := candidates.GetTag()
	if !tag.IsValid() {
		return nil, eh.Errorf("rowintern: the generator of new ids has no valid tag")
	}
	inst = &Internalizer{exec: exec, layout: layout, scope: scope, candidates: candidates, tag: tag,
		cache: make(map[string]identifier.TaggedId), maxCached: DefaultMaxCached}
	return
}

// ErrReadOnly is what a generator built by [NewReadOnly] returns when asked
// for the id of a key that has none. Match with errors.Is.
var ErrReadOnly = errors.New("rowintern: the generator is read-only and the key has no id")

// NewReadOnly is the reader's generator for scope over the directory in
// layout: it looks keys up under the stream tagValue names and gives no key
// an id. [Internalizer.Lookup] and [Internalizer.Lookups] answer as they do
// on any generator; [Internalizer.GetId] of a key without an id fails with
// [ErrReadOnly]. It holds no lease and writes nothing.
func NewReadOnly(exec recordstore.ExecutorI, layout Layout, scope string, tagValue identifier.TagValue) (inst *Internalizer, err error) {
	if exec == nil {
		return nil, eh.Errorf("rowintern: executor is nil")
	}
	if err = layout.validate(); err != nil {
		return nil, err
	}
	if !scopePattern.MatchString(scope) {
		return nil, eb.Build().Str("scope", scope).Errorf("rowintern: a scope is a name: letters, digits, '_', '.', '-', starting with a letter")
	}
	tag := tagValue.GetTag()
	if !tag.IsValid() {
		return nil, eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("rowintern: not a valid tag value")
	}
	inst = &Internalizer{exec: exec, layout: layout, scope: scope, tag: tag,
		cache: make(map[string]identifier.TaggedId), maxCached: DefaultMaxCached}
	return
}

// Scope is the scope this generator's keys live in.
func (inst *Internalizer) Scope() string { return inst.scope }

// GetTag is the tag of the stream every id this generator returns carries.
func (inst *Internalizer) GetTag() identifier.IdTag { return inst.tag }

// Release does nothing: the mapping is the server's.
func (inst *Internalizer) Release() (err error) { return nil }

// GetId resolves naturalKey to its tagged id, minting and recording one
// when the key has none. fresh reports that this call's id became the
// key's. An empty key is refused: it would name every row that has none.
func (inst *Internalizer) GetId(ctx context.Context, naturalKey []byte) (id identifier.TaggedId, fresh bool, err error) {
	ids, freshes, err := inst.GetIds(ctx, [][]byte{naturalKey})
	if err != nil {
		return
	}
	return ids[0], freshes[0], nil
}

// GetUntaggedId is GetId without the tag.
func (inst *Internalizer) GetUntaggedId(ctx context.Context, naturalKey []byte) (untagged identifier.UntaggedId, fresh bool, err error) {
	id, fresh, err := inst.GetId(ctx, naturalKey)
	return id.RemoveTag(), fresh, err
}

// Lookup is the id naturalKey resolves to, without giving it one: found is
// false for a key that was never resolved. It is the reader's half — a
// reader must not mint.
func (inst *Internalizer) Lookup(ctx context.Context, naturalKey []byte) (id identifier.TaggedId, found bool, err error) {
	if len(naturalKey) == 0 {
		err = eh.Errorf("rowintern: empty natural key")
		return
	}
	k := string(naturalKey)
	inst.mu.Lock()
	id, found = inst.cache[k]
	inst.mu.Unlock()
	if found {
		return
	}
	got, err := inst.read(ctx, []string{k})
	if err != nil {
		return
	}
	if id, found = got[k]; found {
		inst.remember(map[string]identifier.TaggedId{k: id})
	}
	return
}

// Lookups is [Internalizer.Lookup] for several keys in one statement per
// [resolveChunk]: ids[i] and found[i] answer keys[i].
func (inst *Internalizer) Lookups(ctx context.Context, keys [][]byte) (ids []identifier.TaggedId, found []bool, err error) {
	ids = make([]identifier.TaggedId, len(keys))
	found = make([]bool, len(keys))
	var unknown []string
	inst.mu.Lock()
	for i, key := range keys {
		if len(key) == 0 {
			inst.mu.Unlock()
			return nil, nil, eb.Build().Int("index", i).Errorf("rowintern: empty natural key")
		}
		if id, ok := inst.cache[string(key)]; ok {
			ids[i], found[i] = id, true
		} else {
			unknown = append(unknown, string(key))
		}
	}
	inst.mu.Unlock()
	if len(unknown) == 0 {
		return
	}
	resolved := make(map[string]identifier.TaggedId, len(unknown))
	for lo := 0; lo < len(unknown); lo += resolveChunk {
		got, rerr := inst.read(ctx, unknown[lo:min(lo+resolveChunk, len(unknown))])
		if rerr != nil {
			return nil, nil, rerr
		}
		for k, id := range got {
			resolved[k] = id
		}
	}
	inst.remember(resolved)
	for i, key := range keys {
		if id, ok := resolved[string(key)]; ok {
			ids[i], found[i] = id, true
		}
	}
	return
}

// GetIds resolves every key of keys, in order; a key named twice resolves
// once. It costs the server at most three statements per [resolveChunk]
// keys it has not seen: a read, an insert of the missing ones, and the read
// that says which rows are the mappings.
func (inst *Internalizer) GetIds(ctx context.Context, keys [][]byte) (ids []identifier.TaggedId, fresh []bool, err error) {
	ids = make([]identifier.TaggedId, len(keys))
	fresh = make([]bool, len(keys))
	var unknown []string
	seen := make(map[string]struct{}, len(keys))
	inst.mu.Lock()
	for i, key := range keys {
		if len(key) == 0 {
			inst.mu.Unlock()
			return nil, nil, eb.Build().Int("index", i).Errorf("rowintern: empty natural key")
		}
		k := string(key)
		if id, ok := inst.cache[k]; ok {
			ids[i] = id
			continue
		}
		if _, dup := seen[k]; !dup {
			seen[k] = struct{}{}
			unknown = append(unknown, k)
		}
	}
	inst.mu.Unlock()
	if len(unknown) == 0 {
		return
	}

	resolved := make(map[string]identifier.TaggedId, len(unknown))
	minted := make(map[string]struct{})
	for lo := 0; lo < len(unknown); lo += resolveChunk {
		chunk := unknown[lo:min(lo+resolveChunk, len(unknown))]
		var got map[string]identifier.TaggedId
		if got, err = inst.read(ctx, chunk); err != nil {
			return nil, nil, err
		}
		candidates := make(map[string]identifier.TaggedId)
		for _, k := range chunk {
			if id, ok := got[k]; ok {
				resolved[k] = id
				continue
			}
			if inst.candidates == nil {
				return nil, nil, eb.Build().Str("scope", inst.scope).Errorf("%w", ErrReadOnly)
			}
			var c identifier.TaggedId
			if c, _, err = inst.candidates.GetId(ctx, nil); err != nil {
				return nil, nil, eb.Build().Str("scope", inst.scope).Errorf("rowintern: draw an id for a new key: %w", err)
			}
			if c.GetTag() != inst.tag {
				return nil, nil, eb.Build().Str("scope", inst.scope).Uint64("id", c.Value()).
					Errorf("rowintern: the generator handed out an id under another tag than its stream's; an id unique only within a process must not enter the directory")
			}
			candidates[k] = c
		}
		if len(candidates) == 0 {
			continue
		}
		if err = inst.exec.Exec(ctx, inst.insertSQL(candidates)); err != nil {
			return nil, nil, eb.Build().Str("scope", inst.scope).Int("keys", len(candidates)).Errorf("rowintern: record new keys: %w", err)
		}
		missing := make([]string, 0, len(candidates))
		for k := range candidates {
			missing = append(missing, k)
		}
		if got, err = inst.read(ctx, missing); err != nil {
			return nil, nil, err
		}
		for _, k := range missing {
			id, ok := got[k]
			if !ok {
				return nil, nil, eb.Build().Str("scope", inst.scope).Errorf("rowintern: a key is not in the directory after its insert")
			}
			resolved[k] = id
			if id == candidates[k] {
				minted[k] = struct{}{}
			}
		}
	}
	inst.remember(resolved)
	for i, key := range keys {
		k := string(key)
		if id, ok := resolved[k]; ok {
			ids[i] = id
			_, fresh[i] = minted[k]
			// Only the first mention of a key in this call is the mint.
			delete(minted, k)
		}
	}
	return
}

// remember keeps resolved mappings for later calls, forgetting everything
// first when the cache would grow past its bound.
func (inst *Internalizer) remember(resolved map[string]identifier.TaggedId) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if len(inst.cache)+len(resolved) > inst.maxCached {
		inst.cache = make(map[string]identifier.TaggedId, len(resolved))
	}
	for k, id := range resolved {
		inst.cache[k] = id
	}
}

// read is the mapping of each of keys that has one.
func (inst *Internalizer) read(ctx context.Context, keys []string) (got map[string]identifier.TaggedId, err error) {
	got = make(map[string]identifier.TaggedId, len(keys))
	var cols struct {
		Key []string `ch:"k"`
		Id  []uint64 `ch:"id"`
	}
	for rec, qerr := range inst.exec.QueryArrow(ctx, inst.readSQL(keys)) {
		if qerr != nil {
			return nil, eb.Build().Str("scope", inst.scope).Errorf("rowintern: read the directory: %w", qerr)
		}
		_, err = chrows.Decode(&cols, rec)
		rec.Release()
		if err != nil {
			return nil, eb.Build().Str("scope", inst.scope).Errorf("rowintern: decode the directory's answer: %w", err)
		}
	}
	for i, hk := range cols.Key {
		raw, derr := hex.DecodeString(hk)
		if derr != nil {
			return nil, eb.Build().Str("scope", inst.scope).Errorf("rowintern: the directory returned a key that is not hex: %w", derr)
		}
		got[string(raw)] = identifier.TaggedId(cols.Id[i])
	}
	return
}

func keyLit(key string) string { return "unhex('" + hex.EncodeToString([]byte(key)) + "')" }

func (inst *Internalizer) where() string {
	return "tag = " + strconv.FormatUint(uint64(inst.tag.GetValue()), 10) + " AND scope = '" + inst.scope + "'"
}

// readSQL reads, for each key, the id of its row at the lowest block
// position: the mapping.
func (inst *Internalizer) readSQL(keys []string) (sql string) {
	lits := make([]string, 0, len(keys))
	for _, k := range keys {
		lits = append(lits, keyLit(k))
	}
	return "SELECT hex(key) AS k, argMin(id, (_block_number, _block_offset)) AS id FROM " + inst.layout.Table() +
		" WHERE " + inst.where() + " AND key IN (" + strings.Join(lits, ", ") + ") GROUP BY key"
}

// insertSQL records candidate ids for keys that had no row. async_insert
// never sits on this path: the read that follows must see the rows.
func (inst *Internalizer) insertSQL(candidates map[string]identifier.TaggedId) (sql string) {
	tag := strconv.FormatUint(uint64(inst.tag.GetValue()), 10)
	rows := make([]string, 0, len(candidates))
	for k, id := range candidates {
		rows = append(rows, "("+tag+", '"+inst.scope+"', "+keyLit(k)+", "+strconv.FormatUint(id.Value(), 10)+")")
	}
	return "INSERT INTO " + inst.layout.Table() + " (tag, scope, key, id) SETTINGS async_insert = 0 VALUES " + strings.Join(rows, ", ")
}
