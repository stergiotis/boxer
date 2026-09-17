//go:build integration

package swisstopo

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

// udfAgreementMeters is what the SQL family and the Go functions must agree
// to. Both directions of both tiers land within a nanometre of each other on
// the grid below, so this is a thousandfold of the observed figure: it is set
// to survive a ClickHouse release retuning an elementary function, not to
// leave room for a wrong formula. A real divergence — a mistyped coefficient,
// a dropped iteration, a constant recomputed differently — moves the result by
// millimetres at least, which is still four orders above this floor.
//
// Note what the constant is not: agreement with the *ground*. That is the
// business of the reference-point tests in lv95_test.go and
// lv95_rigorous_test.go, which this lane re-runs through the server.
const udfAgreementMeters = 1e-6

func liveUDFClient(t *testing.T) (client *chclient.Client, ctx context.Context) {
	t.Helper()
	if clickhouseenv.Endpoint.Get() == "" && clickhouseenv.URL.Get() == "" {
		t.Skip("no ClickHouse endpoint configured (CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL); skipping")
	}
	client = chclient.New(chclient.ConfigFromEnv(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Ping(ctx))
	for _, stmt := range StatementsSQL() {
		require.NoError(t, client.Exec(ctx, stmt), stmt)
	}
	return
}

// sqlFloat renders a float64 as a literal the server parses back to the same
// double, so a disagreement is the transform's and never the wire's.
func sqlFloat(v float64) (s string) {
	s = strconv.FormatFloat(v, 'g', -1, 64)
	return
}

// queryFloatRows runs sql and decodes the TabSeparated body as floats. The
// first column is the caller's row index, which the query orders by, so the
// result lines up with the input slice whatever the server's read order was.
func queryFloatRows(t *testing.T, ctx context.Context, client *chclient.Client, sql string, want int) (rows [][]float64) {
	t.Helper()
	body, err := client.Query(ctx, sql)
	require.NoError(t, err, sql)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)

	rows = make([][]float64, 0, want)
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Split(line, "\t")
		vals := make([]float64, 0, len(fields)-1)
		for _, f := range fields[1:] {
			v, perr := strconv.ParseFloat(f, 64)
			require.NoError(t, perr, "field %q of %q", f, line)
			vals = append(vals, v)
		}
		rows = append(rows, vals)
	}
	require.Len(t, rows, want)
	return
}

// pointQuery wraps a pair-valued input as an indexed row source: one row per
// point, ordered, with the pair bound to `a` and `b`.
func pointQuery(exprs string, pairs [][2]float64) (sql string) {
	var lits strings.Builder
	for i, p := range pairs {
		if i > 0 {
			lits.WriteString(", ")
		}
		lits.WriteString("(" + sqlFloat(p[0]) + ", " + sqlFloat(p[1]) + ")")
	}
	sql = "SELECT i, " + exprs +
		" FROM (SELECT arrayJoin(arrayEnumerate(pts)) AS i, pts[i].1 AS a, pts[i].2 AS b" +
		" FROM (SELECT [" + lits.String() + "] AS pts)) ORDER BY i"
	return
}

// swissGrid is a lattice over the national extent plus the reference points,
// which is where the two tiers are meant to hold and where a coefficient typo
// shows up as a smooth field of small errors rather than one bad corner.
func swissGrid() (pts [][2]float64) {
	pts = make([][2]float64, 0, 128)
	for e := 2_480_000.0; e <= 2_840_000.0; e += 40_000 {
		for n := 1_075_000.0; n <= 1_300_000.0; n += 25_000 {
			pts = append(pts, [2]float64{e, n})
		}
	}
	for _, rp := range referencePoints {
		pts = append(pts, [2]float64{rp.lv95.E, rp.lv95.N})
	}
	return
}

// The family installs, and the server's copy of it is the whole family: the
// namespace is what makes that question answerable in one query.
func TestIntegrationUDFsInstalled(t *testing.T) {
	client, ctx := liveUDFClient(t)
	body, err := client.Query(ctx,
		`SELECT name FROM system.functions WHERE name LIKE 'SWISSTOPO\_%' ORDER BY name`)
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)

	installed := make(map[string]bool, 32)
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		installed[strings.TrimSpace(line)] = true
	}
	for _, name := range UDFNames() {
		assert.True(t, installed[name], "%s is declared but not installed", name)
	}
}

// Both tiers of LV95 -> WGS84, against the Go functions they are the twin of.
func TestIntegrationUDFsLV95ToWGS84MatchGo(t *testing.T) {
	client, ctx := liveUDFClient(t)
	pts := swissGrid()
	rows := queryFloatRows(t, ctx, client, pointQuery(
		"SWISSTOPO_LV95_TO_WGS84_EXACT(a, b).lat, SWISSTOPO_LV95_TO_WGS84_EXACT(a, b).lon, "+
			"SWISSTOPO_LV95_TO_WGS84(a, b).lat, SWISSTOPO_LV95_TO_WGS84(a, b).lon",
		pts), len(pts))

	for i, p := range pts {
		lv := LV95Coord{E: p[0], N: p[1]}
		gotExact := WGS84Coord{Lat: rows[i][0], Lon: rows[i][1]}
		gotDefault := WGS84Coord{Lat: rows[i][2], Lon: rows[i][3]}
		assert.LessOrEqual(t, distanceMeters(gotExact, LV95ToWGS84Rigorous(lv)), udfAgreementMeters,
			"exact tier at %v: SQL %v", lv, gotExact)
		assert.LessOrEqual(t, distanceMeters(gotDefault, LV95ToWGS84(lv)), udfAgreementMeters,
			"default tier at %v: SQL %v", lv, gotDefault)
	}
}

// Both tiers of the inverse. The inputs are the grid carried through the Go
// forward transform, so the pairs exercised are the ones the family will see.
func TestIntegrationUDFsWGS84ToLV95MatchGo(t *testing.T) {
	client, ctx := liveUDFClient(t)
	grid := swissGrid()
	pts := make([][2]float64, 0, len(grid))
	for _, p := range grid {
		w := LV95ToWGS84Rigorous(LV95Coord{E: p[0], N: p[1]})
		pts = append(pts, [2]float64{w.Lat, w.Lon})
	}
	rows := queryFloatRows(t, ctx, client, pointQuery(
		"SWISSTOPO_WGS84_TO_LV95_EXACT(a, b).e, SWISSTOPO_WGS84_TO_LV95_EXACT(a, b).n, "+
			"SWISSTOPO_WGS84_TO_LV95(a, b).e, SWISSTOPO_WGS84_TO_LV95(a, b).n",
		pts), len(pts))

	for i, p := range pts {
		wgs := WGS84Coord{Lat: p[0], Lon: p[1]}
		wantExact := WGS84ToLV95Rigorous(wgs)
		wantDefault := WGS84ToLV95(wgs)
		assert.InDelta(t, wantExact.E, rows[i][0], udfAgreementMeters, "exact E at %v", wgs)
		assert.InDelta(t, wantExact.N, rows[i][1], udfAgreementMeters, "exact N at %v", wgs)
		assert.InDelta(t, wantDefault.E, rows[i][2], udfAgreementMeters, "default E at %v", wgs)
		assert.InDelta(t, wantDefault.N, rows[i][3], udfAgreementMeters, "default N at %v", wgs)
	}
}

// The REFRAME reference points, re-run through the server. This is the lane's
// ground truth: it holds the SQL to the same tolerances the Go tests hold the
// formulas to, so the family cannot pass by agreeing with a broken Go twin.
func TestIntegrationUDFsReferencePoints(t *testing.T) {
	client, ctx := liveUDFClient(t)
	pts := make([][2]float64, 0, len(referencePoints))
	for _, rp := range referencePoints {
		pts = append(pts, [2]float64{rp.lv95.E, rp.lv95.N})
	}
	rows := queryFloatRows(t, ctx, client, pointQuery(
		"SWISSTOPO_LV95_TO_WGS84_EXACT(a, b).lat, SWISSTOPO_LV95_TO_WGS84_EXACT(a, b).lon, "+
			"SWISSTOPO_LV95_TO_WGS84(a, b).lat, SWISSTOPO_LV95_TO_WGS84(a, b).lon",
		pts), len(pts))

	for i, rp := range referencePoints {
		t.Run(rp.name, func(t *testing.T) {
			exact := WGS84Coord{Lat: rows[i][0], Lon: rows[i][1]}
			dflt := WGS84Coord{Lat: rows[i][2], Lon: rows[i][3]}
			assert.LessOrEqual(t, distanceMeters(exact, rp.wgs), rigorousToleranceMeters,
				"exact tier: got %v want %v", exact, rp.wgs)
			assert.LessOrEqual(t, distanceMeters(dflt, rp.wgs), rp.tolerance,
				"default tier: got %v want %v", dflt, rp.wgs)
		})
	}
}

// LV03 is the nominal frame offset and nothing else — the property the header
// of lv95_udfs.sql warns about, pinned so it cannot quietly grow a fudge.
func TestIntegrationUDFsLV03IsTheNominalOffset(t *testing.T) {
	client, ctx := liveUDFClient(t)
	pts := [][2]float64{{600_000, 200_000}, {679_520.05, 12_273.44}, {480_000, 75_000}}
	rows := queryFloatRows(t, ctx, client, pointQuery(
		"SWISSTOPO_LV03_TO_LV95(a, b).e, SWISSTOPO_LV03_TO_LV95(a, b).n, "+
			"SWISSTOPO_LV95_TO_LV03(SWISSTOPO_LV03_TO_LV95(a, b).e, SWISSTOPO_LV03_TO_LV95(a, b).n).y, "+
			"SWISSTOPO_LV95_TO_LV03(SWISSTOPO_LV03_TO_LV95(a, b).e, SWISSTOPO_LV03_TO_LV95(a, b).n).x, "+
			"SWISSTOPO_LV03_TO_WGS84_EXACT(a, b).lat, SWISSTOPO_LV03_TO_WGS84_EXACT(a, b).lon, "+
			"SWISSTOPO_LV03_TO_WGS84(a, b).lat, SWISSTOPO_LV03_TO_WGS84(a, b).lon",
		pts), len(pts))

	for i, p := range pts {
		assert.Equal(t, p[0]+2_000_000, rows[i][0], "the offset is applied to the bit")
		assert.Equal(t, p[1]+1_000_000, rows[i][1])
		// Not to the bit, though: carrying a metre-fraction through ±2e6 and
		// back rounds it, at the scale of an ulp of the shifted value.
		assert.InDelta(t, p[0], rows[i][2], udfAgreementMeters, "LV95 -> LV03 must undo LV03 -> LV95")
		assert.InDelta(t, p[1], rows[i][3], udfAgreementMeters)

		// Each composition must carry its own half's tier, which is the one
		// thing a suffixed pair of wrappers can get wrong.
		lv := LV95Coord{E: p[0] + 2_000_000, N: p[1] + 1_000_000}
		gotExact := WGS84Coord{Lat: rows[i][4], Lon: rows[i][5]}
		gotDefault := WGS84Coord{Lat: rows[i][6], Lon: rows[i][7]}
		assert.LessOrEqual(t, distanceMeters(gotExact, LV95ToWGS84Rigorous(lv)), udfAgreementMeters,
			"LV03 -> WGS84 _EXACT must compose the offset with the exact tier")
		assert.LessOrEqual(t, distanceMeters(gotDefault, LV95ToWGS84(lv)), udfAgreementMeters,
			"LV03 -> WGS84 must compose the offset with the default tier")
	}
}

// The derived constants, checked where they are cheapest to localise. A drift
// here is a wrong projection everywhere, and the failure names the constant
// instead of a field of displaced points.
//
// The tolerance is relative and loose for one reason: SWISSTOPO_C_K cancels
// two quantities near 0.9 down to 0.003, and the SQL reaches it through
// atanh∘sin where the Go reaches it through log∘tan. They agree to ~8e-16
// absolute, which is the cancellation, not the bit.
func TestIntegrationUDFsConstantsMatchGo(t *testing.T) {
	client, ctx := liveUDFClient(t)
	rows := queryFloatRows(t, ctx, client,
		"SELECT 0 AS i, SWISSTOPO_C_BESSEL_E2(), SWISSTOPO_C_BESSEL_E(), SWISSTOPO_C_WGS84_E2(), "+
			"SWISSTOPO_C_ALPHA(), SWISSTOPO_C_B0(), SWISSTOPO_C_K(), SWISSTOPO_C_R(), SWISSTOPO_C_LAMBDA0()", 1)

	for _, c := range []struct {
		name string
		want float64
		got  float64
	}{
		{"besselE2", swissProjection.besselE2, rows[0][0]},
		{"besselE", swissProjection.e, rows[0][1]},
		{"wgs84E2", swissProjection.wgs84E2, rows[0][2]},
		{"alpha", swissProjection.alpha, rows[0][3]},
		{"b0", swissProjection.b0, rows[0][4]},
		{"k", swissProjection.k, rows[0][5]},
		{"r", swissProjection.r, rows[0][6]},
		{"lambda0", swissProjection.lambda0, rows[0][7]},
	} {
		assert.InEpsilon(t, c.want, c.got, 1e-11, "constant %s", c.name)
	}
}
