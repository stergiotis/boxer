package agentschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// withholding is a relation over a plain table with a memberships column,
// two of whose memberships are withheld on one section.
func withholding() Relation {
	return Relation{Name: "notes", Over: "scratch.notes", Scope: Scope{Setting: "custom_agent_run", Column: "run"},
		Sections:    []SectionRule{{Name: "run", Pass: true}, {Name: "ord", Pass: true}, {Name: "note", Pass: true}, {Name: "contact", Pass: true}, {Name: "secret", Pass: false}},
		Memberships: "memberships", Withhold: []WithholdRule{{Section: "contact", Memberships: []string{"personal", "credential"}}}}
}

func testScopes() Scopes {
	return Scopes{ScopeSetting: "cell/run-1", ScopeConversation: "chat-1", ScopeApp: "github.com/x/chat"}
}

// The four rule forms validate; what is not one of them is refused.
func TestDeclarationsValidate(t *testing.T) {
	for _, r := range JournalRelations() {
		assert.NoError(t, r.Validate(), r.Name)
	}
	assert.NoError(t, withholding().Validate())
	bad := withholding()
	bad.Name = "my run"
	assert.Error(t, bad.Validate(), "a name is an identifier")
	bad = withholding()
	bad.Scope.Setting = ""
	assert.Error(t, bad.Validate(), "a scope is named")
	bad = withholding()
	bad.Scope.Except = bad.Scope.Setting
	assert.Error(t, bad.Validate(), "a scope excludes by another setting")
	bad = withholding()
	bad.Withhold[0].Section = "secret"
	assert.Error(t, bad.Validate(), "a withhold names a passed section")
	bad = withholding()
	bad.Memberships = ""
	assert.Error(t, bad.Validate(), "a withhold needs a memberships column")
	bad = withholding()
	bad.Sections = append(bad.Sections, SectionRule{Name: "note", Pass: true})
	assert.Error(t, bad.Validate(), "a section is declared once")
	bad = withholding()
	bad.Withhold[0].Memberships = []string{"it's"}
	assert.Error(t, bad.Validate(), "a membership is a plain key")
	bad = withholding()
	bad.Over = "a.b.c"
	assert.Error(t, bad.Validate())
	assert.Equal(t, []string{"run", "ord", "note", "contact"}, withholding().Passed())
}

// A literal from any package that names one aspect keeps the defaults for
// the others (§SD4).
func TestUnspecifiedAspectsTakeTheDefaults(t *testing.T) {
	assert.Equal(t, DefaultAspects(), AspectDefaults{}.Resolved())
	got := AspectDefaults{Secret: AspectActionPass}.Resolved()
	assert.Equal(t, AspectActionPass, got.Secret)
	assert.Equal(t, AspectActionOpaque, got.Encrypted)
	assert.Equal(t, AspectActionPass, got.Pseudonymized)
	assert.Equal(t, "UNSPECIFIED", AspectActionUnspecified.String())
}

// The compiled SQL is deterministic and pinned: the server principal's
// statements and the worker's views for the journal relations and the
// withholding relation.
func TestTheCompiledSQLMatchesItsGolden(t *testing.T) {
	relations := append(JournalRelations(), withholding())
	scopes := testScopes()
	var b strings.Builder
	stmts, err := ServerDDL(Principal{User: "agent42", Profile: "agent42_p", Database: "harness"}, relations, scopes)
	require.NoError(t, err)
	b.WriteString("-- server\n")
	for _, s := range stmts {
		b.WriteString(s + ";\n")
	}
	b.WriteString("-- worker\n")
	for _, r := range relations {
		v, verr := WorkerView(r, scopes)
		require.NoError(t, verr)
		b.WriteString(v + ";\n")
	}
	b.WriteString("-- laws\n")
	w := withholding()
	l11, err := L11(w, scopes)
	require.NoError(t, err)
	l12, err := L12(w, scopes)
	require.NoError(t, err)
	l13, err := L13(w, scopes, "ord")
	require.NoError(t, err)
	b.WriteString(l11 + ";\n" + l12 + ";\n" + l13 + ";\n")
	a, d, err := L18(MyRun(), scopes, "body")
	require.NoError(t, err)
	b.WriteString(a + ";\n" + d + ";\n")
	got := b.String()
	path := filepath.Join("testdata", "sql.golden")
	if os.Getenv("AGENTSCHEMA_UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden yet: run with AGENTSCHEMA_UPDATE_GOLDEN=1")
	assert.Equal(t, string(want), got)

	assert.Contains(t, got, "SQL SECURITY DEFINER DEFINER = `default`")
	assert.Contains(t, got, "IDENTIFIED WITH no_password HOST LOCAL", "the agent principal is reachable from the host alone")
	assert.Contains(t, got, "`custom_agent_run` = 'cell/run-1' READONLY")
	assert.Contains(t, got, "`withheld_memberships`")
	assert.NotContains(t, got, "`secret`", "a dropped section is not in the view")
	assert.Contains(t, got, "length(base.`body`) AS `body_bytes`")
	hist, err := WorkerSelect(AgentHistory(), scopes)
	require.NoError(t, err)
	assert.NotContains(t, hist, "base.`body` AS", "agent_history carries no body")
	conv, err := WorkerSelect(MyConversation(), scopes)
	require.NoError(t, err)
	assert.Contains(t, conv, "AND base.`run` != 'cell/run-1'", "my_conversation leaves the pinned run to my_run")
	_, err = ServerDDL(Principal{}, relations, scopes)
	assert.Error(t, err)
	_, err = ServerDDL(Principal{User: "u\n", Profile: "p"}, relations, scopes)
	assert.Error(t, err, "a principal's identifier is a plain name")
	stmts, err = ServerDDL(Principal{User: "u", Profile: "p", Definer: "de`finer"}, relations, scopes)
	require.NoError(t, err)
	assert.Contains(t, stmts[2], "DEFINER = `de``finer`", "the definer is quoted into the DDL")
}

// The preflight (§SD3): a one-row table under a restrictive policy that
// passes everything counts one row where the backstops read as
// "everything else passes", and the verdict refuses any other count.
func TestThePreflightProbesTheRestrictivePolicyDefault(t *testing.T) {
	setup, probe, teardown := ServerPreflight("harness")
	require.Len(t, setup, 3)
	assert.Contains(t, setup[2], "ON `harness`.`agentschema_preflight` AS RESTRICTIVE USING 1 TO ALL")
	assert.Equal(t, "SELECT count() FROM `harness`.`agentschema_preflight`", probe)
	require.Len(t, teardown, 2)
	assert.True(t, strings.HasPrefix(teardown[1], "DROP TABLE IF EXISTS"))
	_, probe, _ = ServerPreflight("")
	assert.Equal(t, "SELECT count() FROM `agentschema_preflight`", probe)
	assert.NoError(t, PreflightCount(1))
	assert.ErrorContains(t, PreflightCount(0), "users_without_row_policies_can_read_rows")
	assert.Error(t, PreflightCount(2))
}

// The backstops (§SD3) do not AND together: three relations read one base
// table, and each backstop is keyed on its own relation's marker, which
// its definer view sets, so my_conversation and agent_history are not cut
// down to the pinned run (law L18). A pin replaces the profile, the views
// and the policies rather than keeping what an earlier pin left.
func TestEachBackstopIsKeyedOnItsRelation(t *testing.T) {
	stmts, err := ServerDDL(Principal{User: "u", Profile: "p"}, JournalRelations(), testScopes())
	require.NoError(t, err)
	var policies, views []string
	for _, s := range stmts {
		switch {
		case strings.HasPrefix(s, "CREATE ROW POLICY"):
			policies = append(policies, s)
		case strings.HasPrefix(s, "CREATE OR REPLACE VIEW"):
			views = append(views, s)
		case strings.HasPrefix(s, "CREATE SETTINGS PROFILE"):
			assert.Contains(t, s, "CREATE SETTINGS PROFILE OR REPLACE `p`")
			assert.NotContains(t, s, "custom_agent_relation", "the marker is the view's to set, not pinned")
		}
	}
	require.Len(t, policies, 3, "one backstop per relation, all on one base table")
	require.Len(t, views, 3)
	for i, r := range JournalRelations() {
		assert.Contains(t, policies[i], "CREATE ROW POLICY OR REPLACE `"+r.Name+"_backstop` ON `agent_run_entries` AS RESTRICTIVE USING getSettingOrDefault('custom_agent_relation', '') != '"+r.Name+"' OR ")
		assert.NotContains(t, policies[i], "base.", "a row policy has no alias")
		assert.True(t, strings.HasSuffix(views[i], " SETTINGS `custom_agent_relation` = '"+r.Name+"'"), views[i])
	}
	assert.Contains(t, policies[1], "`run` IN (SELECT `id` FROM `agent_runs` WHERE `conversation` = getSettingOrDefault('custom_agent_conversation', ''))) AND `run` != getSettingOrDefault('custom_agent_run', '')")
}

// A scope left unpinned is refused rather than compiled to ”: the view
// would answer nothing without a refusal (§SD2).
func TestAnUnpinnedScopeIsRefused(t *testing.T) {
	scopes := testScopes()
	delete(scopes, ScopeApp)
	_, err := ServerDDL(Principal{User: "u", Profile: "p"}, JournalRelations(), scopes)
	assert.ErrorContains(t, err, "not pinned")
	_, err = WorkerView(AgentHistory(), scopes)
	assert.ErrorContains(t, err, "not pinned")
	_, err = WorkerView(MyRun(), scopes)
	assert.NoError(t, err)
	scopes = testScopes()
	delete(scopes, ScopeSetting)
	_, err = WorkerView(MyConversation(), scopes)
	assert.ErrorContains(t, err, "not pinned", "the setting a relation excludes by is read too")
}

func TestTheRelationsAreAnIntrospectionTable(t *testing.T) {
	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, append(JournalRelations(), withholding())))
	p, ok := reg.Lookup(TableName)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 4, rec.NumRows())
	assert.Equal(t, introspect.FreshnessStatic, p.Freshness())
	col := func(name string) int {
		for i, f := range rec.Schema().Fields() {
			if f.Name == name {
				return i
			}
		}
		return -1
	}
	assert.Equal(t, `[{"section":"contact","memberships":["personal","credential"]}]`, rec.Column(col("withhold")).ValueStr(3))
	assert.Equal(t, ScopeSetting, rec.Column(col("scope_except")).ValueStr(1), "my_conversation excludes the pinned run")
	assert.Equal(t, -1, col("aspects"), "a rule the views do not apply is not advertised")
}

// The journal relations are declared by the columns keelson('agent_run_entries')
// serves (ADR-0308 §SD10); a column agentrun adds or renames changes the
// relations with it.
func TestTheEntryColumnsAreTheJournalTables(t *testing.T) {
	reg := introspect.NewRegistry()
	require.NoError(t, agentrun.RegisterIntrospect(reg, nil))
	p, ok := reg.Lookup(JournalEntries)
	require.True(t, ok)
	var names []string
	for _, f := range p.Schema().Fields() {
		names = append(names, f.Name)
	}
	assert.Equal(t, names, entryColumns)
}
