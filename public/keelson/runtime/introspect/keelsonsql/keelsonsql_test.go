package keelsonsql

import (
	"testing"

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
		{"SELECT 1", "SELECT 1"},                            // no macro — unchanged
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
