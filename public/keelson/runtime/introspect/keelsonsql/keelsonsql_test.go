package keelsonsql

import (
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

func testReg(t *testing.T) *introspect.Registry {
	t.Helper()
	r := introspect.NewRegistry()
	require.NoError(t, providers.RegisterStatic(r)) // env, apps, build, sbom
	return r
}

func TestRewriteToBare(t *testing.T) {
	r := testReg(t)
	cases := []struct{ in, want string }{
		{"SELECT * FROM keelson('env')", "SELECT * FROM env"},
		{"SELECT name FROM keelson('env') AS e", "SELECT name FROM env AS e"}, // alias preserved
		{"SELECT * FROM keelson('env') JOIN keelson('apps') ON 1", "SELECT * FROM env JOIN apps ON 1"},
		{"SELECT * FROM keelson(env)", "SELECT * FROM env"}, // bare-identifier arg form
		{"SELECT 1", "SELECT 1"}, // no macro — unchanged
	}
	for _, tc := range cases {
		got, err := RewriteToBare(r, tc.in)
		require.NoError(t, err, "in=%q", tc.in)
		assert.Equal(t, tc.want, got, "in=%q", tc.in)
	}
}

func TestRewriteToURL(t *testing.T) {
	r := testReg(t)
	got, err := RewriteToURL(r, "http://127.0.0.1:8097/", "SELECT count() FROM keelson('env')")
	require.NoError(t, err)
	// trailing slash on the base is trimmed; url() + ArrowStream injected.
	assert.Equal(t, "SELECT count() FROM url('http://127.0.0.1:8097/table/env', 'ArrowStream')", got)
}

func TestRewriteUnknownTableErrors(t *testing.T) {
	r := testReg(t)
	_, err := RewriteToBare(r, "SELECT * FROM keelson('bogus')")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown keelson table")
}

func TestRewriteArityErrors(t *testing.T) {
	r := testReg(t)
	for _, in := range []string{"SELECT * FROM keelson('env','x')", "SELECT * FROM keelson()"} {
		_, err := RewriteToBare(r, in)
		assert.Error(t, err, "in=%q", in)
	}
}

func TestRewriteScalarKeelsonUntouched(t *testing.T) {
	r := testReg(t)
	// keelson('env') in SELECT (scalar) position is not a table function;
	// the pass must leave it alone even though 'env' is a known table.
	const in = "SELECT keelson('env')"
	got, err := RewriteToBare(r, in)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestRewriteAliases(t *testing.T) {
	b := map[string]string{"items": "adhoc_deadbeef01234567"}

	// A bound alias is rewritten to its handle; the alias literal is gone.
	got := RewriteAliases("SELECT * FROM keelson('items') ORDER BY x", b)
	assert.Contains(t, got, "keelson('adhoc_deadbeef01234567')")
	assert.NotContains(t, got, "'items'")

	// An unbound name passes through untouched.
	assert.Equal(t, "SELECT * FROM keelson('env')", RewriteAliases("SELECT * FROM keelson('env')", b))

	// No bindings is the identity.
	assert.Equal(t, "SELECT 1", RewriteAliases("SELECT 1", nil))

	// A parse failure passes through rather than erroring.
	assert.Equal(t, "NOT SQL ((", RewriteAliases("NOT SQL ((", b))

	// Mixed: only the bound one moves.
	got = RewriteAliases("SELECT (SELECT count() FROM keelson('items')) + (SELECT count() FROM keelson('env'))", b)
	assert.Contains(t, got, "keelson('adhoc_deadbeef01234567')")
	assert.Contains(t, got, "keelson('env')")

	// A bare, unqualified relation named like a bound alias is rewritten
	// too — the placement wall inspects both spellings, so an alias must
	// reach it under neither (ADR-0240 §SD7) — and keeps its SQL alias.
	got = RewriteAliases("SELECT i.x FROM items AS i JOIN other ON other.k = i.k", b)
	assert.Contains(t, got, "FROM keelson('adhoc_deadbeef01234567') AS i")
	assert.Contains(t, got, "JOIN other ON", "an unbound relation passes through")
	assert.NotContains(t, got, "FROM items")

	// A qualified name, a CTE and a table function of that name are not
	// aliases and are left alone.
	assert.Equal(t, "SELECT * FROM db.items", RewriteAliases("SELECT * FROM db.items", b))
	assert.Equal(t, "WITH items AS (SELECT 1) SELECT * FROM items", RewriteAliases("WITH items AS (SELECT 1) SELECT * FROM items", b))
	assert.Equal(t, "SELECT * FROM items(3)", RewriteAliases("SELECT * FROM items(3)", b))
}

func TestReferences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"quoted arg", "SELECT * FROM keelson('env')", []string{"env"}},
		{"bare identifier arg", "SELECT * FROM keelson(env)", []string{"env"}},
		{"case-insensitive macro name", "SELECT * FROM KEELSON('env')", []string{"env"}},
		{"join", "SELECT * FROM keelson('env') JOIN keelson('apps') ON 1", []string{"env", "apps"}},
		{"subquery", "SELECT * FROM (SELECT name FROM keelson('env'))", []string{"env"}},
		{"union", "SELECT name FROM keelson('env') UNION ALL SELECT name FROM keelson('apps')", []string{"env", "apps"}},
		{"cte", "WITH e AS (SELECT * FROM keelson('env')) SELECT * FROM e", []string{"env"}},
		{"deduplicated, first-appearance order", "SELECT * FROM keelson('apps') JOIN keelson('env') ON 1 JOIN keelson('apps') AS a2 ON 1", []string{"apps", "env"}},
		{"no macro", "SELECT * FROM system.tables", nil},
		{"qualified table is not a macro call", "SELECT * FROM keelson.env", nil},
		{"scalar position is not a macro call", "SELECT keelson('env')", nil},
		{"parse failure", "NOT SQL ((", nil},
		{"malformed calls skipped", "SELECT * FROM keelson('env','x') JOIN keelson() ON 1", nil},
		{"malformed call skipped, well-formed sibling kept", "SELECT * FROM keelson('env') JOIN keelson('a','b') ON 1", []string{"env"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, References(tc.in))
		})
	}
}

func TestReferencesIsRegistryIndependent(t *testing.T) {
	// References states a fact about the SQL, so an unregistered name is
	// reported exactly like a registered one — validation belongs to expand.
	assert.Equal(t, []string{"bogus"}, References("SELECT * FROM keelson('bogus')"))
}

func TestRewriteToURLEncryptedDataset(t *testing.T) {
	r := testReg(t)
	require.NoError(t, r.Register(&sealedStub{name: "adhoc_deadbeef01234567", structure: "id Int64, ts DateTime64(6,'UTC')", revision: 1}))

	// An ad-hoc dataset gets the 3-arg url() with its explicit structure;
	// the 'UTC' quotes inside the structure are escaped.
	got, err := RewriteToURL(r, "http://127.0.0.1:8097/", "SELECT * FROM keelson('adhoc_deadbeef01234567')")
	require.NoError(t, err)
	assert.Equal(t,
		`SELECT * FROM url('http://127.0.0.1:8097/table/adhoc_deadbeef01234567', 'ArrowStream', 'id Int64, ts DateTime64(6,\'UTC\')')`,
		got)

	// A regular introspection table still gets the 2-arg (inferred) form.
	got, err = RewriteToURL(r, "http://127.0.0.1:8097/", "SELECT * FROM keelson('env')")
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM url('http://127.0.0.1:8097/table/env', 'ArrowStream')", got)
}

// The split rewrite sends a sealed dataset to url() and an ordinary table
// to its bare name, in one statement.
func TestRewriteSplit(t *testing.T) {
	r := testReg(t)
	require.NoError(t, r.Register(&sealedStub{name: "adhoc_deadbeef01234567", structure: "id Int64", revision: 1}))
	got, err := RewriteSplit(r, "http://127.0.0.1:8097/",
		"SELECT e.name, d.id FROM keelson('env') e JOIN keelson('adhoc_deadbeef01234567') d ON 1 = 1")
	require.NoError(t, err)
	assert.Equal(t,
		`SELECT e.name, d.id FROM env e JOIN url('http://127.0.0.1:8097/table/adhoc_deadbeef01234567', 'ArrowStream', 'id Int64') d ON 1 = 1`,
		got)
	_, err = RewriteSplit(r, "http://127.0.0.1:8097/", "SELECT * FROM keelson('bogus')")
	require.Error(t, err)
}

// sealedStub is the smallest introspect.EncryptedDatasetI: enough for the
// rewrite to see a sealed provider and read its structure.
type sealedStub struct {
	name      string
	structure string
	revision  uint64
}

func (s *sealedStub) Name() string                         { return s.name }
func (s *sealedStub) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (s *sealedStub) Schema() *arrow.Schema                { return arrow.NewSchema(nil, nil) }
func (s *sealedStub) Structure() string                    { return s.structure }
func (s *sealedStub) Revision() uint64                     { return s.revision }
func (s *sealedStub) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (s *sealedStub) Open() (io.ReadSeekCloser, uint64, error) { return nil, 0, assert.AnError }
