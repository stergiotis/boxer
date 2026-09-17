package swisstopo

import (
	_ "embed"
	"strings"
)

// udfsSQL is the ClickHouse DDL that creates the SWISSTOPO_* function family
// (ADR-0244) — the server-side twin of the transforms in lv95.go and
// lv95_rigorous.go. The file itself carries what the family is, why three of
// its spellings are not the published formulas, and the two things it
// deliberately leaves out.
//
//go:embed lv95_udfs.sql
var udfsSQL string

// ScriptSQL returns the whole installable script: the commentary, then one
// CREATE OR REPLACE FUNCTION per member in dependency order. Feed it to a
// client that accepts multi-statement input — `clickhouse-client < script` —
// and use [StatementsSQL] where the transport takes one query per request.
//
// Nothing here installs it. A package that computes coordinates has no
// business owning a database connection, and every caller that would provision
// the family already has a client.
func ScriptSQL() (sql string) {
	sql = udfsSQL
	return
}

// StatementsSQL returns the family's CREATE statements alone, in installation
// order, with the commentary stripped.
//
// The order is load-bearing: ClickHouse resolves a referenced function at
// CREATE time, so a member installed before what it calls is refused.
//
// The split is lexical — drop `--` lines, cut on `;`. That is safe for this
// file and only this file: no body contains a `;`, and its only string
// literals are the CAST target types. TestStatementsSQLMatchesNames pins the
// result, so a future body that breaks the assumption fails the build rather
// than installing half a function.
func StatementsSQL() (stmts []string) {
	var b strings.Builder
	b.Grow(len(udfsSQL))
	for line := range strings.SplitSeq(udfsSQL, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	stmts = make([]string, 0, 32)
	for stmt := range strings.SplitSeq(b.String(), ";") {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return
}

// UDFNames returns the names [StatementsSQL] creates, in the same order.
//
// The family owns the `SWISSTOPO_` prefix, so a server's copy of it is
// `SELECT name FROM system.functions WHERE name LIKE 'SWISSTOPO\_%'` — which
// is what this list is compared against to tell a provisioned server from a
// partially provisioned one, and what a caller drops to remove the family.
func UDFNames() (names []string) {
	const head = "CREATE OR REPLACE FUNCTION "
	stmts := StatementsSQL()
	names = make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		rest, found := strings.CutPrefix(stmt, head)
		if !found {
			continue
		}
		name, _, _ := strings.Cut(rest, " ")
		names = append(names, name)
	}
	return
}
