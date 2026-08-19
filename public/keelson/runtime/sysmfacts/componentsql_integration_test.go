//go:build integration

package sysmfacts_test

import (
	"context"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveStore binds the generated store to the CLICKHOUSE_* server, skipping
// when it is unreachable.
func liveStore(t *testing.T) (*sysmfacts.SysmetricsStore, *chclient.Client) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	exec, err := storeexec.New(client, nil)
	require.NoError(t, err)
	store := sysmfacts.NewSysmetricsStore(exec, nil, sysmfacts.SysmetricsStoreConfig{})
	t.Cleanup(store.Close)
	return store, client
}

// queryTSV runs sql and returns its rows as fields. TSV rather than Arrow
// because the comparison is over decimal text on both sides — going through
// Arrow would add a decode this test would then have to trust.
func queryTSV(t *testing.T, client *chclient.Client, sql string) (rows [][]string) {
	t.Helper()
	body, err := client.Query(context.Background(), sql+" FORMAT TSV")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)

	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	return
}

// TestComponentReadAgreesWithScan is the ADR-0189 M4 cross-oracle.
//
// LW_COMPONENT and the generated Scan verb are two read paths over one
// component definition: the first assembles a named tuple in SQL from the
// published Projection, the second fetches raw columns and decodes in Go
// (ADR-0100 §SD5 — point reads deliberately bypass the artefacts). Nothing
// forces them to agree, and a disagreement is exactly the failure this whole
// surface exists to prevent, so neither may be its own oracle.
//
// Both sides are pinned to the same snapshot with an upper bound on the order
// column: the table is append-only and may be written continuously by a
// running scraper, so an unbounded comparison would race the writer.
func TestComponentReadAgreesWithScan(t *testing.T) {
	store, client := liveStore(t)
	ctx := context.Background()
	require.NoError(t, store.VerifySchema(ctx))

	// The cutoff is the table's own newest order, taken once. Using a wall
	// clock here would compare a bound the writer's rows are dated against.
	maxOrder := queryTSV(t, client,
		"SELECT max("+sysmfacts.SysmetricsColOrder+") FROM "+sysmfacts.SysmetricsTableName)
	require.Len(t, maxOrder, 1)
	cutoff := maxOrder[0][0]
	if cutoff == "0" || cutoff == "" {
		t.Skip("boxer.facts carries no rows to compare")
	}
	// The order column is DateTime64(9, 'UTC'), so max() comes back rendered
	// rather than numeric; it is compared as a quoted literal, which
	// ClickHouse coerces to the column's type. Taking the bound once and
	// reusing the text is what makes both reads see one snapshot — a
	// subquery bound would be re-evaluated per statement and could move
	// between them.
	bound := sysmfacts.SysmetricsColOrder + " <= '" + cutoff + "'"

	// The Go side: the store's own Scan verb, which uses the baked Filter and
	// decodes the raw columns.
	type sample struct{ id, total string }
	fromScan := make([]sample, 0, 128)
	for ent, scanErr := range store.ScanSysMem(ctx, recordstore.ScanOpts{ExtraPredicate: bound}) {
		require.NoError(t, scanErr)
		require.NotNil(t, ent)
		require.True(t, ent.SysMem.Has, "ScanSysMem yielded an entity carrying no SysMem")
		fromScan = append(fromScan, sample{
			id:    strconv.FormatUint(ent.ID, 10),
			total: strconv.FormatUint(ent.SysMem.Val.TotalBytes, 10),
		})
	}
	if len(fromScan) == 0 {
		t.Skip("no SysMem rows in boxer.facts to compare against")
	}

	// The SQL side: the same component, read through the authoring surface.
	// The pass injects the kind's conformance filter into the inner scope,
	// conjoined with the bound written here.
	sql := expandComponent(t,
		"SELECT m.Id AS id, m.TotalBytes AS total FROM (SELECT LW_COMPONENT('SysMem') AS m FROM "+
			sysmfacts.SysmetricsTableName+" WHERE "+bound+")")
	fromSQL := make([]sample, 0, len(fromScan))
	for _, r := range queryTSV(t, client, sql) {
		require.Len(t, r, 2)
		fromSQL = append(fromSQL, sample{id: r[0], total: r[1]})
	}

	// Order is not part of the claim — the two paths sort differently — so
	// both sides are compared as multisets.
	less := func(s []sample) func(i, j int) bool {
		return func(i, j int) bool {
			if s[i].id != s[j].id {
				return s[i].id < s[j].id
			}
			return s[i].total < s[j].total
		}
	}
	sort.Slice(fromScan, less(fromScan))
	sort.Slice(fromSQL, less(fromSQL))

	// Logged so a passing run states how much it compared: an oracle that
	// agreed about three rows is not the same evidence as one that agreed
	// about thousands, and the difference is invisible otherwise.
	t.Logf("compared %d SysMem rows through both read paths", len(fromScan))

	assert.Equal(t, len(fromScan), len(fromSQL),
		"the two read paths disagree on which rows carry a conforming SysMem")
	assert.Equal(t, fromScan, fromSQL,
		"the two read paths disagree on values; the projection and the Go decode have drifted")
}

// The conformance filter is what makes the projection exact rather than
// first-match, so this asserts it is doing work: the component read must be a
// strict subset of the same projection with no filter at all.
//
// It is the observable half of the ADR-0066 property. A row carrying one
// membership twice would pass the unfiltered form and be rejected by this one;
// absent such a row in the live table, what remains checkable is that the
// filter narrows rather than being decorative.
func TestTheInjectedFilterNarrowsTheRowSet(t *testing.T) {
	_, client := liveStore(t)

	filtered := queryTSV(t, client, expandComponent(t,
		"SELECT count() FROM (SELECT LW_COMPONENT('SysMem') AS m FROM "+sysmfacts.SysmetricsTableName+")"))
	require.Len(t, filtered, 1)

	total := queryTSV(t, client, "SELECT count() FROM "+sysmfacts.SysmetricsTableName)
	require.Len(t, total, 1)

	conforming, err := strconv.Atoi(filtered[0][0])
	require.NoError(t, err)
	all, err := strconv.Atoi(total[0][0])
	require.NoError(t, err)

	assert.Positive(t, conforming, "no row passed the component filter; the comparison below would be vacuous")
	assert.Less(t, conforming, all,
		"every row in boxer.facts passed a SysMem filter — the predicate is not discriminating")
}

// symbolLrColumn is the symbol section's membership lane, found by prefix off
// the generated schema rather than spelled out: the aspect suffix moves when
// the section is re-aspected, and a literal here would go stale silently.
func symbolLrColumn(t *testing.T) string {
	t.Helper()
	var found []string
	for _, n := range factsColumnNames() {
		if strings.HasPrefix(n, "tv:symbol:lr:") {
			found = append(found, n)
		}
	}
	require.Len(t, found, 1, "expected exactly one symbol membership lane, got %v", found)
	return `"` + found[0] + `"`
}

// TestTheFilterRejectsARowCarryingAMembershipTwice demonstrates the ADR-0066
// property the whole injection exists for, rather than asserting it.
//
// Projection locates an attribute with indexOf and returns the first match, so
// on a row carrying one membership twice it answers plausibly and wrongly.
// Validator — and therefore Filter — is what rejects that row. Both halves are
// observed here: the bare projection answers for the malformed row, and the
// filtered read does not.
//
// The malformed row cannot be written through the store, which is the point:
// the writer cannot produce one, so the check needs a row crafted against a
// test-owned clone of the table. The artefacts carry unqualified column names
// (ADR-0189 §SD6), which is what lets them run against that clone unchanged.
func TestTheFilterRejectsARowCarryingAMembershipTwice(t *testing.T) {
	_, client := liveStore(t)
	ctx := context.Background()

	mem := sysmfacts.SysmetricsComponentSQL.Kinds["SysMem"]
	require.NotEmpty(t, mem.Filter)
	require.NotEmpty(t, mem.Projection)

	hostID, ok := vocabIds(t)["sysmMemHost"]
	require.True(t, ok, "the host membership should be in the vocabulary")
	lr := symbolLrColumn(t)

	// A clone rather than boxer.facts itself: this row is deliberately
	// malformed and must never join the runtime's data. LowCardinality(UInt64)
	// needs the suspicious-types setting to be created, which is a property of
	// the facts schema, not of this test.
	table := "boxer.lwtrap_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	const susp = " SETTINGS allow_suspicious_low_cardinality_types = 1"
	require.NoError(t, client.Exec(ctx, "CREATE TABLE "+table+" AS "+sysmfacts.SysmetricsTableName+susp))
	t.Cleanup(func() { _ = client.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })

	// One conforming row, copied from the live table.
	require.NoError(t, client.Exec(ctx,
		"INSERT INTO "+table+" SELECT * FROM "+sysmfacts.SysmetricsTableName+
			" WHERE "+mem.Filter+" LIMIT 1"))
	if n := queryTSV(t, client, "SELECT count() FROM "+table); n[0][0] == "0" {
		t.Skip("boxer.facts carries no conforming SysMem row to derive the case from")
	}

	// The same row with the host membership appearing twice.
	require.NoError(t, client.Exec(ctx,
		"INSERT INTO "+table+" SELECT * REPLACE (arrayConcat("+lr+", [toUInt64("+
			strconv.FormatUint(hostID, 10)+")]) AS "+lr+") FROM "+table+" LIMIT 1"))

	counts := queryTSV(t, client,
		"SELECT (SELECT count() FROM "+table+") AS all_rows,"+
			" (SELECT count() FROM "+table+" WHERE "+mem.Filter+") AS conforming,"+
			" (SELECT count() FROM (SELECT "+mem.Projection+" AS m FROM "+table+")) AS projected")
	require.Len(t, counts, 1)
	require.Len(t, counts[0], 3)

	assert.Equal(t, "2", counts[0][0], "the case needs both rows present")
	assert.Equal(t, "1", counts[0][1],
		"the Filter admitted a row carrying one membership twice; the conformance check is not working")
	assert.Equal(t, "2", counts[0][2],
		"the bare Projection declined to answer for the malformed row — if this ever holds, "+
			"the first-match hazard is gone and SD4's injection could be reconsidered")
}
