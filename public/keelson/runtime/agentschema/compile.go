package agentschema

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// One declaration compiles twice (ADR-0313 §SD6): to a server principal —
// a user, a settings profile that pins the scope READONLY, definer views,
// grants on the views alone, and the restrictive backstop policy — and to
// the local worker, the same view text with the scope as a constant.

// Principal is the agent's server identity.
type Principal struct {
	User    string
	Profile string
	// Database is where the views are created; empty leaves them
	// unqualified.
	Database string
	// Definer is the user the views run as; empty is default.
	Definer string
}

func (inst Principal) qualify(name string) string {
	if inst.Database == "" {
		return quote(name)
	}
	return quote(inst.Database) + "." + quote(name)
}

func (inst Principal) definer() string {
	if inst.Definer == "" {
		return quote("default")
	}
	return quote(inst.Definer)
}

func quote(ident string) string { return "`" + strings.ReplaceAll(ident, "`", "``") + "`" }

func quoteRef(ref string) string {
	if i := strings.IndexByte(ref, '.'); i > 0 {
		return quote(ref[:i]) + "." + quote(ref[i+1:])
	}
	return quote(ref)
}

// ScopeRelationSetting is the marker a definer view sets on its own SELECT
// and the backstop of the same relation is keyed on (§SD3). Several
// relations read one base table, and ClickHouse ANDs restrictive policies
// on a table together, so a backstop keyed on the scope setting alone
// would cut every relation down to the narrowest one; keyed on the
// relation's marker, a backstop fires inside its own view and nowhere
// else. The server's custom_settings_prefixes must admit the prefix, as
// for the scope settings.
const ScopeRelationSetting = "custom_agent_relation"

// Scopes maps a scope setting to the value it is pinned to: for the
// server, in the principal's profile; for the worker, as the constant the
// single writer ships.
type Scopes map[string]string

// resolver is a setting's value as one compilation target reads it.
type resolver func(setting string) string

// serverValue reads the constrained setting.
func serverValue(setting string) string {
	return "getSettingOrDefault(" + marshalling.EscapeString(setting) + ", '')"
}

// workerValues is the constants the worker ships; every setting the
// relation reads must be pinned, since an empty scope is a view that
// answers nothing without a refusal (§SD2).
func workerValues(r Relation, scopes Scopes) (resolve resolver, err error) {
	if err = pinned(r, scopes); err != nil {
		return
	}
	resolve = func(setting string) string { return marshalling.EscapeString(scopes[setting]) }
	return
}

// pinned refuses a relation whose scope setting, or the setting it
// excludes by, has no non-empty value.
func pinned(r Relation, scopes Scopes) (err error) {
	for _, s := range r.settings() {
		if scopes[s] == "" {
			return eb.Build().Str("relation", r.Name).Str("setting", s).Errorf("the relation's scope setting is not pinned")
		}
	}
	return
}

// baseAlias is the base table's alias in a relation's SELECT: every base
// column is read through it, so an alias the select list defines (the
// kept memberships keep the column's name) never shadows the column.
const baseAlias = "base"

func base(col string) string { return baseAlias + "." + quote(col) }

// ScopePredicate is the WHERE clause binding the relation to the server's
// settings, an SQL expression over the base alias; BackstopPredicate is
// the same over the bare column, for a row policy, which has no alias.
func ScopePredicate(r Relation) string {
	return scopePredicate(r, serverValue, base(r.Scope.Column))
}

func BackstopPredicate(r Relation) string {
	return scopePredicate(r, serverValue, quote(r.Scope.Column))
}

func scopePredicate(r Relation, resolve resolver, col string) (pred string) {
	if v := r.Scope.Via; v != nil {
		pred = col + " IN (SELECT " + quote(v.Key) + " FROM " + quoteRef(v.Table) + " WHERE " + quote(v.Column) + " = " + resolve(r.Scope.Setting) + ")"
	} else {
		pred = col + " = " + resolve(r.Scope.Setting)
	}
	if r.Scope.Except != "" {
		pred = "(" + pred + ") AND " + col + " != " + resolve(r.Scope.Except)
	}
	return
}

// SelectList is the relation's columns as the agent reads them: the passed
// sections, each blanked on a withheld row, and the withheld section's two
// lanes when the relation withholds anything (§SD5).
func SelectList(r Relation) (cols []string) {
	for _, s := range r.Sections {
		if !s.Pass {
			continue
		}
		expr := base(s.Name)
		if s.Expr != "" {
			expr = s.Expr
		}
		if withheld := r.withheldFor(s.Name); len(withheld) > 0 {
			expr = "if(hasAny(" + base(r.Memberships) + ", " + stringArray(withheld) + "), defaultValueOfTypeName(toTypeName(" + expr + ")), " + expr + ")"
		}
		cols = append(cols, expr+" AS "+quote(s.Name))
	}
	if all := r.withheldAll(); len(all) > 0 {
		kept := "arrayFilter(x -> NOT has(" + stringArray(all) + ", x), " + base(r.Memberships) + ")"
		withheld := "arrayFilter(x -> has(" + stringArray(all) + ", x), " + base(r.Memberships) + ")"
		cols = append(cols,
			kept+" AS "+quote(r.Memberships),
			withheld+" AS `withheld_memberships`",
			"arrayMap(x -> "+marshalling.EscapeString("withhold:"+r.Name)+", "+withheld+") AS `withheld_policy`")
	}
	return
}

func stringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, marshalling.EscapeString(v))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func selectSQL(r Relation, resolve resolver) string {
	return "SELECT " + strings.Join(SelectList(r), ", ") + " FROM " + quoteRef(r.Over) + " AS " + baseAlias + " WHERE " + scopePredicate(r, resolve, base(r.Scope.Column))
}

// ServerSelect is the relation's SELECT as the server's definer view runs
// it: under the constrained settings, with the relation's marker set so
// its own backstop applies.
func ServerSelect(r Relation) string {
	return selectSQL(r, serverValue) + " SETTINGS " + quote(ScopeRelationSetting) + " = " + marshalling.EscapeString(r.Name)
}

// ServerDDL is the statements that make the principal and the relations on
// the server (§SD2, §SD3): the user, reachable from the host alone; the
// profile pinning each scope setting to its value READONLY (readonly = 2
// too), replaced on every pin; one definer view per relation, replaced
// likewise; a grant on the view alone; and one restrictive backstop policy
// per relation, keyed on the relation's marker. Every setting a relation
// reads must be pinned. The principal's lifetime is the caller's: the user
// is created once and its profile, views and policies follow each pin;
// nothing here drops them.
//
// Two facts about the server the caller holds: a replaced profile reaches
// a new connection or session and not one that is open, so a pin needs a
// fresh connection and a pooled connection must not cross a pin; and the
// backstops are restrictive policies with no permissive one beside them,
// which read as "everything else passes" only while the server's
// access_control_improvements.users_without_row_policies_can_read_rows
// holds its default (true) — off, every reader of the base table gets no
// rows and no refusal, the silent empty result §SD2 exists to prevent.
// ServerPreflight probes for it; a permissive "USING 1 TO ALL" would not
// do, since permissive policies OR together and would widen any other
// permissive policy a user holds.
func ServerDDL(p Principal, relations []Relation, scopes Scopes) (stmts []string, err error) {
	if p.User == "" || p.Profile == "" {
		err = eh.Errorf("a principal names its user and profile")
		return
	}
	for _, ident := range []string{p.User, p.Profile, p.Database, p.Definer} {
		if strings.ContainsAny(ident, "\x00\n\r") {
			err = eb.Build().Str("identifier", ident).Errorf("a principal's identifier is a plain name")
			return
		}
	}
	for _, r := range relations {
		if err = r.Validate(); err != nil {
			return
		}
		if err = pinned(r, scopes); err != nil {
			return
		}
	}
	stmts = append(stmts, "CREATE USER IF NOT EXISTS "+quote(p.User)+" IDENTIFIED WITH no_password HOST LOCAL")
	settings := []string{"readonly = 2 READONLY"}
	seen := map[string]bool{}
	for _, r := range relations {
		for _, s := range r.settings() {
			if seen[s] {
				continue
			}
			seen[s] = true
			settings = append(settings, quote(s)+" = "+marshalling.EscapeString(scopes[s])+" READONLY")
		}
	}
	stmts = append(stmts, "CREATE SETTINGS PROFILE OR REPLACE "+quote(p.Profile)+" SETTINGS "+strings.Join(settings, ", ")+" TO "+quote(p.User))
	for _, r := range relations {
		view := p.qualify(r.Name)
		stmts = append(stmts, "CREATE OR REPLACE VIEW "+view+" SQL SECURITY DEFINER DEFINER = "+p.definer()+" AS "+ServerSelect(r))
		stmts = append(stmts, "GRANT SELECT ON "+view+" TO "+quote(p.User))
		// The backstop (§SD3): restrictive, to all, keyed on the
		// relation's marker with "another relation, or none" as its other
		// arm; it fires inside this relation's definer view written
		// without its scope and never otherwise (law L18).
		stmts = append(stmts, "CREATE ROW POLICY OR REPLACE "+quote(r.Name+"_backstop")+" ON "+quoteRef(r.Over)+
			" AS RESTRICTIVE USING "+serverValue(ScopeRelationSetting)+" != "+marshalling.EscapeString(r.Name)+" OR "+BackstopPredicate(r)+" TO ALL")
	}
	return
}

// PreflightTable is the scratch table ServerPreflight probes with.
const PreflightTable = "agentschema_preflight"

// ServerPreflight is the probe for the server fact ServerDDL depends on:
// a one-row table under a restrictive policy that passes every row, read
// by the caller. With users_without_row_policies_can_read_rows at its
// default the probe counts one row; off, it counts none, and the
// backstops would blank every relation. The setup runs first, the probe
// goes through PreflightCount, and the teardown runs last whatever the
// count was.
func ServerPreflight(database string) (setup []string, probe string, teardown []string) {
	table := quote(PreflightTable)
	if database != "" {
		table = quote(database) + "." + table
	}
	setup = []string{
		"CREATE TABLE IF NOT EXISTS " + table + " (`one` UInt8) ENGINE = Memory",
		"INSERT INTO " + table + " VALUES (1)",
		"CREATE ROW POLICY OR REPLACE " + quote(PreflightTable+"_backstop") + " ON " + table + " AS RESTRICTIVE USING 1 TO ALL",
	}
	probe = "SELECT count() FROM " + table
	teardown = []string{
		"DROP ROW POLICY IF EXISTS " + quote(PreflightTable+"_backstop") + " ON " + table,
		"DROP TABLE IF EXISTS " + table,
	}
	return
}

// PreflightCount is the probe's verdict: one row means the server reads
// through a restrictive policy as ServerDDL assumes; anything else refuses
// the setup.
func PreflightCount(n int64) (err error) {
	if n != 1 {
		return eb.Build().Int64("rows", n).Errorf("the server hides rows from readers without a permissive policy (access_control_improvements.users_without_row_policies_can_read_rows is off): the backstops would blank every relation")
	}
	return
}

// WorkerView is the relation as the local worker creates it, with the
// scopes as constants the single writer ships (§SD6); replaced when it
// exists, as the server's is.
func WorkerView(r Relation, scopes Scopes) (sql string, err error) {
	if err = r.Validate(); err != nil {
		return
	}
	resolve, err := workerValues(r, scopes)
	if err != nil {
		return
	}
	return "CREATE OR REPLACE VIEW " + quote(r.Name) + " AS " + selectSQL(r, resolve), nil
}

// WorkerSelect is the relation's rows in the worker under constant scopes,
// without a view.
func WorkerSelect(r Relation, scopes Scopes) (sql string, err error) {
	if err = r.Validate(); err != nil {
		return
	}
	resolve, err := workerValues(r, scopes)
	if err != nil {
		return
	}
	return selectSQL(r, resolve), nil
}
