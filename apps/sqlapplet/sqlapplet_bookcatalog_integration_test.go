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
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

// TestCatalogBookQueries_LiveServer runs every chapter buffer verbatim against
// a real ClickHouse, which is the only place they can run: unlike every other
// gov book these read `boxer.tables_*` on the configured endpoint rather than
// the introspection plane.
//
// It skips when the catalog has never been built, because a book over derived
// data cannot be tested without the data — run `boxer datacatalog refresh`
// first. Skipping rather than building is deliberate: a refresh replaces the
// instance's catalog whole, which is not a side effect a test lane should have.
func TestCatalogBookQueries_LiveServer(t *testing.T) {
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
		datacatalog.DatabaseName+"' AND name = '"+datacatalog.TableCatalog+"' FORMAT TabSeparated", nil)
	require.NoError(t, err)
	if present == "0" {
		t.Skipf("%s does not exist; run `boxer datacatalog refresh` first",
			datacatalog.Qualified(datacatalog.TableCatalog))
	}

	bySlug := catalogDefsBySlug(t)
	for _, slug := range []string{"cat-overview", "cat-shapes", "cat-unmatched"} {
		t.Run(slug, func(t *testing.T) {
			// A buffer's leading `SET param_*` prelude is not part of the
			// statement: play harvests it into the HTTP parameter channel and
			// ships the residual alone, because ClickHouse rejects a
			// multi-statement body. Running the raw text here would fail for
			// that reason rather than for anything about the chapter.
			residual, params, exErr := play.ExtractParams(bySlug[slug].SQL)
			require.NoError(t, exErr)
			// ExtractParams keeps the SET name verbatim (`param_x`) while
			// chclient adds the URL-side `param_` marker itself; handing the
			// map over unstripped asks the server for `{param_x:…}`.
			out, qErr := query(residual+"\nFORMAT TabSeparated", stripParamPrefix(params))
			require.NoErrorf(t, qErr, "%s failed:\n%s", slug, bySlug[slug].SQL)
			// cat-unmatched may legitimately be empty — an instance where every
			// opaque table matched something is a good instance, not a broken
			// chapter — so only the two that describe the catalog itself must
			// produce rows.
			if slug != "cat-unmatched" {
				assert.NotEmptyf(t, out, "%s produced no rows", slug)
			}
		})
	}
}

// stripParamPrefix turns ExtractParams' `param_<x>` keys into the `<x>`
// chclient re-prefixes.
func stripParamPrefix(params map[string]string) (out map[string]string) {
	out = make(map[string]string, len(params))
	for k, v := range params {
		out[strings.TrimPrefix(k, "param_")] = v
	}
	return
}
