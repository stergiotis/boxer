package capmapfacts

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/observability/eh"
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

// resolveHandles rewrites every handle in sql to the physical column name the
// generated schema gives it.
//
// The schema comes from [dml.CreateSchemaFacts] rather than from the server:
// the names are a property of the code that writes the table, so a dump can be
// composed — and tested — with nothing running. It also means a handle that
// does not resolve is a mistake in this package, caught here, rather than a
// column ClickHouse will complain about later.
//
// table is the qualified name the query reads; its database half is what
// resolves the unqualified references inside.
func resolveHandles(sql string, table string) (out string, err error) {
	database, _, qualified := strings.Cut(table, ".")
	if !qualified || database == "" {
		return "", eh.Errorf("capmapfacts: %q is not a database-qualified table", table)
	}
	provider := passes.NewStaticSchemaProvider(map[string][]string{table: factsColumnNames()})
	pass := passes.ResolveColumnNames(lwsql.NewResolver(provider), database, nil)
	out, err = pass.Apply(env.NewEnvironment(), sql)
	if err != nil {
		return "", eh.Errorf("capmapfacts: unable to resolve column handles: %w", err)
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
