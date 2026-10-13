// Package journalviews composes read-only ClickHouse views over the run
// journal's ledger (ADR-0308, update of 2026-10-13): one row per run as it
// stands, and one row per entry at its newest revision — every run, not
// the newest the keelson tables are bounded to.
//
// The views carry what the ledger carries and nothing of the vault: an
// entry's kind, key, role, names, label and writer, never its body, calls
// or arguments (ADR-0320 §SD1). A reader who may read content reads the
// vault through the journal store, under its grant.
//
// They are written as the trail views are (trailviews): authored with
// `LW_GET` over the generated schema, each output minted with `LW_PLAIN`
// as an opaque plain column whose physical name carries its logical name
// and canonical type, expanded client-side, stamped with [Stamp] in their
// COMMENT, and registered as a dependent of the read-surface install so a
// surface install re-creates them where the ledger tables exist.
package journalviews

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore/internal/lowlevel"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// ViewsVersion is the revision of the views this package composes. Bump it
// whenever a view's columns or meaning change.
const ViewsVersion = 1

// The view names, unqualified; each is created in the ledger's database.
const (
	ViewRuns    = "dm_journal_runs"
	ViewEntries = "dm_journal_entries"
)

// AllViewNames lists the views in creation order.
func AllViewNames() []string { return []string{ViewRuns, ViewEntries} }

// Stamp is what every view records in its COMMENT: this package's revision
// and the read-surface revision whose function bodies it inlines.
func Stamp() (stamp chviews.Stamp) {
	return chviews.Stamp("keelson journal views v" + strconv.Itoa(ViewsVersion) +
		" over leeway SQL read surface v" + strconv.Itoa(lwsqlsurface.Version))
}

// View is one composed view: its unqualified name, the statement that
// creates it, and its output columns.
type View struct {
	Name    string
	Sql     string
	Columns []Column
}

// Column is one output column: its logical name, canonical type, the
// ClickHouse type its values are cast to, and its physical name. Backbone
// columns are the ledger table's own and keep their names.
type Column struct {
	Name          string
	CanonicalType string
	ClickHouse    string
	Physical      string
	Backbone      bool
}

var clickhouseTypes = map[string]string{
	"s":   "String",
	"sh":  "Array(String)",
	"u8":  "UInt8",
	"u32": "UInt32",
	"u64": "UInt64",
	"z64": "DateTime64(9, 'UTC')",
}

// mintItem is the item kind every minted column declares.
const mintItem = "item:oq"

// plainChannel is the channel the LW_GET family reads a value through.
const plainChannel = "chan:low-card-ref"

// readShape is how a column is read off its section.
type readShape uint8

const (
	// scalar reads a section holding one value per attribute.
	scalar readShape = iota
	// first reads the single value of a `unit` field on an array section.
	first
	// list reads every value of an attribute on an array section.
	list
)

// column is one projected attribute: its logical name, the section and
// membership it lives on, how it is read and its canonical type.
type column struct {
	name    string
	section string
	memb    string
	shape   readShape
	ctype   string
}

// runColumns follow agentrunstore.Run: one section per attribute.
var runColumns = []column{
	{"cell", "runCell", "agentrunCell", scalar, "s"},
	{"conversation", "runConversation", "agentrunConversation", scalar, "s"},
	{"owner-app", "runOwnerApp", "runtimeApp", scalar, "s"},
	{"coordinator", "runCoordinator", "agentrunCoordinator", scalar, "s"},
	{"state", "runState", "agentrunState", scalar, "s"},
	{"fingerprint", "runFingerprint", "agentrunFingerprint", scalar, "s"},
	{"build", "runBuild", "agentrunBuild", scalar, "s"},
	{"profile", "runProfile", "agentrunProfile", scalar, "s"},
	{"label-level", "runLabelLevel", "agentrunLabelLevel", scalar, "u32"},
	{"label-tier", "runLabelTier", "agentrunLabelTier", scalar, "u32"},
	{"label-exposure", "runLabelExposure", "agentrunLabelExposure", scalar, "s"},
	{"label-origin", "runLabelOrigin", "agentrunLabelOrigin", scalar, "s"},
	{"created", "runCreated", "agentrunCreated", scalar, "z64"},
	{"last-ordinal", "runLastOrdinal", "agentrunLastOrdinal", scalar, "u32"},
	{"closed", "runClosed", "agentrunClosed", scalar, "z64"},
	{"digest", "runDigest", "agentrunDigest", scalar, "s"},
	{"head", "runHead", "agentrunHead", scalar, "s"},
	{"version", "runVersion", "agentrunVersion", scalar, "u32"},
	{"deleted-by", "runDeletedBy", "agentrunDeletedBy", scalar, "s"},
	{"deleted-at", "runDeletedAt", "agentrunDeletedAt", scalar, "z64"},
	{"holder", "runHolder", "agentrunHolder", scalar, "s"},
	{"epoch", "runEpoch", "agentrunEpoch", scalar, "u32"},
}

// entryColumns follow agentrunstore.Entry: the ledger's metadata on the
// facts data sections, `unit` fields read as first.
var entryColumns = []column{
	{"at", "timeArray", "agentrunEntryAt", first, "z64"},
	{"kind", "symbol", "agentrunEntryKind", scalar, "s"},
	{"key", "stringArray", "agentrunEntryKey", first, "s"},
	{"role", "symbol", "agentrunEntryRole", scalar, "s"},
	{"entry-id", "u64Array", "agentrunEntryId", first, "u64"},
	{"name", "stringArray", "agentrunEntryName", first, "s"},
	{"book", "symbol", "agentrunEntryBook", scalar, "s"},
	{"form", "symbol", "agentrunEntryForm", scalar, "s"},
	{"target", "u32Array", "agentrunEntryTarget", first, "u32"},
	{"from", "u32Array", "agentrunEntryFrom", first, "u32"},
	{"effect", "symbol", "agentrunEntryEffect", scalar, "s"},
	{"turn", "symbol", "agentrunEntryTurn", scalar, "s"},
	{"label-level", "u8Array", "agentrunEntryLabelLevel", first, "u8"},
	{"label-tier", "u8Array", "agentrunEntryLabelTier", first, "u8"},
	{"label-exposure", "symbolArray", "agentrunEntryLabelExposure", list, "sh"},
	{"label-origin", "symbolArray", "agentrunEntryLabelOrigin", list, "sh"},
	{"writer-run", "u64Array", "agentrunEntryWriterRun", first, "u64"},
	{"writer-turn", "symbol", "agentrunEntryWriterTurn", scalar, "s"},
	{"writer-ordinal", "u32Array", "agentrunEntryWriterOrdinal", first, "u32"},
	{"parent", "u32Array", "agentrunEntryParent", first, "u32"},
	{"attachment-media-types", "symbolArray", "agentrunEntryAttachmentMediaTypes", list, "sh"},
}

// read renders a column's read expression, authored.
func read(c column) (expr string) {
	switch c.shape {
	case scalar:
		return fmt.Sprintf("LW_GET('%s', '%s', '%s')", c.section, c.memb, plainChannel)
	case first:
		return fmt.Sprintf("arrayElement(LW_GET_LIST('%s', '%s', '%s'), 1)", c.section, c.memb, plainChannel)
	}
	return fmt.Sprintf("LW_GET_LIST('%s', '%s', '%s')", c.section, c.memb, plainChannel)
}

func castTo(expr string, ctype string) (out string, err error) {
	ch, ok := clickhouseTypes[ctype]
	if !ok {
		return "", eb.Build().Str("canonicalType", ctype).Errorf("journalviews: no ClickHouse type for canonical type")
	}
	if ctype == "z64" {
		return "toDateTime64(" + expr + ", 9, 'UTC')", nil
	}
	return "CAST(" + expr + ", '" + ch + "')", nil
}

// backbone is a ledger envelope column a view passes through.
type backbone struct {
	name     string
	physical string
}

// Compose returns the views over the ledger in database, in creation
// order; empty selects agentrunstore.DatabaseName. Each statement is
// CREATE OR REPLACE and stamped. The read surface must be installed, and
// the ledger tables must exist.
func Compose(database string) (views []View, err error) {
	if database == "" {
		database = agentrunstore.DatabaseName
	}
	composer, err := lwsql.NewComposer(lwsql.DefaultTableSegments())
	if err != nil {
		return nil, eh.Errorf("journalviews: composer: %w", err)
	}
	runs, err := compose(composer, database, ViewRuns, database+"."+agentrunstore.TableNameRun, schemaNames(lowlevel.CreateSchemaAgentrunTable().Fields()),
		[]backbone{{"run", strings.Trim(agentrunstore.RunColKey, `"`)}, {"ts", strings.Trim(agentrunstore.RunColOrder, `"`)}},
		runColumns,
		// The run row is rewritten latest-wins: the newest row per run
		// is the run, a deletion's tombstone included.
		" ORDER BY "+agentrunstore.RunColOrder+" DESC LIMIT 1 BY "+agentrunstore.RunColKey)
	if err != nil {
		return nil, err
	}
	entries, err := compose(composer, database, ViewEntries, database+"."+agentrunstore.TableNameEntry, schemaNames(lowlevel.CreateSchemaAgentrunentryTable().Fields()),
		[]backbone{{"run", strings.Trim(agentrunstore.EntryColKey, `"`)}, {"ordinal", strings.Trim(agentrunstore.EntryColOrder, `"`)}, {"revision", strings.Trim(agentrunstore.EntryColRevision, `"`)}},
		entryColumns,
		// An entry is appended once; a later lease epoch rewrites it under
		// a higher revision, which supersedes.
		" ORDER BY "+agentrunstore.EntryColKey+" ASC, "+agentrunstore.EntryColOrder+" ASC, "+agentrunstore.EntryColRevision+" DESC LIMIT 1 BY "+agentrunstore.EntryColKey+", "+agentrunstore.EntryColOrder)
	if err != nil {
		return nil, err
	}
	return []View{runs, entries}, nil
}

// Statements is [Compose] as bare statements.
func Statements(database string) (stmts []string, err error) {
	views, err := Compose(database)
	if err != nil {
		return nil, err
	}
	for _, v := range views {
		stmts = append(stmts, v.Sql)
	}
	return
}

func schemaNames(fields []arrow.Field) (names []string) {
	names = make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	return
}

// compose authors one view's SELECT, expands it against the table's
// generated schema, and wraps the expansion in the current-row rule: the
// collapse runs over the expanded columns, so it needs no handle.
func compose(composer *lwsql.Composer, database string, name string, table string, physical []string, bb []backbone, cols []column, collapse string) (v View, err error) {
	b := strings.Builder{}
	b.WriteString("SELECT\n  ")
	for i, c := range bb {
		if i > 0 {
			b.WriteString(",\n  ")
		}
		b.WriteString(`"` + c.physical + `"`)
		v.Columns = append(v.Columns, Column{Name: c.name, Physical: c.physical, Backbone: true})
	}
	for _, c := range cols {
		var cast string
		cast, err = castTo(read(c), c.ctype)
		if err != nil {
			return View{}, eb.Build().Str("view", name).Str("column", c.name).Errorf("%w", err)
		}
		fmt.Fprintf(&b, ",\n  LW_PLAIN(%s, '%s', '%s', '%s')", cast, c.name, c.ctype, mintItem)
		var phys string
		phys, err = composer.PlainColumn(c.name, c.ctype, []string{mintItem})
		if err != nil {
			return View{}, eb.Build().Str("view", name).Str("column", c.name).Errorf("journalviews: mint: %w", err)
		}
		v.Columns = append(v.Columns, Column{Name: c.name, CanonicalType: c.ctype, ClickHouse: clickhouseTypes[c.ctype], Physical: phys})
	}
	fmt.Fprintf(&b, "\nFROM %s", table)
	sel, err := prepare(b.String(), table, physical)
	if err != nil {
		return View{}, eb.Build().Str("view", name).Errorf("%w", err)
	}
	v.Name = name
	v.Sql = fmt.Sprintf("CREATE OR REPLACE VIEW %s.%s AS\nSELECT * FROM (\n%s\n)%s\nCOMMENT %s", database, name, sel, collapse, Stamp().Literal())
	return v, nil
}

// membershipIds resolves membership names over the runtime vocabulary,
// the one the journal is written in.
type membershipIds struct{}

// LookupMembership is constructsql.MembershipIdsI.
func (membershipIds) LookupMembership(name string) (id uint64, err error) {
	e, err := vocab.NkRegistry.Lookup(naming.StylableName(name))
	if err != nil {
		return 0, eb.Build().Str("name", name).Errorf("journalviews: membership is not in the runtime vocabulary: %w", err)
	}
	return e.GetId().Value(), nil
}

// prepare expands the LW_GET family and LW_PLAIN against the table's
// generated schema, so a misspelled section or membership fails here.
func prepare(sql string, table string, physical []string) (out string, err error) {
	database, _, _ := strings.Cut(table, ".")
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(map[string][]string{table: physical}))
	out = sql
	for _, pass := range []nanopass.Pass{
		passes.ResolveColumnNames(resolver, database, nil),
		constructsql.ExtractExpandPassWithIds(resolver, membershipIds{}, database),
		constructsql.ExpandPass,
	} {
		out, err = pass.Apply(env.NewEnvironment(), out)
		if err != nil {
			return "", eb.Build().Str("pass", pass.Name).Errorf("journalviews: pass failed: %w", err)
		}
	}
	return
}
