// Package rowalloc is an identgen.AllocatorI over one lease row per tag on
// the one ClickHouse server a deployment runs (ADR-0319 §SD1, §SD3): the row
// carries the next unleased body, and a block is reserved by a compare-and-set
// on it — a guarded lightweight update writing the new bound and a holder
// token, then a read of the row by the same client. The read-back is the
// answer. The statements are composed through rowcas, so the table lives
// under rowcas's conditions (its package doc); this package states them on
// every statement it can and has no way to enforce the rest.
//
// # Which row is the lease
//
// The row for a tag is inserted the first time a tag is leased, and two
// processes can both find no row and both insert one. A duplicate is not
// harmless: a reader that saw the first row at body 1 could later guard on 1
// and win on the duplicate, handing out a block the first row already gave
// away. So the lease is not "the row for the tag" but the row for the tag
// with the lowest block position (_block_number, _block_offset), which the
// rowcas table settings persist and merges keep. Block numbers only grow on
// one server, so a duplicate inserted later is never the lease and is never
// read or updated again; every guard pins the position as well as the value.
//
// # What a lost reply costs
//
// A block is reserved when the update that moved the lease row over it
// commits, whether or not the caller hears of it. A caller that cannot see its
// own token on the row afterwards — the update lost, the reply lost, or a
// later lease already moved the row on — does not hand the block out: it is
// burnt, never re-handed, as identgen.AllocatorI requires. Gaps are never
// recycled and do not matter (ADR-0319 §SD4).
package rowalloc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/rowcas"
)

// DatabaseName is the default database, the runtime's own.
const DatabaseName = "boxer"

// TableName is the lease table's bare name.
const TableName = "idlease"

// DefaultMaxAttempts bounds how many lost compare-and-sets one AllocateBlock
// retries before it reports contention.
const DefaultMaxAttempts = 32

const maxUint64 = ^uint64(0)

// Layout names the database the lease table lives in. The zero value is the
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

// Table is the qualified lease table reference.
func (inst Layout) Table() (name string) { return inst.DatabaseName() + "." + TableName }

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (inst Layout) validate() (err error) {
	if !identifierPattern.MatchString(inst.DatabaseName()) {
		err = eb.Build().Str("database", inst.DatabaseName()).Errorf("the lease table's database is not a plain identifier")
	}
	return
}

// ProvisionSQL is what provisioning runs, in order: the database, the table
// with the block-position settings a lightweight update needs, and the one
// ALTER a guarded table admits, for a table created before the settings were.
// Every statement is idempotent.
func ProvisionSQL(layout Layout) (stmts []string) {
	return []string{
		"CREATE DATABASE IF NOT EXISTS " + layout.DatabaseName(),
		"CREATE TABLE IF NOT EXISTS " + layout.Table() + " (" +
			"tag UInt32 COMMENT 'the claimed tag value whose stream this row leases', " +
			"next UInt64 COMMENT 'the lowest body not yet leased', " +
			"holder String COMMENT 'the token of the lease that last moved next'" +
			") ENGINE = MergeTree ORDER BY tag" +
			" SETTINGS index_granularity = 8192" + rowcas.TableSettingsTail +
			" COMMENT 'tagged id stream leases, one row per tag (ADR-0319 §SD3)'",
		rowcas.AlterTableSettingsSQL(layout.Table()),
	}
}

// ProvisionIn creates the lease table; idempotent.
func ProvisionIn(ctx context.Context, exec recordstore.ExecutorI, layout Layout) (err error) {
	if err = layout.validate(); err != nil {
		return
	}
	for _, s := range ProvisionSQL(layout) {
		if err = exec.Exec(ctx, s); err != nil {
			return eb.Build().Str("table", layout.Table()).Errorf("provision lease table: %w", err)
		}
	}
	return
}

var _ identgen.AllocatorI = (*Allocator)(nil)

// Allocator leases body blocks through the lease table. It holds no state of
// its own beyond the executor, so any number of allocators, in any number of
// processes, may share one table and draw disjoint blocks. Safe for
// concurrent use as far as the executor is.
type Allocator struct {
	exec   recordstore.ExecutorI
	layout Layout
	// MaxAttempts bounds the compare-and-sets one AllocateBlock retries after
	// losing a race; zero means [DefaultMaxAttempts].
	MaxAttempts int
	// newToken mints a holder token; a test replaces it.
	newToken func() (token string, err error)
}

// NewAllocator is an allocator over a provisioned lease table (ProvisionIn).
// The executor stays the caller's; Close does not close it.
func NewAllocator(exec recordstore.ExecutorI, layout Layout) (inst *Allocator, err error) {
	if exec == nil {
		err = eh.Errorf("executor is nil")
		return
	}
	if err = layout.validate(); err != nil {
		return
	}
	inst = &Allocator{exec: exec, layout: layout, newToken: randomToken}
	return
}

// lease is the lease row as one read sees it.
type lease struct {
	found  bool
	block  uint64
	offset uint64
	next   uint64
	mine   bool
}

func (inst *Allocator) AllocateBlock(ctx context.Context, tagValue identifier.TagValue, minSize uint64, maxSize uint64) (lo identifier.UntaggedId, hi identifier.UntaggedId, err error) {
	if !tagValue.IsValid() {
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("invalid tag value (zero is reserved)")
		return
	}
	if minSize == 0 || minSize > maxSize {
		err = eb.Build().Uint64("minSize", minSize).Uint64("maxSize", maxSize).Errorf("invalid block size request (need 1 <= minSize <= maxSize)")
		return
	}
	attempts := inst.MaxAttempts
	if attempts <= 0 {
		attempts = DefaultMaxAttempts
	}
	for range attempts {
		if err = ctx.Err(); err != nil {
			return
		}
		var token string
		if token, err = inst.newToken(); err != nil {
			return
		}
		var cur lease
		if cur, err = inst.current(ctx, tagValue, token); err != nil {
			return
		}
		// Body 0 is the invalid sentinel in every stream (ADR-0319 §SD5); a
		// row reads below 1 only if someone wrote it by hand.
		n := max(cur.next, 1)
		room := maxUint64 - n
		if room < minSize {
			err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("cannot reserve id block: %w", identgen.ErrIdSpaceExhausted)
			return
		}
		sz := min(maxSize, room)
		uerr := inst.exec.Exec(ctx, updateSQL(inst.layout, tagValue, cur, n+sz, token))
		var after lease
		after, err = inst.current(ctx, tagValue, token)
		if err != nil {
			if uerr != nil {
				err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("lease update failed (%w) and its read-back too: %w", uerr, err)
			}
			return
		}
		if after.mine && after.next == n+sz {
			lo, hi = identifier.UntaggedId(n), identifier.UntaggedId(n+sz)
			return
		}
		if uerr != nil {
			// The update may yet commit; its block is burnt either way.
			err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("lease update: %w", uerr)
			return
		}
	}
	err = eb.Build().Uint64("tagValue", uint64(tagValue)).Int("attempts", attempts).Errorf("the lease row stayed contended; no block reserved")
	return
}

// current reads the lease row, inserting it at body 1 when the tag has none.
// The insert may race another process's; the row with the lowest block
// position is the lease whichever of them lands first.
func (inst *Allocator) current(ctx context.Context, tagValue identifier.TagValue, token string) (cur lease, err error) {
	if cur, err = inst.read(ctx, tagValue, token); err != nil || cur.found {
		return
	}
	if err = inst.exec.Exec(ctx, insertSQL(inst.layout, tagValue)); err != nil {
		return cur, eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("insert lease row: %w", err)
	}
	if cur, err = inst.read(ctx, tagValue, token); err != nil {
		return
	}
	if !cur.found {
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("the lease row is not there after its insert")
	}
	return
}

func (inst *Allocator) read(ctx context.Context, tagValue identifier.TagValue, token string) (cur lease, err error) {
	var cols struct {
		Block  []uint64 `ch:"b"`
		Offset []uint64 `ch:"o"`
		Next   []uint64 `ch:"n"`
		Mine   []uint8  `ch:"m"`
	}
	for rec, qerr := range inst.exec.QueryArrow(ctx, readSQL(inst.layout, tagValue, token)) {
		if qerr != nil {
			return cur, eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("read lease row: %w", qerr)
		}
		_, err = chrows.Decode(&cols, rec)
		rec.Release()
		if err != nil {
			return cur, eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("decode lease row: %w", err)
		}
	}
	if len(cols.Block) > 0 {
		cur = lease{found: true, block: cols.Block[0], offset: cols.Offset[0], next: cols.Next[0], mine: cols.Mine[0] != 0}
	}
	return
}

func (inst *Allocator) Close() (err error) {
	return
}

func tagLit(tagValue identifier.TagValue) string {
	return strconv.FormatUint(uint64(tagValue), 10)
}

func u64Lit(v uint64) string {
	return strconv.FormatUint(v, 10)
}

// readSQL reads the lease row — the tag's row at the lowest block position —
// and whether token holds it.
func readSQL(layout Layout, tagValue identifier.TagValue, token string) (sql string) {
	return "SELECT _block_number AS b, _block_offset AS o, next AS n, holder = '" + token + "' AS m FROM " + layout.Table() +
		" WHERE tag = " + tagLit(tagValue) + " ORDER BY b ASC, o ASC LIMIT 1"
}

// insertSQL is the lease row's first write. async_insert never sits on the
// claim path (rowcas), so it is off on the statement.
func insertSQL(layout Layout, tagValue identifier.TagValue) (sql string) {
	return "INSERT INTO " + layout.Table() + " (tag, next, holder) SETTINGS async_insert = 0 VALUES (" + tagLit(tagValue) + ", 1, '')"
}

// updateSQL is the compare-and-set: the lease row, pinned by its position and
// guarded on the value the caller read, moves to next and names token.
func updateSQL(layout Layout, tagValue identifier.TagValue, cur lease, next uint64, token string) (sql string) {
	return rowcas.UpdateSQL(layout.Table(),
		[]rowcas.Set{{Column: "next", Expr: u64Lit(next)}, {Column: "holder", Expr: "'" + token + "'"}},
		"tag = "+tagLit(tagValue)+" AND _block_number = "+u64Lit(cur.block)+" AND _block_offset = "+u64Lit(cur.offset)+" AND next = "+u64Lit(cur.next))
}

// randomToken is 128 random bits in hex: unique enough that two leases never
// share one, and nothing a SQL literal needs to escape.
func randomToken() (token string, err error) {
	var b [16]byte
	if _, err = rand.Read(b[:]); err != nil {
		err = eh.Errorf("mint lease token: %w", err)
		return
	}
	token = hex.EncodeToString(b[:])
	return
}
