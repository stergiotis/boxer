//go:build integration

package sqlapplet

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chviews"
)

// TestLeewayBookQueries_LiveServer runs every chapter buffer verbatim against a
// real ClickHouse. These chapters read the ADR-0226 views on the configured
// endpoint, so a live server is the only place they can run — and the views are
// generated SQL over generated SQL, which a Go test can check the shape of and
// not the meaning.
//
// It skips when the surface is not installed rather than installing it: an
// install writes functions and a database into somebody's server, which is not
// a side effect a test lane should have. `boxer leeway sqlsurface install` is
// the one-liner the skip names.
func TestLeewayBookQueries_LiveServer(t *testing.T) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable: %v", err)
	}

	query := func(sql string, params map[string]string) (out string, err error) {
		body, err := client.QueryParams(ctx, sql, params)
		if err != nil {
			return "", err
		}
		defer func() { _ = body.Close() }()
		raw, err := io.ReadAll(body)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}

	target := chviews.TargetDatabase("")
	names := chviews.AllViewNames()
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, "'"+n+"'")
	}
	present, err := query("SELECT count() FROM system.tables WHERE database = '"+target.Name()+
		"' AND name IN ("+strings.Join(quoted, ", ")+") FORMAT TabSeparated", nil)
	require.NoError(t, err)
	if present != "3" {
		t.Skipf("%s carries %s of %d views; run `boxer leeway sqlsurface install` first",
			target.Name(), present, len(names))
	}

	bySlug := leewayDefsBySlug(t)
	for slug, def := range bySlug {
		t.Run(slug, func(t *testing.T) {
			// A buffer's leading `SET param_*` prelude is not part of the
			// statement: play harvests it into the HTTP parameter channel and
			// ships the residual alone, because ClickHouse rejects a
			// multi-statement body.
			residual, params, exErr := play.ExtractParams(def.SQL)
			require.NoError(t, exErr)
			out, qErr := query(residual+"\nFORMAT TabSeparated", stripParamPrefix(params))
			require.NoErrorf(t, qErr, "%s failed:\n%s", slug, def.SQL)
			// Every chapter describes the server's own schema, and a server
			// carrying the views carries at least the views' own columns — so
			// unlike a book over derived data, empty here is a defect.
			assert.NotEmptyf(t, out, "%s produced no rows", slug)
		})
	}
}

// TestLeewayBookKnobs_LiveServer exercises the knobs away from their defaults.
// A default that happens to work says nothing about a prelude whose other
// values name a column that does not exist — which is the failure mode of a
// knob spliced into a projection rather than a predicate, as lw-aspects' is.
func TestLeewayBookKnobs_LiveServer(t *testing.T) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	ctx := context.Background()
	if err := client.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable: %v", err)
	}
	target := chviews.TargetDatabase("")
	probe, err := client.QueryParams(ctx, "SELECT count() FROM system.tables WHERE database = '"+
		target.Name()+"' AND name = '"+chviews.ViewColumns+"' FORMAT TabSeparated", nil)
	require.NoError(t, err)
	raw, _ := io.ReadAll(probe)
	_ = probe.Close()
	if strings.TrimSpace(string(raw)) != "1" {
		t.Skip("the leeway views are not installed; run `boxer leeway sqlsurface install` first")
	}

	bySlug := leewayDefsBySlug(t)
	run := func(t *testing.T, slug string, overrides map[string]string) (out string) {
		t.Helper()
		residual, params, exErr := play.ExtractParams(bySlug[slug].SQL)
		require.NoError(t, exErr)
		bound := stripParamPrefix(params)
		for k, v := range overrides {
			bound[k] = v
		}
		body, qErr := client.QueryParams(ctx, residual+"\nFORMAT TabSeparated", bound)
		require.NoErrorf(t, qErr, "%s with %v failed", slug, overrides)
		defer func() { _ = body.Close() }()
		b, rErr := io.ReadAll(body)
		require.NoError(t, rErr)
		return strings.TrimSpace(string(b))
	}

	// Each vocabulary is a different column of the view, selected by the knob.
	for _, v := range []string{"enc", "sem", "use"} {
		assert.NotEmptyf(t, run(t, "lw-aspects", map[string]string{"v": v}),
			"lw-aspects produced no rows for v=%q", v)
	}
	// The floor is a threshold, so the two ends are what matter: a floor of 1
	// draws every pair sharing anything, and an unreachably high one draws
	// none without erroring.
	loose := run(t, "lw-affinity", map[string]string{"floor": "1"})
	assert.NotEmpty(t, loose, "lw-affinity at floor=1 found no pair")
	assert.Empty(t, run(t, "lw-affinity", map[string]string{"floor": "10000"}),
		"lw-affinity at an unreachable floor must draw nothing, not fail")
	// The patterns narrow rather than filter to nothing.
	assert.NotEmpty(t, run(t, "lw-anatomy", map[string]string{"db": "%", "tbl": "%"}))
}
