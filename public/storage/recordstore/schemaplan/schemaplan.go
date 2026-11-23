// Package schemaplan derives the statements that bring a stored table to
// the columns its store was generated for, when the change is mechanical
// (ADR-0387): generated columns the table lacks are added, columns present
// under an aspect-only variant of their name (ADR-0182) are renamed and
// given the generated codec, a plain MergeTree table without an insert
// deduplication window gets one, and the table is stamped with the
// generating schema. Anything else is refused with the reason; doc/migration
// holds the recipes for those.
//
// A table that does not exist is planned from its declaration
// (recordstore.StoredTable): the statements that create it, when the
// declaration can.
//
// It only derives. `boxer schema plan` prints a plan, `boxer schema check`
// answers whether every table is in line, and `boxer schema apply` executes
// the kinds of statement it is told to, --create and --alter; no host
// applies a plan on its own.
package schemaplan

import (
	"context"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// MigrationDocs is where a refused plan points.
const MigrationDocs = "doc/migration/README.md"

// Plan is the outcome for one table.
type Plan struct {
	// Owner is the subsystem that declared the table; empty for a plan made
	// from a bare contract.
	Owner   string                    `json:"owner,omitempty"`
	Verdict recordstore.SchemaVerdict `json:"verdict"`
	// Creates make a table that does not exist, and its database, in order;
	// set only for an absent table whose declaration can create it.
	Creates []string `json:"creates,omitempty"`
	// Statements bring the table in line, in order; empty when nothing is
	// to do or the plan is refused.
	Statements []string `json:"statements,omitempty"`
	// Refused says why no statements are offered for a shape change;
	// statements that only add the window or the stamp may still be.
	Refused []string `json:"refused,omitempty"`
}

// IsRefused reports whether the plan declines a change the table needs.
func (inst Plan) IsRefused() bool { return len(inst.Refused) > 0 }

// OutcomeE is where a plan leaves its table.
type OutcomeE string

const (
	// OutcomeInSync: the table is as this build reads and writes it, and
	// the plan holds no statement.
	OutcomeInSync OutcomeE = "in-sync"
	// OutcomePending: the plan's statements bring the table in line; nothing
	// is refused.
	OutcomePending OutcomeE = "pending"
	// OutcomeBlocked: the table needs a change the plan declines to make.
	OutcomeBlocked OutcomeE = "blocked"
	// OutcomeUnchecked: the table could not be read, so nothing is known.
	OutcomeUnchecked OutcomeE = "unchecked"
)

// Outcome classifies the plan: unchecked when the table could not be read,
// blocked when a needed change is refused, pending when statements remain,
// and in sync otherwise.
func (inst Plan) Outcome() (o OutcomeE) {
	switch {
	case inst.Verdict.Verdict == recordstore.SchemaVerdictUnchecked:
		o = OutcomeUnchecked
	case inst.IsRefused():
		o = OutcomeBlocked
	case len(inst.Creates) > 0 || len(inst.Statements) > 0:
		o = OutcomePending
	case inst.Verdict.IsOk():
		o = OutcomeInSync
	default:
		// A verdict that is not ok with nothing offered and nothing refused
		// is a change nobody planned.
		o = OutcomeBlocked
	}
	return
}

// Derive plans c's table from facts.
func Derive(c recordstore.TableContract, facts recordstore.TableFacts) (p Plan) {
	p.Verdict = recordstore.JudgeFacts(c, facts)
	v := p.Verdict
	if c.ColumnsOnly {
		// A relation judged by its columns is not boxer's to change
		// (ADR-0402 §SD4): a view or a deployment's storage gets no
		// statement, whatever its verdict.
		if !v.IsOk() {
			p.Refused = append(p.Refused, "the relation is read, not owned: it returns the facts columns from storage this build does not manage, so bringing it in line is a change to that storage or its view; see "+MigrationDocs)
		}
		return
	}
	switch v.Verdict {
	case recordstore.SchemaVerdictAbsent:
		p.Refused = append(p.Refused, "the table does not exist; the store that owns it creates it when it starts")
		return
	case recordstore.SchemaVerdictUnchecked:
		p.Refused = append(p.Refused, v.Reasons...)
		return
	case recordstore.SchemaVerdictIncompatible:
		for _, r := range v.Reasons {
			p.Refused = append(p.Refused, r+" — not a mechanical change; see "+MigrationDocs)
		}
		return
	}
	ref := quoteRef(c.Table)
	byName := make(map[string]recordstore.ColumnSpec, len(c.Columns))
	for _, col := range c.Columns {
		byName[col.Name] = col
	}
	for _, r := range v.Renames {
		if referencedElsewhere(facts.CreateQuery, r.From) {
			p.Refused = append(p.Refused, "column "+r.From+" would be renamed to "+r.To+", but a key, index, TTL or another column's expression names it, and ClickHouse renames no such column; see "+MigrationDocs)
			continue
		}
		// The codec first, under the old name, then the rename: the server
		// refuses both on one column in one ALTER, and a failure between two
		// statements in the other order leaves a table that judges ok — codecs
		// are not compared — and would never get its codec. In this order a
		// rerun still sees the rename and proposes both again.
		col := byName[r.To]
		if col.Codec != "" {
			p.Statements = append(p.Statements, "ALTER TABLE "+ref+" MODIFY COLUMN "+jackstay.QuoteIdent(r.From)+" "+col.Type+" CODEC("+col.Codec+")"+typeSettings(col.Type))
		}
		p.Statements = append(p.Statements, "ALTER TABLE "+ref+" RENAME COLUMN "+jackstay.QuoteIdent(r.From)+" TO "+jackstay.QuoteIdent(r.To))
	}
	if p.IsRefused() {
		p.Statements = nil
		return
	}
	missing := make(map[string]bool, len(v.Missing))
	for _, m := range v.Missing {
		missing[m] = true
	}
	prev := ""
	for _, col := range c.Columns {
		if missing[col.Name] {
			p.Statements = append(p.Statements, addColumn(c.Table, col, prev)+typeSettings(col.Type))
		}
		prev = col.Name
	}
	if recordstore.IsPlainMergeTree(facts.Engine) && !v.DedupWindow && !c.NoDedupWindow {
		p.Statements = append(p.Statements, "ALTER TABLE "+ref+" MODIFY SETTING "+recordstore.DedupWindowSetting+" = "+strconv.Itoa(recordstore.DedupWindowBlocks))
	}
	if c.Stamp != "" && facts.Comment != c.Stamp && len(v.Extra) > 0 {
		// Columns this build does not know: a newer build may have written
		// the table, and this build's stamp would claim an older schema did.
		p.Verdict.Notes = append(p.Verdict.Notes, "the table has columns this build does not know, so its stamp is left as it is")
	} else if c.Stamp != "" && facts.Comment != c.Stamp && v.LayoutDiffers {
		// The build's stamp records the build's layout, which this table
		// does not have (ADR-0397).
		p.Verdict.Notes = append(p.Verdict.Notes, "the table is not stamped: the stamp would record a layout the table does not have")
	} else if c.Stamp != "" && facts.Comment != c.Stamp {
		if _, stamped := recordstore.ParseTableStamp(facts.Comment); stamped || strings.TrimSpace(facts.Comment) == "" {
			p.Statements = append(p.Statements, "ALTER TABLE "+ref+" MODIFY COMMENT "+jackstay.QuoteString(c.Stamp))
		} else {
			p.Verdict.Notes = append(p.Verdict.Notes, "the table carries a comment no store wrote; it is left as it is, and the table stays unstamped")
		}
	}
	return
}

// PlanTable reads c's table through exec and derives its plan.
func PlanTable(ctx context.Context, exec recordstore.ExecutorI, c recordstore.TableContract) (p Plan) {
	facts, err := recordstore.ReadTableFacts(ctx, exec, c.Table)
	if err != nil {
		p.Verdict = recordstore.SchemaVerdict{Kind: c.Kind, Table: c.Table, Verdict: recordstore.SchemaVerdictUnchecked, Reasons: []string{err.Error()}}
		p.Refused = append(p.Refused, err.Error())
		return
	}
	return Derive(c, facts)
}

// PlanStored reads t's table through exec and derives its plan. For a table
// that does not exist and that t can create, the plan holds the creating
// statements in place of the refusal a bare contract gets.
func PlanStored(ctx context.Context, exec recordstore.ExecutorI, t recordstore.StoredTable) (p Plan) {
	p = PlanTable(ctx, exec, t.Contract)
	p.Owner = t.Owner
	if p.Verdict.Verdict != recordstore.SchemaVerdictAbsent || t.Provision == nil || t.Contract.ColumnsOnly {
		return
	}
	creates, err := recordstore.ProvisioningSQL(ctx, t.Provision)
	if err != nil {
		p.Refused = []string{"the table does not exist, and the statements that create it cannot be listed: " + err.Error()}
		return
	}
	p.Refused, p.Creates = nil, creates
	return
}

// ApplyCreates executes a plan's creating statements in order and stops at
// the first that fails. They create what is missing and change nothing that
// exists, so running them again is harmless.
func ApplyCreates(ctx context.Context, exec recordstore.ExecutorI, p Plan) (applied int, err error) {
	for _, stmt := range p.Creates {
		if err = exec.Exec(ctx, stmt); err != nil {
			err = eb.Build().Str("table", p.Verdict.Table).Int("statement", applied+1).Str("sql", stmt).Errorf("schema apply: create: %w", err)
			return
		}
		applied++
	}
	return
}

// Apply executes a plan's altering statements in order and stops at the
// first that fails. A refused plan's statements are not run.
func Apply(ctx context.Context, exec recordstore.ExecutorI, p Plan) (applied int, err error) {
	if p.IsRefused() {
		return
	}
	for _, stmt := range p.Statements {
		if err = exec.Exec(ctx, stmt); err != nil {
			err = eb.Build().Str("table", p.Verdict.Table).Int("statement", applied+1).Str("sql", stmt).Errorf("schema apply: %w", err)
			return
		}
		applied++
	}
	return
}

// addColumn renders one ADD COLUMN through jackstay's renderer, placed
// after the generated column before it, or first.
func addColumn(table string, col recordstore.ColumnSpec, after string) string {
	info := jackstay.ColumnInfo{Name: col.Name, Type: col.Type}
	if col.Codec != "" {
		info.CompressionCodec = "CODEC(" + col.Codec + ")"
	}
	db, name := recordstore.SplitTableRef(table)
	if db == "" {
		// jackstay always qualifies; a bare reference stays bare.
		sql := jackstay.AddColumnDDL(datacatalog.TableRef{Database: "_", Name: name}, info, after)
		return strings.Replace(sql, jackstay.QuoteRef(datacatalog.TableRef{Database: "_", Name: name}), jackstay.QuoteIdent(name), 1)
	}
	return jackstay.AddColumnDDL(datacatalog.TableRef{Database: db, Name: name}, info, after)
}

func quoteRef(table string) string {
	db, name := recordstore.SplitTableRef(table)
	if db == "" {
		return jackstay.QuoteIdent(name)
	}
	return jackstay.QuoteRef(datacatalog.TableRef{Database: db, Name: name})
}

// typeSettings is the statement-level setting a column of typ needs: the
// generated tables carry LowCardinality over numbers, which ClickHouse
// creates only when asked, and a table's own setting does not reach an
// ALTER.
func typeSettings(typ string) string {
	if strings.Contains(typ, "LowCardinality(") {
		return " SETTINGS allow_suspicious_low_cardinality_types = 1"
	}
	return ""
}

// referencedElsewhere reports whether the CREATE statement names column
// beyond its own definition: in a sorting, primary or partition key, a
// skip index, a projection, a TTL or another column's expression.
func referencedElsewhere(createQuery string, column string) bool {
	n := strings.Count(createQuery, "`"+column+"`") + strings.Count(createQuery, `"`+column+`"`)
	return n > 1
}
