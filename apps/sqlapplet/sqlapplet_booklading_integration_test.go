//go:build integration

package sqlapplet

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// TestLadingBookQueries_LiveServer runs every chapter of the lading book
// against a reachable server, through the two client-side expansions a play
// host would apply — the lading macros (ADR-0198 §SD7, ADR-0200 §SD6) and the
// component projection (ADR-0189) — with the remaining knobs bound as query
// parameters. It asserts that each chapter is a statement the server accepts;
// row counts are whatever the store holds.
func TestLadingBookQueries_LiveServer(t *testing.T) {
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
	present, err := query("SELECT count() FROM system.tables WHERE database = '"+
		ladingschema.DatabaseName+"' AND name = '"+ladingschema.TableNameSnap+"' FORMAT TabSeparated", nil)
	require.NoError(t, err)
	if present == "0" {
		t.Skipf("%s.%s does not exist; provision the lading store first", ladingschema.DatabaseName, ladingschema.TableNameSnap)
	}

	comps := componentsql.NewRegistry()
	require.NoError(t, comps.Register(ladingpolicy.PolicyComponentSQL))
	componentPass := constructsql.ComponentExpandPass(comps, "")
	ladingCfg := ladingsql.Config{Visibility: ladingsql.VisibleAll{}}

	bySlug := ladingDefsBySlug(t)
	for slug, d := range bySlug {
		t.Run(slug, func(t *testing.T) {
			expanded, xErr := ladingsql.Expand(ladingCfg, d.SQL)
			require.NoError(t, xErr)
			expanded, xErr = componentPass.Run(expanded)
			require.NoError(t, xErr)
			residual, params, exErr := play.ExtractParams(expanded)
			require.NoError(t, exErr)
			_, qErr := query(residual+"\nFORMAT TabSeparated", stripParamPrefix(params))
			require.NoErrorf(t, qErr, "%s failed:\n%s", slug, residual)
		})
	}

	// As a window builds it: play's client takes the `SET param_…` prelude off
	// for the URL channel before its pre-execute stage, the order the
	// expansion above does not exercise. A macro whose mount is a slot reads
	// the prelude, so this is where an applet that passed above still sent
	// `fs(…)` to the server unexpanded.
	play.RegisterHostSql(zerolog.Nop())
	for slug, d := range bySlug {
		t.Run(slug+"/as-a-window-builds-it", func(t *testing.T) {
			cl := play.NewClient(play.ClientConfig{URL: chclient.ConfigFromEnv().URL}, nil)
			_ = play.NewLivePlayApp(cl, d.SQL, 1, nil)
			body, params := cl.BuildStatement(d.SQL)
			_, qErr := query(body, stripParamPrefix(params))
			require.NoErrorf(t, qErr, "%s failed:\n%s", slug, body)
		})
	}
}
