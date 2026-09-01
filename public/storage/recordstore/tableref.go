package recordstore

import (
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// tableRefRe is the shape a runtime table override must have: an unquoted
// ClickHouse identifier, optionally database-qualified. Wider than the
// generation-time gate (gen.Input.TableName is [a-z][a-z0-9]*, because
// class names are derived from it) — at run time the reference is only
// spliced into SQL, so the unquoted-identifier rule is the one that
// matters. Underscores are common in scratch and per-environment table
// names and are legal here.
var tableRefRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// CheckTableRef validates a <Store>StoreConfig.Table override. The
// generated constructors call it and panic on error — a malformed
// reference is a wiring bug, and every statement the store issues would
// carry it.
func CheckTableRef(ref string) (err error) {
	if !tableRefRe.MatchString(ref) {
		err = eb.Build().Str("ref", ref).Errorf("table reference must be an unquoted ClickHouse identifier, optionally database-qualified ([A-Za-z_][A-Za-z0-9_]*(.[A-Za-z_][A-Za-z0-9_]*)?)")
	}
	return
}

// createTableVerbs are the header prefixes clickhouse.ComposeCreateTable
// can emit (CreateModeIfNotExists / CreateModeOrReplace / CreateModePlain).
// Longest first: "CREATE TABLE " is a prefix of the IF NOT EXISTS form.
var createTableVerbs = []string{
	"CREATE TABLE IF NOT EXISTS ",
	"CREATE OR REPLACE TABLE ",
	"CREATE TABLE ",
}

// ProvisioningStatements turns a generated store's embedded DDL script into
// the statements its EnsureTable issues, one per ExecutorI.Exec: the
// optional "CREATE DATABASE IF NOT EXISTS <db>" and the "CREATE TABLE …".
// One statement per Exec is the executor contract — the ClickHouse HTTP
// interface rejects a multi-statement body outright ("Multi-statements are
// not allowed"), so a store that shipped the script whole could never
// provision itself through the HTTP executor.
//
// baked is the qualified reference the script was composed with (the
// store's <Store>TableName const: "<db>.<table>" or "<table>"). target,
// when non-empty, is the <Store>StoreConfig.Table override the statements
// are re-pointed at instead: the database statement follows target's
// qualification (dropped for a bare target, which then binds whatever
// database the executor connects to), and the CREATE TABLE header is
// re-named. Only the header is touched — the column block, the ADR-0102
// clauses and any tail stay byte-identical, so an override table has the
// schema the store decodes positionally.
//
// Both edits are anchored on text the generator emits verbatim, and the
// function refuses rather than guesses: a script whose prelude or header
// it does not recognise returns an error, because provisioning under the
// wrong name is worse than not provisioning.
func ProvisioningStatements(script, baked, target string) (stmts []string, err error) {
	if baked == "" {
		err = eh.Errorf("provisioning statements: empty baked table reference")
		return
	}
	if target != "" {
		err = CheckTableRef(target)
		if err != nil {
			return
		}
	} else {
		target = baked
	}
	rest := script
	// Prelude: present iff the baked reference is database-qualified.
	if db, _, qualified := strings.Cut(baked, "."); qualified {
		prelude := "CREATE DATABASE IF NOT EXISTS " + db + ";\n\n"
		if !strings.HasPrefix(rest, prelude) {
			err = eb.Build().Str("baked", baked).Errorf("provisioning statements: baked reference is qualified but the script does not start with its database prelude")
			return
		}
		rest = rest[len(prelude):]
	}
	// Header: exactly one of the composer's verbs, then the baked name,
	// then the column block opener.
	create := ""
	for _, verb := range createTableVerbs {
		head := verb + baked + " ("
		if strings.HasPrefix(rest, head) {
			create = verb + target + " (" + rest[len(head):]
			break
		}
	}
	if create == "" {
		err = eh.Errorf("provisioning statements: no CREATE TABLE header naming %q found at the start of the script", baked)
		return
	}
	create = strings.TrimRight(create, " \t\r\n;")
	if db, _, qualified := strings.Cut(target, "."); qualified {
		stmts = append(stmts, "CREATE DATABASE IF NOT EXISTS "+db)
	}
	stmts = append(stmts, create)
	return
}
