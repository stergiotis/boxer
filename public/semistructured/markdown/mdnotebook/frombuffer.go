package mdnotebook

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultSink is the name [FromBuffer] gives the buffer's own SELECT when
// its caller names none: what play calls the statement its panels draw.
const DefaultSink = "main"

// FromBuffer is the notebook of a play buffer: [Buffer] read the other way
// (ADR-0371 §SD6). Every query of the statement's WITH list is a record cell
// under its name, in the list's order, and the statement's own SELECT is the
// last record, named sink ([DefaultSink] when empty). One table cell over
// that record follows: what the buffer shows when it is run.
//
// The notebook is not sealed: it lists no stored result until it is
// collected. [Buffer] of it is the buffer again, statement for statement.
//
// A buffer no notebook can hold is an error that says which part: a SET
// before the statement and a `{name:Type}` slot, because a notebook holds
// no parameters; a WITH item that is an expression, because a record is a
// relation; WITH RECURSIVE and a FORMAT, as for a record written by hand.
func FromBuffer(sql string, sink string) (text []byte, err error) {
	records, err := bufferRecords(sql, sink)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, r := range records {
		b.WriteString(recordCellText(r.name, r.statement))
		b.WriteString("\n")
	}
	b.WriteString("```sql " + RendererTable + "\nSELECT * FROM " + records[len(records)-1].name + "\n```\n")
	return []byte(b.String()), nil
}

// bufferRecord is one part of a buffer read as a notebook's records.
type bufferRecord struct{ name, statement string }

// recordCellText is a record cell as [FromBuffer] and [SetRecords] write
// one: the statement in a fence it cannot close, and its id on the next
// line. A block id holds no underscore; the reader writes it back as one.
func recordCellText(name, statement string) (text string) {
	fence := fenceFor(statement, '`', 3)
	return fence + "sql\n" + statement + "\n" + fence + "\n^" + strings.ReplaceAll(name, "_", "-") + "\n"
}

// bufferRecords takes a buffer apart: each query of its WITH list, then its
// own SELECT under the name sink ([DefaultSink] when empty). Each part is a
// record as one written by hand is, checked here, so what is made of the
// parts is a notebook collection takes.
func bufferRecords(sql string, sink string) (records []bufferRecord, err error) {
	if sink == "" {
		sink = DefaultSink
	}
	if !reIdent.MatchString(sink) {
		return nil, eb.Build().Str("sink", sink).Errorf("the name for the buffer's own SELECT does not make a SQL name")
	}
	src := statementText(sql)
	if src == "" {
		return nil, eh.Errorf("the buffer is empty")
	}
	pr, pErr := nanopass.Parse(src)
	if pErr != nil {
		return nil, eh.Errorf("the buffer does not parse: %w", pErr)
	}
	fail := func(what string) ([]bufferRecord, error) {
		return nil, eh.Errorf("the buffer is not a notebook's: %s", what)
	}
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok || qs.Query() == nil {
		return fail("it is not one SELECT")
	}
	if qs.FORMAT() != nil {
		return fail("it names a FORMAT, which collection chooses")
	}
	q := qs.Query().(*grammar1.QueryContext)
	if len(q.AllSetStmt()) > 0 {
		return fail("it starts with SET, and a notebook holds no parameters")
	}
	su, ok := q.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
	if !ok || su == nil {
		return fail("it is not one SELECT")
	}
	if slots := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, isSlot := ctx.(*grammar1.ParamSlotContext)
		return isSlot
	}); len(slots) > 0 {
		return fail("it reads the parameter " + slots[0].GetText() + ", and a notebook holds no parameters")
	}

	seen := map[string]struct{}{}
	own := src
	if c := su.Ctes(); c != nil {
		cc := c.(*grammar1.CtesContext)
		if cc.RECURSIVE() != nil {
			return fail("its WITH list is RECURSIVE, and a record is computed after the records before it")
		}
		for _, wi := range cc.AllWithItem() {
			nq, isQuery := wi.(*grammar1.WithItemNamedQueryContext)
			if !isQuery {
				return fail("its WITH list holds an expression (" + wi.GetText() + "), and a record is a relation: write the expression into the queries that read it")
			}
			named := nq.NamedQuery().(*grammar1.NamedQueryContext)
			name := nanopass.DecodeIdentifier(named.Identifier().GetText())
			if named.ColumnAliases() != nil {
				return fail("the WITH query " + name + " renames its columns in the list; name them in its SELECT")
			}
			if !reIdent.MatchString(name) {
				return fail("the WITH query " + named.Identifier().GetText() + " has a name a record cannot take")
			}
			if _, dup := seen[name]; dup {
				return fail("its WITH list names " + name + " twice")
			}
			seen[name] = struct{}{}
			r := pr.SourceRangeOf(named.Query())
			if r.Empty() {
				return fail("the WITH query " + name + " is empty")
			}
			records = append(records, bufferRecord{name: name, statement: strings.TrimSpace(pr.Source[r.Start:r.End])})
		}
		r := pr.SourceRangeOf(cc)
		if r.Empty() {
			return fail("its WITH list is empty")
		}
		own = strings.TrimSpace(pr.Source[r.End:])
	}
	if _, taken := seen[sink]; taken {
		return nil, eb.Build().Str("sink", sink).Errorf("the buffer has a WITH query of the name its own SELECT was to take; give the SELECT another")
	}
	records = append(records, bufferRecord{name: sink, statement: own})
	for _, r := range records {
		if _, rErr := readRecordStatement(&Cell{Name: r.name, Body: r.statement}); rErr != nil {
			return nil, rErr
		}
	}
	return records, nil
}

// fenceFor is a fence of ch that body cannot close: one longer than the
// longest run of ch in it, and least at least.
func fenceFor(body string, ch byte, least int) (fence string) {
	longest, run := 0, 0
	for i := 0; i < len(body); i++ {
		if body[i] == ch {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat(string(ch), max(least, longest+1))
}
