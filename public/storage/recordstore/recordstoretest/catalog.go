package recordstoretest

import (
	"context"
	"iter"
	"regexp"
	"strings"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

var _ recordstore.ExecutorI = (*CatalogExecutor)(nil)
var _ recordstore.InsertSettingsI = (*CatalogExecutor)(nil)

// DefaultDatabase is the database an unqualified table name and
// currentDatabase() resolve to, as on a fresh ClickHouse server.
const DefaultDatabase = "default"

// CatalogExecutor decorates an executor that does not answer catalog
// reads. It remembers each CREATE TABLE statement Exec is sent (and forgets
// a table on DROP TABLE), and answers the two reads
// recordstore.ReadTableFacts makes — one row of system.tables, the rows of
// system.columns — from what it remembers: engine, partition and sorting
// key, comment and CREATE statement as written, and each column's name,
// type and codec as recordstore.ParseCreateTableColumns reads them. A table
// it was not sent is absent. Every statement, query and insert is also
// forwarded to Inner; a nil Inner accepts them and answers other queries
// with no rows.
//
// It reads only what a generated store's EnsureTable writes. An ALTER TABLE
// does not change what it remembers, a column's default kind is always
// empty, and the spellings are the DDL's, not the server's normalized ones;
// recordstore's judgement compares types and keys normalized, so a store's
// own CREATE TABLE judges ok.
type CatalogExecutor struct {
	inner  recordstore.ExecutorI
	alloc  memory.Allocator
	mu     sync.Mutex
	tables map[string]rememberedTable
}

type rememberedTable struct {
	facts recordstore.TableFacts
}

// NewCatalogExecutor decorates inner (nil for none). alloc allocates the
// answers; nil uses the Go allocator.
func NewCatalogExecutor(inner recordstore.ExecutorI, alloc memory.Allocator) (inst *CatalogExecutor) {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	return &CatalogExecutor{inner: inner, alloc: alloc, tables: map[string]rememberedTable{}}
}

// Exec remembers a CREATE TABLE (a CREATE TABLE IF NOT EXISTS of a table
// it already holds keeps the first) and forgets a dropped table, then
// forwards. A CREATE TABLE it cannot read is an error, not a guess.
func (inst *CatalogExecutor) Exec(ctx context.Context, sql string) (err error) {
	if inst.inner != nil {
		if err = inst.inner.Exec(ctx, sql); err != nil {
			return
		}
	}
	stmt := strings.TrimSpace(sql)
	switch {
	case createTableRe.MatchString(stmt):
		var key string
		var facts recordstore.TableFacts
		var ifNotExists bool
		key, facts, ifNotExists, err = parseCreateTable(stmt)
		if err != nil {
			return
		}
		inst.mu.Lock()
		defer inst.mu.Unlock()
		if _, held := inst.tables[key]; held && ifNotExists {
			return
		}
		inst.tables[key] = rememberedTable{facts: facts}
	case dropTableRe.MatchString(stmt):
		m := dropTableRe.FindStringSubmatch(stmt)
		inst.mu.Lock()
		defer inst.mu.Unlock()
		delete(inst.tables, qualify(m[1]))
	}
	return
}

// QueryArrow answers the two catalog reads and forwards every other query.
func (inst *CatalogExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	if m := tablesQueryRe.FindStringSubmatch(sql); m != nil {
		return inst.answer(m[1], m[2], false)
	}
	if m := columnsQueryRe.FindStringSubmatch(sql); m != nil {
		return inst.answer(m[1], m[2], true)
	}
	if inst.inner != nil {
		return inst.inner.QueryArrow(ctx, sql)
	}
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *CatalogExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	if inst.inner != nil {
		return inst.inner.InsertArrow(ctx, table, records)
	}
	return
}

func (inst *CatalogExecutor) InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, settings recordstore.InsertSettings) (applied bool, err error) {
	if inst.inner != nil {
		return recordstore.InsertArrowWith(ctx, inst.inner, table, records, settings)
	}
	return true, nil
}

// Facts returns what the executor holds for a table reference; ok is false
// for a table it was not sent.
func (inst *CatalogExecutor) Facts(table string) (facts recordstore.TableFacts, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	t, ok := inst.tables[qualify(table)]
	return t.facts, ok
}

func (inst *CatalogExecutor) answer(dbExpr string, nameLit string, columns bool) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		db := DefaultDatabase
		if dbExpr != "currentDatabase()" {
			db = unquoteLiteral(dbExpr)
		}
		key := db + "." + unquoteLiteral(nameLit)
		inst.mu.Lock()
		t, held := inst.tables[key]
		inst.mu.Unlock()
		if !held {
			return
		}
		var rec arrow.RecordBatch
		if columns {
			rows := make([][]string, 0, len(t.facts.Columns))
			for _, c := range t.facts.Columns {
				rows = append(rows, []string{c.Name, c.Type, c.DefaultKind, c.Codec})
			}
			rec = stringRecord(inst.alloc, []string{"name", "type", "default_kind", "compression_codec"}, rows)
		} else {
			f := t.facts
			rec = stringRecord(inst.alloc, []string{"engine", "partition_key", "sorting_key", "comment", "create_table_query"},
				[][]string{{f.Engine, f.PartitionKey, f.SortingKey, f.Comment, f.CreateQuery}})
		}
		yield(rec, nil)
	}
}

func stringRecord(alloc memory.Allocator, names []string, rows [][]string) (rec arrow.RecordBatch) {
	fields := make([]arrow.Field, len(names))
	for i, n := range names {
		fields[i] = arrow.Field{Name: n, Type: arrow.BinaryTypes.String}
	}
	b := array.NewRecordBuilder(alloc, arrow.NewSchema(fields, nil))
	defer b.Release()
	for _, row := range rows {
		for i, v := range row {
			b.Field(i).(*array.StringBuilder).Append(v)
		}
	}
	return b.NewRecordBatch()
}

// The two reads recordstore.ReadTableFacts issues. A change to them fails
// this package's round-trip test.
var (
	literalPat     = `'(?:[^'\\]|\\.)*'`
	dbPat          = `(currentDatabase\(\)|` + literalPat + `)`
	tablesQueryRe  = regexp.MustCompile(`^SELECT engine, partition_key, sorting_key, comment, create_table_query FROM system\.tables WHERE database = ` + dbPat + ` AND name = (` + literalPat + `)`)
	columnsQueryRe = regexp.MustCompile(`^SELECT name, type, default_kind, compression_codec FROM system\.columns WHERE database = ` + dbPat + ` AND table = (` + literalPat + `)`)
	createTableRe  = regexp.MustCompile(`(?is)^CREATE\s+(?:OR\s+REPLACE\s+)?TABLE\s`)
	createHeadRe   = regexp.MustCompile("(?is)^CREATE\\s+(OR\\s+REPLACE\\s+)?TABLE\\s+(IF\\s+NOT\\s+EXISTS\\s+)?([A-Za-z0-9_.`\"]+)\\s*\\(")
	dropTableRe    = regexp.MustCompile("(?is)^DROP\\s+TABLE\\s+(?:IF\\s+EXISTS\\s+)?([A-Za-z0-9_.`\"]+)")
)

// parseCreateTable reads a CREATE TABLE statement into the facts the
// catalog reads return.
func parseCreateTable(stmt string) (key string, facts recordstore.TableFacts, ifNotExists bool, err error) {
	m := createHeadRe.FindStringSubmatchIndex(stmt)
	if m == nil {
		err = eb.Build().Str("statement", firstLine(stmt)).Errorf("catalog executor: cannot read the CREATE TABLE header")
		return
	}
	ifNotExists = m[4] >= 0
	key = qualify(stmt[m[6]:m[7]])
	open := m[1] - 1
	end, ok := matchingParen(stmt, open)
	if !ok {
		err = eb.Build().Str("statement", firstLine(stmt)).Errorf("catalog executor: the column list is not closed")
		return
	}
	var cols []recordstore.ColumnSpec
	cols, err = recordstore.ParseCreateTableColumns(stmt)
	if err != nil {
		err = eh.Errorf("catalog executor: %w", err)
		return
	}
	tail := stmt[end+1:]
	comment := ""
	if i := strings.LastIndex(tail, "COMMENT '"); i >= 0 {
		comment = unquoteLiteral(strings.TrimSpace(tail[i+len("COMMENT "):]))
		tail = tail[:i]
	}
	var layout recordstore.TableLayout
	layout, _, err = recordstore.ParseEngineClause(tail)
	if err != nil {
		err = eh.Errorf("catalog executor: %w", err)
		return
	}
	facts = recordstore.TableFacts{
		Exists:       true,
		Engine:       layout.Engine,
		PartitionKey: layout.PartitionKey,
		SortingKey:   layout.SortingKey,
		Comment:      comment,
		CreateQuery:  stmt,
	}
	for _, c := range cols {
		facts.Columns = append(facts.Columns, recordstore.LiveColumn{Name: c.Name, Type: c.Type, Codec: c.Codec})
	}
	return
}

// qualify is a table reference with quotes removed and the default
// database filled in.
func qualify(ref string) (key string) {
	ref = strings.NewReplacer("`", "", `"`, "").Replace(ref)
	if strings.Contains(ref, ".") {
		return ref
	}
	return DefaultDatabase + "." + ref
}

// matchingParen is the index of the parenthesis closing the one at open,
// outside quotes.
func matchingParen(s string, open int) (end int, ok bool) {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return
}

// unquoteLiteral reads a single-quoted SQL string literal with backslash
// escapes; text after the closing quote is ignored.
func unquoteLiteral(lit string) (s string) {
	if len(lit) < 2 || lit[0] != '\'' {
		return lit
	}
	var b strings.Builder
	for i := 1; i < len(lit); i++ {
		ch := lit[i]
		if ch == '\\' && i+1 < len(lit) {
			i++
			b.WriteByte(lit[i])
			continue
		}
		if ch == '\'' {
			break
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func firstLine(s string) (l string) {
	l, _, _ = strings.Cut(s, "\n")
	return
}
