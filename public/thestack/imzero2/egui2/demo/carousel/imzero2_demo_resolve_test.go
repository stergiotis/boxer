package demo

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

const (
	idWidgets       = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
	idHnExplorer    = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/hn_explorer"
	idLeewaywidgets = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets"
	idPlay          = "github.com/stergiotis/boxer/apps/play"
	idRegexExplorer = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer"
	idImztop        = "github.com/stergiotis/boxer/apps/imztop"
)

// skipIfNoClickhouseLocal lets the SQL launch tests run live where the
// binary is installed and silently skip on minimal CI without it. The
// project memory `reference_clickhouse_local.md` records that
// /usr/bin/clickhouse-local is the expected path on the maintainer's box.
func skipIfNoClickhouseLocal(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(chlocalpool.DefaultBinaryPath); err == nil {
		return
	}
	if _, err := exec.LookPath("clickhouse-local"); err == nil {
		return
	}
	t.Skip("clickhouse-local not found; skipping SQL launch test")
}

func extractIds(apps []app.AppI) (ids []app.AppIdT) {
	ids = make([]app.AppIdT, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.Manifest().Id)
	}
	return
}

func TestExpandLaunchExpr_BareAlias(t *testing.T) {
	assert.Equal(t, "subject_alias = 'play'", expandLaunchExpr("play"))
}

func TestExpandLaunchExpr_BareAliasWithUnderscore(t *testing.T) {
	assert.Equal(t, "subject_alias = 'hn_explorer'", expandLaunchExpr("hn_explorer"))
}

func TestExpandLaunchExpr_BareAliasWithDigit(t *testing.T) {
	// Digits are allowed after the first char by the identifier regex.
	assert.Equal(t, "subject_alias = 'a005'", expandLaunchExpr("a005"))
}

func TestExpandLaunchExpr_TrimsWhitespace(t *testing.T) {
	assert.Equal(t, "subject_alias = 'play'", expandLaunchExpr("  play  "))
}

func TestExpandLaunchExpr_EmptyStaysEmpty(t *testing.T) {
	assert.Equal(t, "", expandLaunchExpr(""))
	assert.Equal(t, "", expandLaunchExpr("   \t\n "))
}

func TestExpandLaunchExpr_SqlExpressionsPassThrough(t *testing.T) {
	cases := []string{
		"subject_alias = 'play'",
		"category = 'tools'",
		"legacy_code IN (1,2,3)",
		"id LIKE '%hn%'",
		"subject_alias = 'play' AND category = 'tools'",
		"legacy_code IS NOT NULL",
	}
	for _, raw := range cases {
		assert.Equal(t, raw, expandLaunchExpr(raw), "raw=%s", raw)
	}
}

func TestExpandLaunchExpr_NonIdentifierPassesThroughVerbatim(t *testing.T) {
	// Comma or hyphen disqualifies the shorthand; the value flows
	// through unmodified and clickhouse-local will reject it.
	assert.Equal(t, "play,widgets", expandLaunchExpr("play,widgets"))
	assert.Equal(t, "time-range", expandLaunchExpr("time-range"))
}

func TestResolveLaunchSql_Shorthand(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	apps, err := resolveLaunchSql("play")
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, app.AppIdT(idPlay), apps[0].Manifest().Id)
}

func TestResolveLaunchSql_SubjectAlias(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	apps, err := resolveLaunchSql("subject_alias = 'play'")
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, app.AppIdT(idPlay), apps[0].Manifest().Id)
}

func TestResolveLaunchSql_LegacyCode(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	apps, err := resolveLaunchSql("legacy_code = 5")
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, app.AppIdT(idPlay), apps[0].Manifest().Id)
}

func TestResolveLaunchSql_FullId(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	apps, err := resolveLaunchSql("id = '" + idWidgets + "'")
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, app.AppIdT(idWidgets), apps[0].Manifest().Id)
}

func TestResolveLaunchSql_InList(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	apps, err := resolveLaunchSql("subject_alias IN ('play','widgets','imztop')")
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]app.AppIdT{idPlay, idWidgets, idImztop},
		extractIds(apps))
}

func TestResolveLaunchSql_LegacyCodeInList(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	apps, err := resolveLaunchSql("legacy_code IN (1, 2, 5)")
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]app.AppIdT{idWidgets, idHnExplorer, idPlay},
		extractIds(apps))
}

func TestResolveLaunchSql_NoMatch(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	apps, err := resolveLaunchSql("subject_alias = 'no-such-app'")
	require.NoError(t, err)
	assert.Empty(t, apps)
}

func TestResolveLaunchSql_SyntaxError(t *testing.T) {
	skipIfNoClickhouseLocal(t)
	_, err := resolveLaunchSql("this is not valid sql")
	require.Error(t, err)
	// The clickhouse-local stderr is folded into the error; the
	// executed query is in a structured field so users see what
	// the runtime asked for.
	assert.Contains(t, err.Error(), "clickhouse-local")
}

func TestResolveLaunchSql_EmptyExpr(t *testing.T) {
	apps, err := resolveLaunchSql("")
	require.NoError(t, err)
	assert.Empty(t, apps)
}

func TestResolveLaunchSql_WhitespaceOnlyExpr(t *testing.T) {
	apps, err := resolveLaunchSql("   \t\n ")
	require.NoError(t, err)
	assert.Empty(t, apps)
}

// TestAdaptToRenderer_MultipleInstancesShareUnderlying documents and
// verifies a known M1/M2 limitation: app.Registry keys by Manifest.Id and
// stores one AppI per id, so two adaptToRenderer closures over the same
// AppI share the same underlying state. Multi-instance per AppId is M3
// dock-host work — until then, launching the same app twice does NOT
// produce two independent instances. EXPLANATION.md records this.
//
// Uses SurfaceHeadless so adaptToRenderer takes the no-wrap path —
// c.Window's KeepIter needs the Rust FFFI runtime which unit tests
// don't bring up. The shared-instance contract is surface-agnostic.
func TestAdaptToRenderer_MultipleInstancesShareUnderlying(t *testing.T) {
	var count int
	m := app.Manifest{
		Id:      "carousel.test.shared",
		Version: "0.1.0",
		Display: "carousel test (shared)",
		Surface: app.SurfaceHeadless,
	}
	a, err := app.NewLegacyFuncApp(m, func() (err error) {
		count++
		return
	})
	require.NoError(t, err)

	r1 := adaptToRenderer(a)
	r2 := adaptToRenderer(a)
	require.NoError(t, r1())
	require.NoError(t, r2())

	// Two closures invoked once each call the same underlying renderer
	// twice — confirming shared state. If a future change introduces
	// per-instance dispatch this assertion will need to update.
	assert.Equal(t, 2, count)
}

func TestRegistry_AllSixAppsRegistered(t *testing.T) {
	// init() in each app's package fires when this test binary's imports
	// transitively pull them in (via the side-effect imports in
	// imzero2_demo_resolve.go).
	wantIds := []app.AppIdT{
		idWidgets,
		idHnExplorer,
		idLeewaywidgets,
		idPlay,
		idRegexExplorer,
		idImztop,
	}
	for _, id := range wantIds {
		_, ok := app.Lookup(id)
		assert.True(t, ok, "expected %s registered", id)
	}
}
