// Package chviews declares the leeway schema-decode views (ADR-0226): three
// ClickHouse views that read a leeway physical column name apart over
// system.columns and system.tables, so the structure a name carries —
// section, column, role, canonical type, aspects, row config, co-section and
// streaming group — is selectable instead of re-derived by hand in every
// ad-hoc query.
//
// The views decode names. They do not classify tables (§SD2): restoration and
// normalization live in the Go path, and boxer.tables_leeway (ADR-0170)
// stays the authority on whether a table is leeway. What is reported here is
// what a name's *shape* supports, which is why the verdict columns are called
// `layout` and `name_shape` and why none of them is called `is_leeway`.
//
// Every segment index, prefix spelling and enum code in the emitted SQL is
// rendered from the declarations the composer and the parser already read
// (§SD1) — the naming convention's per-layout field positions, and the enum
// tables for plain item types, column roles, lane kinds and table row
// configs. The three aspect segments are handed to chpack's LW_ASPECT_*
// family rather than re-rendered here, because those bodies are generated
// from the aspect vocabularies already (ADR-0182 §SD4) and a second rendering
// is the drift ADR-0170 §Q1 named.
//
// This package declares and renders the views; it does not install them.
// Provisioning and the version handshake live in lwsqlsurface (ADR-0171
// §SD2).
//
// One ClickHouse behaviour shapes both: a SQL UDF call is EXPANDED INTO the
// view's stored query at CREATE time, not resolved at read time. A view
// written against LW_ASPECT_NAMES_ENC carries the whole aspect vocabulary
// inlined, keeps answering after the function is replaced or dropped, and
// answers with the tables as they were the moment it was created. So a view
// is a snapshot, functions must exist before it is created, and re-creating
// it is the only thing that refreshes it — which is why every install
// re-creates them and why each carries the surface revision it was built
// against (ADR-0226 §SD4).
package chviews

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
)

// DefaultDatabase is where the views live. Their own database, not a corner
// of boxer: these are always-live decodes of server metadata, and boxer.*
// holds run-stamped snapshots (ADR-0226 §SD5).
const DefaultDatabase = "leeway"

// View names, unqualified.
const (
	// ViewColumns is the decode — one row per column of every table outside
	// the system databases, foreign columns included as rows rather than
	// absences.
	ViewColumns = "columns"
	// ViewSections aggregates the decode to (table, section) grain.
	ViewSections = "sections"
	// ViewTables aggregates it to table grain and joins system.tables.
	ViewTables = "tables"
)

// systemDatabases are the databases a decode never reaches: no leeway table
// lives in one, and both INFORMATION_SCHEMA spellings exist (ADR-0170 §SD1).
var systemDatabases = []string{"system", "INFORMATION_SCHEMA", "information_schema"}

// TargetDatabase is where the views are created. The empty value means
// [DefaultDatabase]; an endpoint whose role cannot create that database
// points this at one it owns.
type TargetDatabase string

// Name resolves the target, applying the default.
func (inst TargetDatabase) Name() (name string) {
	if inst == "" {
		return DefaultDatabase
	}
	return string(inst)
}

// Validate refuses a target that is not a plain SQL identifier. The name is
// spliced into DDL and into the reconciler's system.tables predicate, so it
// is checked once at the boundary rather than quoted at every use.
func (inst TargetDatabase) Validate() (err error) {
	name := inst.Name()
	if name == "" {
		return eh.Errorf("empty target database")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		alpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		if alpha || (i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return eb.Build().Str("database", name).Errorf("target database is not a plain SQL identifier")
	}
	return
}

// Qualified renders `database.view` for one of the views.
func (inst TargetDatabase) Qualified(view string) (name string) {
	return inst.Name() + "." + view
}

// Stamp is the provenance a view records in its ClickHouse COMMENT — the
// surface revision its inlined function bodies came from.
//
// It exists because a view is a snapshot (see the package doc) and absence is
// therefore not the only way a view can be wrong: one created against an
// earlier vocabulary is present, selectable, and answers with a stale table.
// Comparing this string to what the build expects is what makes that visible,
// and it is the reason the views need no version marker of their own — the
// stamp carries the surface's.
type Stamp string

// Literal renders the stamp as a SQL string literal, for a reader comparing
// system.tables.comment against it.
func (inst Stamp) Literal() (lit string) {
	return sqlLiteral(string(inst))
}

// View is one declared view: the name a query spells, the SELECT it stands
// for, and one line on what it carries.
type View struct {
	Name string
	Body string
	Doc  string
}

// Views returns the roster in installation order, which is also dependency
// order — the aggregates select from the decode, so it is created first.
func Views(target TargetDatabase) (views []View) {
	views = []View{
		{
			Name: ViewColumns,
			Body: columnsBody(),
			Doc:  "one row per column of every non-system table, with the leeway physical name decoded; layout is foreign for a name that is not leeway-shaped",
		},
		{
			Name: ViewSections,
			Body: sectionsBody(target),
			Doc:  "one row per (table, section), with the section's value columns, roles, canonical types and use-aspects",
		},
		{
			Name: ViewTables,
			Body: tablesBody(target),
			Doc:  "one row per non-system table, with its layout counts, sections and system.tables passthrough; name_shape reports what the names support, never whether discovery would succeed",
		},
	}
	return
}

// AllViewNames lists the views in installation order.
func AllViewNames() (names []string) {
	names = []string{ViewColumns, ViewSections, ViewTables}
	return
}

// Statement renders one CREATE OR REPLACE VIEW, stamped with the revision its
// inlined bodies came from.
func Statement(target TargetDatabase, stamp Stamp, v View) (sql string) {
	return "CREATE OR REPLACE VIEW " + target.Qualified(v.Name) + " AS\n" + v.Body +
		"\nCOMMENT " + stamp.Literal()
}

// Statements renders the whole family in installation order, preceded by the
// database it needs.
//
// CREATE OR REPLACE rather than drop-then-create: replacing re-expands the
// function bodies, which is the whole refresh, and it leaves no window in
// which the views are absent.
func Statements(target TargetDatabase, stamp Stamp) (stmts []string) {
	views := Views(target)
	stmts = make([]string, 0, len(views)+1)
	stmts = append(stmts, "CREATE DATABASE IF NOT EXISTS "+target.Name())
	for _, v := range views {
		stmts = append(stmts, Statement(target, stamp, v))
	}
	return
}

// DropStatements removes the family, aggregates first — the aggregates select
// from the decode, so dropping in install order would leave a view standing
// over one that no longer exists.
//
// Not part of an install: a re-install replaces the views in place. This is
// for taking the family off a server.
func DropStatements(target TargetDatabase) (stmts []string) {
	names := AllViewNames()
	stmts = make([]string, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		stmts = append(stmts, "DROP VIEW IF EXISTS "+target.Qualified(names[i]))
	}
	return
}

// notSystemDatabases is the predicate both system-table readers carry.
func notSystemDatabases(column string) (expr string) {
	return column + " NOT IN " + sqlStringTuple(systemDatabases)
}

// separatorLiteral is the component separator the decode splits on, as a SQL
// literal. One separator is in scope (ADR-0226 §SD5).
func separatorLiteral() (lit string) {
	return sqlLiteral(ddl.DefaultSeparator)
}

// indent shifts a rendered sub-select so a printed statement stays readable.
func indent(sql string, by string) (out string) {
	return by + strings.ReplaceAll(sql, "\n", "\n"+by)
}
