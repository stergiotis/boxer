package capmapfacts

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// The column names this package's read-back queries are written in.
//
// They are **handles** — `section:column` (ADR-0116) — resolved to physical
// names by a client-side pass against the table's generated schema, not
// literals spelled out here. That is the difference between this file and the
// one it replaced: a `boxer.facts` regeneration that re-aspects a column
// changes the physical name, and a query written in handles follows it while a
// query written in literals goes stale silently. The jsonbench-on-facts trial
// (doc/trials/jsonbench-on-facts/) measured that failure mode across four
// separate findings; doc/explanation/leeway-sql-read-surface.md is the entry
// point, and the repository's standing instruction is to reach for that
// surface before hand-writing anything against a leeway table.
const (
	hId = "`id:id`"
	hTs = "`timestamp:ts`"

	hSymValue   = "`symbol:value`"
	hSymLr      = "`symbol:lr`"
	hSymLrCard  = "`symbol:lrcard`"
	hSymLmr     = "`symbol:lmr`"
	hSymLmrCard = "`symbol:lmrcard`"
	hSymMrhp    = "`symbol:mrhp`"

	hStrValue  = "`stringArray:value`"
	hStrLr     = "`stringArray:lr`"
	hStrLrCard = "`stringArray:lrcard`"
	hStrLen    = "`stringArray:len`"

	hTxtValue   = "`textArray:value`"
	hTxtLmr     = "`textArray:lmr`"
	hTxtLmrCard = "`textArray:lmrcard`"
	hTxtMrhp    = "`textArray:mrhp`"
	hTxtLen     = "`textArray:len`"

	hU8Value  = "`u8Array:value`"
	hU8Lr     = "`u8Array:lr`"
	hU8LrCard = "`u8Array:lrcard`"
	hU8Len    = "`u8Array:len`"

	hTimeValue   = "`timeArray:value`"
	hTimeLmr     = "`timeArray:lmr`"
	hTimeLmrCard = "`timeArray:lmrcard`"
	hTimeMrhp    = "`timeArray:mrhp`"
	hTimeLen     = "`timeArray:len`"

	hF64Value  = "`f64Array:value`"
	hF64Lr     = "`f64Array:lr`"
	hF64LrCard = "`f64Array:lrcard`"
	hF64Len    = "`f64Array:len`"

	hFkValue  = "`foreignKey:value`"
	hFkLr     = "`foreignKey:lr`"
	hFkLrCard = "`foreignKey:lrcard`"
)

// prepare rewrites an authored query into the one that executes: column
// handles become physical names, and `LW_GET` calls become the read-back
// expressions that locate an attribute by membership.
//
// Both resolve against the schema [dml.CreateSchemaFacts] generates rather than
// against the server: the names and lanes are a property of the code that
// writes the table, so a dump can be composed — and tested — with nothing
// running, and a handle or a section this package spells wrongly fails here
// instead of as a column ClickHouse has never heard of.
//
// Handles first, extraction second. The expansion emits physical names, and
// those contain colons; feeding them back through the handle resolver would
// invite it to read one as a `section:column` of its own.
//
// table is the qualified name the query reads; its database half is what
// resolves the unqualified references inside.
func prepare(sql string, table string) (out string, err error) {
	database, _, qualified := strings.Cut(table, ".")
	if !qualified || database == "" {
		return "", eh.Errorf("capmapfacts: %q is not a database-qualified table", table)
	}
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(
		map[string][]string{table: factsColumnNames()}))

	out = sql
	for _, pass := range []nanopass.Pass{
		passes.ResolveColumnNames(resolver, database, nil),
		constructsql.ExtractExpandPass(resolver, database),
	} {
		out, err = pass.Apply(env.NewEnvironment(), out)
		if err != nil {
			return "", eh.Errorf("capmapfacts: %s: %w", pass.Name, err)
		}
	}
	return out, nil
}

// factsColumnNames is the table's physical column list, off the generated
// Arrow schema — the same source the DML builders write through, so the two
// cannot disagree.
func factsColumnNames() (names []string) {
	fields := dml.CreateSchemaFacts().Fields()
	names = make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	return names
}
