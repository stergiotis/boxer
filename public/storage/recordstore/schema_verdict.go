package recordstore

import (
	"context"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SchemaVerdictE is what a binary concludes about a stored table it uses,
// against the columns its code was generated for (ADR-0387).
type SchemaVerdictE uint8

const (
	// SchemaVerdictUnchecked: the table's shape could not be read.
	SchemaVerdictUnchecked SchemaVerdictE = iota
	// SchemaVerdictOk: every generated column is present by name with a
	// matching type. Columns the store does not know may be present too;
	// they are listed, and a by-name read ignores them.
	SchemaVerdictOk
	// SchemaVerdictMigratable: generated columns are missing, and each is
	// either absent (ADD COLUMN brings it in) or present under a name that
	// differs only in leeway aspects (RENAME COLUMN). `boxer schema plan`
	// derives the statements.
	SchemaVerdictMigratable
	// SchemaVerdictIncompatible: a column the store reads has another type,
	// or is computed rather than stored. Nothing mechanical repairs it.
	SchemaVerdictIncompatible
	// SchemaVerdictAbsent: the table does not exist.
	SchemaVerdictAbsent
)

var AllSchemaVerdicts = []SchemaVerdictE{SchemaVerdictUnchecked, SchemaVerdictOk, SchemaVerdictMigratable, SchemaVerdictIncompatible, SchemaVerdictAbsent}

func (inst SchemaVerdictE) String() string {
	switch inst {
	case SchemaVerdictUnchecked:
		return "unchecked"
	case SchemaVerdictOk:
		return "ok"
	case SchemaVerdictMigratable:
		return "migratable"
	case SchemaVerdictIncompatible:
		return "incompatible"
	case SchemaVerdictAbsent:
		return "absent"
	}
	return "invalid"
}

// StampAgeE relates a table's COMMENT stamp to the one this build writes.
type StampAgeE uint8

const (
	// StampAgeUnstamped: no stamp; the table is judged by its columns alone.
	StampAgeUnstamped StampAgeE = iota
	// StampAgeSame: the stamp's column digest is this build's.
	StampAgeSame
	// StampAgeOlder: another digest, and the table lacks columns this build
	// declares — written by an older schema.
	StampAgeOlder
	// StampAgeNewer: another digest, and the table carries columns this build
	// does not declare and lacks none — written by a newer schema.
	StampAgeNewer
	// StampAgeOther: another digest that neither direction explains.
	StampAgeOther
)

func (inst StampAgeE) String() string {
	switch inst {
	case StampAgeUnstamped:
		return "unstamped"
	case StampAgeSame:
		return "same"
	case StampAgeOlder:
		return "older"
	case StampAgeNewer:
		return "newer"
	case StampAgeOther:
		return "other"
	}
	return "invalid"
}

// TableContract is what a store declares about the table it binds: enough
// to judge a live table without the store itself.
type TableContract struct {
	// Kind names the table's schema (the generated table name), the same
	// for every table a store is pointed at.
	Kind string
	// Table is the reference the store reads, optionally database-qualified.
	Table string
	// Columns are the generated columns, in DDL order.
	Columns []ColumnSpec
	// Stamp is the COMMENT the store's CREATE TABLE writes; empty when its
	// creator does not stamp.
	Stamp string
	// Layout is the engine, partition key and sorting key the store creates
	// the table with; zero when the contract does not declare one. A table
	// with another layout is still judged by its columns: the difference is
	// a note, since none of the three can change in place (ADR-0397).
	Layout TableLayout
	// LayoutRecipe is where a note on a differing layout points: the
	// doc/migration recipe that copies the table into the declared layout.
	LayoutRecipe string
	// ColumnsOnly judges Table as a relation the store reads but does not
	// own (ADR-0402 §SD4) — a view over a deployment's own table, a Merge
	// over storage tables: by its columns' names and types alone. Its stamp,
	// layout and deduplication window are not looked at, since a view has
	// none of them, and a plan proposes no statement for it.
	ColumnsOnly bool
}

// ColumnRename is a column present under a name that differs from the
// generated one only in leeway aspects (ADR-0182).
type ColumnRename struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// LiveColumn is one column of a live table as system.columns reports it.
type LiveColumn struct {
	Name        string
	Type        string
	DefaultKind string
	Codec       string
}

// TableFacts is what the server says about one table.
type TableFacts struct {
	Exists       bool
	Engine       string
	PartitionKey string
	SortingKey   string
	Comment      string
	CreateQuery  string
	Columns      []LiveColumn
}

// SchemaVerdict is the judgement of one table.
type SchemaVerdict struct {
	Kind    string         `json:"kind"`
	Table   string         `json:"table"`
	Verdict SchemaVerdictE `json:"verdict"`
	// Missing are generated columns the table lacks (ADD COLUMN).
	Missing []string `json:"missing,omitempty"`
	// Renames are generated columns present under an aspect-only variant
	// of their name.
	Renames []ColumnRename `json:"renames,omitempty"`
	// Extra are live columns the store does not declare.
	Extra []string `json:"extra,omitempty"`
	// Reasons explain an incompatible or unchecked verdict.
	Reasons []string `json:"reasons,omitempty"`
	// Notes are facts an operator should see whatever the verdict.
	Notes []string `json:"notes,omitempty"`
	// Stamp is the table's COMMENT; Age relates it to the build's.
	Stamp string    `json:"stamp,omitempty"`
	Age   StampAgeE `json:"age"`
	// Engine is the table's engine; DedupWindow is whether its settings
	// keep a non-replicated insert deduplication window, without which an
	// insert's deduplication token is ignored.
	Engine      string `json:"engine,omitempty"`
	DedupWindow bool   `json:"dedupWindow"`
	// LayoutDiffers is set when the contract declares a layout and the
	// table has another. It is informational and leaves the verdict as it
	// is: the store reads and writes either layout (ADR-0397).
	LayoutDiffers bool `json:"layoutDiffers,omitempty"`
}

// IsOk reports whether the store reads and writes the table as it is.
func (inst SchemaVerdict) IsOk() bool { return inst.Verdict == SchemaVerdictOk }

// Summary is the verdict as one line for an operator.
func (inst SchemaVerdict) Summary() string {
	var b strings.Builder
	b.WriteString(inst.Table)
	b.WriteString(": ")
	b.WriteString(inst.Verdict.String())
	switch inst.Verdict {
	case SchemaVerdictMigratable:
		if n := len(inst.Missing); n > 0 {
			b.WriteString(" — " + strconv.Itoa(n) + " column(s) missing (" + abbreviate(inst.Missing, 3) + ")")
		}
		if n := len(inst.Renames); n > 0 {
			b.WriteString(" — " + strconv.Itoa(n) + " column(s) under an older aspect spelling")
		}
		b.WriteString("; `boxer schema plan` lists the statements")
	case SchemaVerdictIncompatible, SchemaVerdictUnchecked:
		if len(inst.Reasons) > 0 {
			b.WriteString(" — " + strings.Join(inst.Reasons, "; "))
		}
		if inst.Verdict == SchemaVerdictIncompatible {
			b.WriteString("; see doc/migration/")
		}
	case SchemaVerdictOk:
		if n := len(inst.Extra); n > 0 {
			b.WriteString(" — " + strconv.Itoa(n) + " column(s) the store does not read")
		}
	}
	switch inst.Age {
	case StampAgeOlder:
		b.WriteString(" (written by an older schema)")
	case StampAgeNewer:
		b.WriteString(" (written by a newer schema)")
	case StampAgeOther:
		b.WriteString(" (stamped by another schema)")
	}
	if inst.LayoutDiffers {
		b.WriteString(" — layout differs (informational)")
	}
	return b.String()
}

// Err is nil for an ok verdict and otherwise an error carrying the summary.
func (inst SchemaVerdict) Err() (err error) {
	if inst.IsOk() {
		return
	}
	b := eb.Build().Str("table", inst.Table).Stringer("verdict", inst.Verdict).Str("summary", inst.Summary())
	if len(inst.Missing) > 0 {
		b = b.Strs("missing", inst.Missing)
	}
	if len(inst.Reasons) > 0 {
		b = b.Strs("reasons", inst.Reasons)
	}
	err = b.Errorf("schema: the table's verdict is not ok; `boxer schema plan` shows it and what is mechanical")
	return
}

// JudgeColumns compares a live column list with the generated one. It is
// the pure half of [JudgeTable]; an empty live list is an absent table.
func JudgeColumns(want []ColumnSpec, live []LiveColumn) (v SchemaVerdict) {
	if len(live) == 0 {
		v.Verdict = SchemaVerdictAbsent
		return
	}
	liveBy := make(map[string]LiveColumn, len(live))
	for _, l := range live {
		liveBy[l.Name] = l
	}
	wantBy := make(map[string]bool, len(want))
	typeOf := make(map[string]string, len(want))
	for _, w := range want {
		wantBy[w.Name] = true
		typeOf[w.Name] = w.Type
		l, has := liveBy[w.Name]
		if !has {
			v.Missing = append(v.Missing, w.Name)
			continue
		}
		switch l.DefaultKind {
		case "", "DEFAULT":
		default:
			v.Reasons = append(v.Reasons, "column "+w.Name+" is "+l.DefaultKind+" on the table; the store writes it")
			continue
		}
		wt, lt := NormalizeType(w.Type), NormalizeType(l.Type)
		if wt != lt {
			if stripLowCardinality(wt) == stripLowCardinality(lt) {
				v.Notes = append(v.Notes, "column "+w.Name+" differs only in LowCardinality: "+l.Type+" on the table, "+w.Type+" generated")
			} else {
				v.Reasons = append(v.Reasons, "column "+w.Name+" is "+l.Type+" on the table, "+w.Type+" generated")
				continue
			}
		}
		// Codecs are not compared: the server spells them with defaults
		// filled in (Delta becomes Delta(1)), and none changes what a
		// reader sees.
	}
	for _, l := range live {
		if !wantBy[l.Name] {
			v.Extra = append(v.Extra, l.Name)
		}
	}
	// A missing column whose name matches an extra one but for aspects, with
	// the same type, is that column renamed by a codec or vocabulary change.
	if len(v.Missing) > 0 && len(v.Extra) > 0 {
		extraByKey := map[string]int{}
		for i, e := range v.Extra {
			if k, ok := AspectFreeKey(e); ok {
				if _, dup := extraByKey[k]; dup {
					extraByKey[k] = -1
				} else {
					extraByKey[k] = i
				}
			}
		}
		taken := map[int]bool{}
		missing := v.Missing[:0:0]
		for _, m := range v.Missing {
			k, ok := AspectFreeKey(m)
			i, found := extraByKey[k]
			if ok && found && i >= 0 && !taken[i] && NormalizeType(liveBy[v.Extra[i]].Type) == NormalizeType(typeOf[m]) {
				taken[i] = true
				v.Renames = append(v.Renames, ColumnRename{From: v.Extra[i], To: m})
				continue
			}
			missing = append(missing, m)
		}
		v.Missing = missing
		if len(taken) > 0 {
			extra := v.Extra[:0:0]
			for i, e := range v.Extra {
				if !taken[i] {
					extra = append(extra, e)
				}
			}
			v.Extra = extra
		}
	}
	switch {
	case len(v.Reasons) > 0:
		v.Verdict = SchemaVerdictIncompatible
	case len(v.Missing) > 0 || len(v.Renames) > 0:
		v.Verdict = SchemaVerdictMigratable
	default:
		v.Verdict = SchemaVerdictOk
	}
	return
}

// JudgeTable reads the live table c names and judges it. It does not fail:
// a table whose shape cannot be read gets the unchecked verdict with the
// reason.
func JudgeTable(ctx context.Context, exec ExecutorI, c TableContract) (v SchemaVerdict) {
	facts, err := ReadTableFacts(ctx, exec, c.Table)
	if err != nil {
		v = SchemaVerdict{Kind: c.Kind, Table: c.Table, Verdict: SchemaVerdictUnchecked, Reasons: []string{err.Error()}}
		return
	}
	v = JudgeFacts(c, facts)
	return
}

// JudgeFacts is [JudgeTable] over facts already read.
func JudgeFacts(c TableContract, facts TableFacts) (v SchemaVerdict) {
	if !facts.Exists {
		v = SchemaVerdict{Kind: c.Kind, Table: c.Table, Verdict: SchemaVerdictAbsent}
		return
	}
	v = JudgeColumns(c.Columns, facts.Columns)
	v.Kind, v.Table = c.Kind, c.Table
	v.Engine = facts.Engine
	if c.ColumnsOnly {
		return
	}
	v.Stamp = facts.Comment
	v.DedupWindow = strings.Contains(facts.CreateQuery, DedupWindowSetting)
	v.Age = stampAge(c, facts.Comment, v)
	if IsPlainMergeTree(facts.Engine) && !v.DedupWindow {
		v.Notes = append(v.Notes, "the table keeps no insert deduplication window, so a retried insert that had landed is written twice; `boxer schema plan` proposes the setting")
	}
	if !c.Layout.IsZero() {
		live := TableLayout{Engine: facts.Engine, PartitionKey: facts.PartitionKey, SortingKey: facts.SortingKey}
		if !live.Equal(c.Layout) {
			v.LayoutDiffers = true
			note := "layout differs (informational): the table is " + live.String() + "; this build creates " + c.Layout.String() +
				". The store reads and writes either; a partition or sorting key cannot change in place"
			if c.LayoutRecipe != "" {
				note += ", and " + c.LayoutRecipe + " copies the table into the new layout"
			}
			v.Notes = append(v.Notes, note)
		}
	}
	if ts, ok := ParseTableStamp(facts.Comment); ok {
		if ws, wok := ParseTableStamp(c.Stamp); wok && ts.Vocab != ws.Vocab && ts.Vocab != "" && ws.Vocab != "" {
			v.Notes = append(v.Notes, "the stamp's vocabulary digest differs from this build's: rows written under another membership-id assignment decode as absent")
		}
	}
	return
}

func stampAge(c TableContract, comment string, v SchemaVerdict) StampAgeE {
	live, ok := ParseTableStamp(comment)
	if !ok {
		return StampAgeUnstamped
	}
	if live.Columns == ColumnsDigest(c.Columns) {
		return StampAgeSame
	}
	switch {
	case len(v.Missing) > 0 || len(v.Renames) > 0:
		return StampAgeOlder
	case len(v.Extra) > 0:
		return StampAgeNewer
	}
	return StampAgeOther
}

// IsPlainMergeTree reports a non-replicated engine of the MergeTree family,
// the engines the non-replicated deduplication window applies to.
func IsPlainMergeTree(engine string) bool {
	return strings.HasSuffix(engine, "MergeTree") && !strings.HasPrefix(engine, "Replicated") && !strings.HasPrefix(engine, "Shared")
}

// SplitTableRef splits a possibly database-qualified reference. db is empty
// for a bare name.
func SplitTableRef(ref string) (db string, name string) {
	if i := strings.IndexByte(ref, '.'); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

// ReadTableFacts reads one table's engine, keys, comment, CREATE statement
// and columns from system.tables and system.columns. A bare reference resolves
// in the executor's current database.
func ReadTableFacts(ctx context.Context, exec ExecutorI, table string) (facts TableFacts, err error) {
	if err = CheckTableRef(table); err != nil {
		return
	}
	db, name := SplitTableRef(table)
	dbExpr := "currentDatabase()"
	if db != "" {
		dbExpr = quoteLiteral(db)
	}
	const settings = " SETTINGS output_format_arrow_string_as_string=1, output_format_arrow_low_cardinality_as_dictionary=0"
	err = forEachRow(ctx, exec,
		"SELECT engine, partition_key, sorting_key, comment, create_table_query FROM system.tables WHERE database = "+dbExpr+" AND name = "+quoteLiteral(name)+settings,
		[]string{"engine", "partition_key", "sorting_key", "comment", "create_table_query"}, func(vals []string) {
			facts.Exists = true
			facts.Engine, facts.PartitionKey, facts.SortingKey = vals[0], vals[1], vals[2]
			facts.Comment, facts.CreateQuery = vals[3], vals[4]
		})
	if err != nil || !facts.Exists {
		return
	}
	err = forEachRow(ctx, exec,
		"SELECT name, type, default_kind, compression_codec FROM system.columns WHERE database = "+dbExpr+" AND table = "+quoteLiteral(name)+" ORDER BY position"+settings,
		[]string{"name", "type", "default_kind", "compression_codec"}, func(vals []string) {
			facts.Columns = append(facts.Columns, LiveColumn{Name: vals[0], Type: vals[1], DefaultKind: vals[2], Codec: vals[3]})
		})
	return
}

// forEachRow runs sql and hands each row's named string columns to fn.
func forEachRow(ctx context.Context, exec ExecutorI, sql string, names []string, fn func(vals []string)) (err error) {
	vals := make([]string, len(names))
	for rec, rerr := range exec.QueryArrow(ctx, sql) {
		if rerr != nil {
			err = eh.Errorf("read table facts: %w", rerr)
			return
		}
		cols := make([]func(int) string, len(names))
		for i, n := range names {
			cols[i], err = stringAccessor(rec, n)
			if err != nil {
				rec.Release()
				return
			}
		}
		for r := range int(rec.NumRows()) {
			for i, c := range cols {
				vals[i] = strings.Clone(c(r))
			}
			fn(vals)
		}
		rec.Release()
	}
	return
}

func stringAccessor(rec arrow.RecordBatch, name string) (get func(int) string, err error) {
	idx := rec.Schema().FieldIndices(name)
	if len(idx) == 0 {
		err = eb.Build().Str("column", name).Errorf("result has no such column")
		return
	}
	switch a := rec.Column(idx[0]).(type) {
	case *array.String:
		get = a.Value
	case *array.LargeString:
		get = a.Value
	case *array.Binary:
		get = func(i int) string { return string(a.Value(i)) }
	default:
		err = eb.Build().Str("column", name).Stringer("type", a.DataType()).Errorf("column is not a string")
	}
	return
}

// abbreviate joins the first n of names and counts the rest.
func abbreviate(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:n], ", ") + ", … " + strconv.Itoa(len(names)-n) + " more"
}

func quoteLiteral(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
