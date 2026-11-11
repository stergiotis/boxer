// Package factsread is how Go code reads the facts table in SQL: a statement
// is authored against leeway's read surface — column handles and the LW_GET
// family — and [Prepare] turns it into the statement that executes.
//
// Nothing here computes a lane position. Locating an attribute by membership
// is the read surface's job (doc/explanation/leeway-sql-read-surface.md), and
// what the expansion emits calls functions installed on the server, which
// the facts store's setup provisions (ADR-0367, update of 2026-11-11).
package factsread

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// The membership channels the facts sections carry. Every section carries
// more than one, so a read names the one it means.
const (
	// PlainChannel is one membership per attribute.
	PlainChannel = "chan:low-card-ref"
	// MixedChannel is one membership shared by several attributes, told
	// apart by a high-cardinality parameter.
	MixedChannel = "chan:low-card-ref-high-card-params"
)

// MembName is the spelling a membership is named by in an LW_ call: the
// registry's folded natural key, which [Prepare] resolves back to the id
// before the statement ships (ADR-0171 §SD4).
func MembName(m registry.RegisteredNaturalKey) (name string) {
	return string(m.GetNaturalKey())
}

// Scalar reads the attribute carrying memb on a section that stores one
// value per attribute; the type default when the row does not carry it.
func Scalar(section string, memb registry.RegisteredNaturalKey) (expr string) {
	return fmt.Sprintf("LW_GET('%s', '%s', '%s')", section, MembName(memb), PlainChannel)
}

// List reads the values of the attribute carrying memb on an array-valued
// section; empty when the row does not carry it.
func List(section string, memb registry.RegisteredNaturalKey) (expr string) {
	return fmt.Sprintf("LW_GET_LIST('%s', '%s', '%s')", section, MembName(memb), PlainChannel)
}

// ListFirst is the first value of [List], the type default when there is
// none. It is the read for an attribute written with one value on an
// array-valued section: the verb follows the section's arity, not the Go
// field's type.
func ListFirst(section string, memb registry.RegisteredNaturalKey) (expr string) {
	return "arrayElement(" + List(section, memb) + ", 1)"
}

// membershipIds resolves names over the runtime vocabulary alone, which is
// the vocabulary the facts store and the query-run capture write.
type membershipIds struct{}

func (membershipIds) LookupMembership(name string) (id uint64, err error) {
	e, err := vocab.NkRegistry.Lookup(naming.StylableName(name))
	if err != nil {
		return 0, eb.Build().Str("name", name).Errorf("factsread: membership is not in the runtime vocabulary: %w", err)
	}
	return e.GetId().Value(), nil
}

// Prepare rewrites an authored statement into the one that executes: column
// handles become physical names, and LW_ extraction calls become read-back
// expressions.
//
// Both resolve against the schema [dml.CreateSchemaFacts] generates rather
// than against a server, so a statement is composed and tested with nothing
// running, and a handle or section spelled wrongly fails here. table must be
// database-qualified; its database half resolves the unqualified references
// inside. A trailing FORMAT is not part of the grammar the passes read: the
// caller appends it to the result.
//
// Handles first, extraction second: the expansion emits physical names, which
// contain colons, and the handle resolver must not see them.
func Prepare(sql string, table string) (out string, err error) {
	database, _, qualified := strings.Cut(table, ".")
	if !qualified || database == "" {
		return "", eb.Build().Str("table", table).Errorf("factsread: table is not database-qualified")
	}
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(
		map[string][]string{table: columnNames()}))
	out = sql
	for _, pass := range []nanopass.Pass{
		passes.ResolveColumnNames(resolver, database, nil),
		constructsql.ExtractExpandPassWithIds(resolver, membershipIds{}, database),
	} {
		out, err = pass.Apply(env.NewEnvironment(), out)
		if err != nil {
			return "", eb.Build().Str("pass", pass.Name).Errorf("factsread: pass failed: %w", err)
		}
	}
	return out, nil
}

// columnNames is the table's physical column list, off the generated Arrow
// schema the writers use.
func columnNames() (names []string) {
	fields := dml.CreateSchemaFacts().Fields()
	names = make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	return names
}
