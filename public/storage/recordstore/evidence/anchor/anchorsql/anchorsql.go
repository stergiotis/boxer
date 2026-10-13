// Package anchorsql is a durable anchor store (ADR-0314 §SD2, by scope
// since ADR-0327 §SD7): the anchor.StoreI contract over one append-only
// ClickHouse table, for an organisation that runs its anchor store on a
// ClickHouse server outside any person's control (ADR-0314, update of
// 2026-10-13).
//
// The table is the store's own and plain — one row per receipt, no leeway
// encoding — since it is the organisation's record, read by its tools as
// much as by boxer. Rows are inserted and never rewritten. The contract's
// "first receipt wins" is kept across writers that share the table: an
// append inserts its receipt, reads the key back, and answers with the
// earliest receipt there, so two hosts racing on one ordinal both come
// away with the same receipt, or the later one with a refusal when its
// digest differs. Its receipts say Durable: whether the server is in fact
// outside the person's control is the deployment's statement, made by
// configuring it, and not something a client can check.
package anchorsql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// DefaultTable is the table a store uses when none is named.
const DefaultTable = "boxer.anchorhead"

// MaxAll bounds what All lists: the newest receipts first.
const MaxAll = 10000

// allTimeout bounds one All, which the introspection table calls without
// a context.
const allTimeout = 10 * time.Second

// Store is anchor.StoreI over a ClickHouse table. Create it with New and
// provision the table once with Provision.
type Store struct {
	exec  recordstore.ExecutorI
	table string
	by    string
}

var _ anchor.StoreI = (*Store)(nil)

// New is a store over table (DefaultTable when empty) through exec; by is
// the sender its receipts name.
func New(exec recordstore.ExecutorI, table string, by string) (inst *Store) {
	if table == "" {
		table = DefaultTable
	}
	return &Store{exec: exec, table: table, by: by}
}

// Provision creates the table, and its database, where absent.
func (inst *Store) Provision(ctx context.Context) (err error) {
	if db, _, ok := strings.Cut(inst.table, "."); ok {
		if err = inst.exec.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+db); err != nil {
			return eb.Build().Str("table", inst.table).Errorf("anchorsql: create database: %w", err)
		}
	}
	err = inst.exec.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+inst.table+` (
	cell String,
	scope UInt64,
	ordinal UInt64,
	scheme String,
	digest String,
	receipt_id String,
	received_at DateTime64(9, 'UTC'),
	received_by String
) ENGINE = MergeTree ORDER BY (cell, scope, ordinal, scheme, received_at, receipt_id)`)
	if err != nil {
		return eb.Build().Str("table", inst.table).Errorf("anchorsql: create table: %w", err)
	}
	return
}

var _ anchor.SenderI = (*Store)(nil)

// Append ships h under the store's own sender.
func (inst *Store) Append(ctx context.Context, h anchor.Head) (r anchor.Receipt, err error) {
	return inst.AppendFor(ctx, h, inst.by)
}

// AppendFor ships h for the sender by. A head already received for its
// ordinal under its scheme answers with that first receipt, whoever sent
// it; a different digest there is refused.
func (inst *Store) AppendFor(ctx context.Context, h anchor.Head, by string) (r anchor.Receipt, err error) {
	first, found, err := inst.first(ctx, h)
	if err != nil {
		return
	}
	if found {
		return held(h, first)
	}
	var b [12]byte
	if _, err = rand.Read(b[:]); err != nil {
		return r, eh.Errorf("anchorsql: mint a receipt id: %w", err)
	}
	r = anchor.Receipt{Id: "anchor-" + hex.EncodeToString(b[:]), At: anchor.Now(), By: by, Durable: true}
	err = inst.exec.Exec(ctx, "INSERT INTO "+inst.table+" (cell, scope, ordinal, scheme, digest, receipt_id, received_at, received_by) VALUES ("+
		marshalling.EscapeString(h.Cell)+", "+strconv.FormatUint(h.Scope, 10)+", "+strconv.FormatUint(h.Ordinal, 10)+", "+
		marshalling.EscapeString(h.Scheme)+", "+marshalling.EscapeString(h.Digest)+", "+marshalling.EscapeString(r.Id)+", "+
		"fromUnixTimestamp64Nano("+strconv.FormatInt(r.At.UnixNano(), 10)+", 'UTC'), "+marshalling.EscapeString(r.By)+")")
	if err != nil {
		return anchor.Receipt{}, eb.Build().Str("table", inst.table).Errorf("anchorsql: insert the head: %w", err)
	}
	// Another writer may have received the same ordinal meanwhile: the
	// earliest receipt is the one that counts.
	first, found, err = inst.first(ctx, h)
	if err != nil {
		return anchor.Receipt{}, err
	}
	if !found {
		return anchor.Receipt{}, eb.Build().Str("table", inst.table).Errorf("anchorsql: the head does not read back")
	}
	return held(h, first)
}

func held(h anchor.Head, first anchor.Anchored) (r anchor.Receipt, err error) {
	if first.Digest != h.Digest {
		return r, eb.Build().Uint64("scope", h.Scope).Uint64("ordinal", h.Ordinal).Str("scheme", h.Scheme).Errorf("another digest is anchored at the ordinal")
	}
	return first.Receipt, nil
}

const columns = "cell, scope, ordinal, scheme, digest, receipt_id, toUnixTimestamp64Nano(received_at) AS at_ns, received_by"

// first is the earliest receipt for h's ordinal under its scheme.
func (inst *Store) first(ctx context.Context, h anchor.Head) (a anchor.Anchored, found bool, err error) {
	rows, err := inst.query(ctx, "SELECT "+columns+" FROM "+inst.table+
		" WHERE cell = "+marshalling.EscapeString(h.Cell)+" AND scope = "+strconv.FormatUint(h.Scope, 10)+
		" AND ordinal = "+strconv.FormatUint(h.Ordinal, 10)+" AND scheme = "+marshalling.EscapeString(h.Scheme)+
		" ORDER BY received_at, receipt_id LIMIT 1")
	if err != nil || len(rows) == 0 {
		return
	}
	return rows[0], true, nil
}

// Read is every head received for (cell, scope), the earliest receipt
// per ordinal and scheme, in ordinal order.
func (inst *Store) Read(ctx context.Context, cell string, scope uint64) (heads []anchor.Anchored, err error) {
	return inst.query(ctx, "SELECT "+columns+" FROM "+inst.table+
		" WHERE cell = "+marshalling.EscapeString(cell)+" AND scope = "+strconv.FormatUint(scope, 10)+
		" ORDER BY ordinal, scheme, received_at, receipt_id LIMIT 1 BY ordinal, scheme")
}

// All is the newest MaxAll receipts that count, oldest first: what
// keelson('anchors') lists. A read that fails lists nothing.
func (inst *Store) All() (rows []anchor.Anchored) {
	ctx, cancel := context.WithTimeout(context.Background(), allTimeout)
	defer cancel()
	rows, err := inst.query(ctx, "SELECT * FROM (SELECT "+columns+" FROM "+inst.table+
		" ORDER BY cell, scope, ordinal, scheme, received_at, receipt_id LIMIT 1 BY cell, scope, ordinal, scheme)"+
		" ORDER BY at_ns DESC LIMIT "+strconv.Itoa(MaxAll))
	if err != nil {
		return nil
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return
}

func (inst *Store) query(ctx context.Context, sql string) (rows []anchor.Anchored, err error) {
	for rec, qerr := range inst.exec.QueryArrow(ctx, sql) {
		if qerr != nil {
			return nil, eb.Build().Str("table", inst.table).Errorf("anchorsql: read: %w", qerr)
		}
		rows, err = appendRows(rows, rec)
		if err != nil {
			return nil, err
		}
	}
	return
}

func appendRows(rows []anchor.Anchored, rec arrow.RecordBatch) (out []anchor.Anchored, err error) {
	out = rows
	str := func(c int, i int) string {
		switch col := rec.Column(c).(type) {
		case *array.String:
			return col.Value(i)
		case *array.Binary:
			return string(col.Value(i))
		case *array.LargeString:
			return col.Value(i)
		}
		return ""
	}
	u64 := func(c int, i int) uint64 {
		if col, ok := rec.Column(c).(*array.Uint64); ok {
			return col.Value(i)
		}
		return 0
	}
	if rec.NumCols() != 8 {
		return nil, eb.Build().Int64("columns", rec.NumCols()).Errorf("anchorsql: unexpected columns")
	}
	ns, ok := rec.Column(6).(*array.Int64)
	if !ok {
		return nil, eb.Build().Str("type", rec.Column(6).DataType().String()).Errorf("anchorsql: unexpected receipt time column")
	}
	for i := 0; i < int(rec.NumRows()); i++ {
		out = append(out, anchor.Anchored{
			Head:    anchor.Head{Cell: str(0, i), Scope: u64(1, i), Ordinal: u64(2, i), Scheme: str(3, i), Digest: str(4, i)},
			Receipt: anchor.Receipt{Id: str(5, i), At: time.Unix(0, ns.Value(i)).UTC(), By: str(7, i), Durable: true},
		})
	}
	return
}
