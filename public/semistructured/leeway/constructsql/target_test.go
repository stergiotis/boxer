package constructsql_test

// ADR-0181 §SD8 M2: target adoption and the target shape check. The events
// fixture spells its sections camelCase (`geoPoint`), so a resolved mint's
// spelling is observable — an adopted name carries the fixture's case and
// aspect hints, a composed one is folded and aspect-free.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
)

func TestTargetAdoptionResolvesAgainstTheTarget(t *testing.T) {
	r := extractResolver(t)
	lanes, ok := r.ExtractLanesFor("", extractTable, "geoPoint")
	require.True(t, ok)
	lat, err := lanes.ValueColumnFor("lat")
	require.NoError(t, err)
	ch, err := lanes.ChannelFor("low-card-verbatim")
	require.NoError(t, err)

	pass := constructsql.ExpandPassWithTargetAdoption(r, "")
	out, err := pass.Run("INSERT INTO events SELECT " +
		"LW_TV(f, 'geo_point', 'lat', 'f64'), " +
		"LW_TV_MEMB(m, 'geo_point', 'low-card-verbatim'), " +
		"LW_TV_SUPPORT(k, 'geo_point', 'lvcard') " +
		"FROM events")
	require.NoError(t, err)
	// Spelled `geo_point` in the calls, resolved to the target's own
	// camelCase physicals — spelling reconciliation is the resolver's fold.
	require.Contains(t, out, `AS "`+lat.Physical+`"`)
	require.Contains(t, out, `AS "`+ch.Ident+`"`)
	require.Contains(t, out, `AS "`+ch.Card+`"`)
	require.Contains(t, out, "geoPoint", "adopted names keep the target's case")
	require.NotContains(t, out, "geo-point", "no folded fresh-table mint under a resolved target")
}

func TestTargetAdoptionPlainAndMiss(t *testing.T) {
	r := extractResolver(t)
	pass := constructsql.ExpandPassWithTargetAdoption(r, "")

	// The fixture's plain entity id adopts through the handle path.
	idHandle := r.Resolve("", extractTable, "id:id")
	require.Equal(t, passes.ResolveOK, idHandle.Kind)
	out, err := pass.Run("INSERT INTO events SELECT LW_PLAIN(x, 'id', 'u64', 'item:id') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, `AS "`+idHandle.Physical[0]+`"`)

	// A mint the target does not carry keeps its composition — the loud
	// verdict on a true miss is the shape check's, not the mint's.
	out, err = pass.Run("INSERT INTO events SELECT LW_TV(v, 'geo_point', 'altitude', 'f64') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, `"tv:geo-point:altitude:val:f64`, "a miss composes the fresh-table name")
}

func TestTargetAdoptionOnlyUnderAResolvedWrapper(t *testing.T) {
	r := extractResolver(t)
	pass := constructsql.ExpandPassWithTargetAdoption(r, "")

	// No wrapper: identical to the unbound pass.
	sql := "SELECT LW_TV(v, 'geo_point', 'lat', 'f64') FROM events"
	bound, err := pass.Run(sql)
	require.NoError(t, err)
	unbound, err := constructsql.ExpandPass.Run(sql)
	require.NoError(t, err)
	require.Equal(t, unbound, bound)

	// A wrapper whose target the schema does not carry: fresh-table mint.
	out, err := pass.Run("INSERT INTO nosuch SELECT LW_TV(v, 'geo_point', 'lat', 'f64') FROM events")
	require.NoError(t, err)
	require.Contains(t, out, `"tv:geo-point:lat:val:f64`)
}

func TestShapeCheckTargetContainmentAndList(t *testing.T) {
	names := extractFixture(t)
	provider := passes.NewStaticSchemaProvider(map[string][]string{"db.events": names})
	check := constructsql.ShapeCheckPassWithTarget(provider, "db")

	r := extractResolver(t)
	lanes, ok := r.ExtractLanesFor("", extractTable, "symbol")
	require.True(t, ok)
	value, err := lanes.ValueColumnFor("")
	require.NoError(t, err)
	q := func(s string) string { return `"` + s + `"` }

	// Containment holds — the analytical pass returns its input.
	sql := "INSERT INTO db.events SELECT src." + q(value.Physical) + " FROM db.events AS src"
	out, err := check.Run(sql)
	require.NoError(t, err)
	require.Equal(t, sql, out)

	// A column the target does not carry is a true miss.
	_, err = check.Run(`INSERT INTO db.events SELECT x AS "tv:nosuch:value:val:s::::0::" FROM db.events`)
	require.ErrorContains(t, err, "the SELECT outputs a column the target does not carry")

	// The column list correlates positionally, fold-equivalent.
	_, err = check.Run("INSERT INTO db.events (" + q(value.Physical) + ", " + q(lanes.Channels[0].Ident) + ") " +
		"SELECT " + q(lanes.Channels[0].Ident) + ", " + q(value.Physical) + " FROM db.events")
	require.ErrorContains(t, err, "disagree at this position")

	// Arity mismatch against the list.
	_, err = check.Run("INSERT INTO db.events (" + q(value.Physical) + ") " +
		"SELECT " + q(value.Physical) + ", " + q(lanes.Channels[0].Ident) + " FROM db.events")
	require.ErrorContains(t, err, "more columns than the column list names")

	// An unknown target cannot be verified, loudly.
	_, err = check.Run("INSERT INTO db.nosuch SELECT 1 FROM db.events")
	require.ErrorContains(t, err, "not in the bound schema")

	// Without a wrapper the pass falls back to the closure check.
	_, err = check.Run("SELECT 1 + 1 FROM db.events")
	require.ErrorContains(t, err, "closure rule")
}
