package tally

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// opsFixture is a window that knows two mounts and has listed a directory
// in pane A, without a store behind it.
func opsFixture(t *testing.T) (inst *App) {
	t.Helper()
	inst = newApp()
	t0 := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	inst.mountRows = []mountRow{
		{id: identifier.TaggedId(0x3BFE363BCF148002), name: "boxer-doc", snapshots: []ladingadapter.Snapshot{{Snap: t0}}},
		{id: identifier.TaggedId(0x3BFE363BCF148003), name: "lading-src", snapshots: []ladingadapter.Snapshot{{Snap: t0.Add(-time.Hour)}, {Snap: t0.Add(-2 * time.Hour)}}},
	}
	inst.mountsSeen = true
	a := &inst.panes[paneIDA]
	a.mount = inst.mountRows[0].id
	a.followLatest = true
	a.st.SetDir("doc")
	a.rows = []fsbrowser.Entry{
		{Name: "adr", Path: "doc/adr", IsDir: true},
		{Name: "README.md", Path: "doc/README.md", Size: 1200},
		{Name: "env-vars.md", Path: "doc/env-vars.md", Size: 800},
	}
	return
}

func query[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, inst *App, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(app.OperationCall{}, name, args)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(ManifestId)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(ManifestId))
	for _, name := range []string{opGetView, opListEntries, opGetSelection, opFind, opGrep, opSetLocation, opSelectEntry, opShowTab, opSetQuery, opRunQuery} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	run, _ := m.Operations.Lookup(opRunQuery)
	assert.True(t, run.Async, "run_query settles when the lane lands")
	assert.Equal(t, app.OperationEffectRun, run.Effect)
	set, _ := m.Operations.Lookup(opSetQuery)
	assert.Equal(t, app.OperationEffectDocument, set.Effect, "the query is authored state")
}

func TestGetViewReadsThePanesAndTheMounts(t *testing.T) {
	inst := opsFixture(t)
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "A", v.Panes.Target)
	assert.Equal(t, "doc", v.Panes.A.Dir)
	assert.Equal(t, "3bfe363bcf148002", v.Panes.A.Mount)
	assert.True(t, v.Panes.A.FollowLatest)
	assert.Equal(t, 3, v.Panes.A.Entries)
	assert.Equal(t, "", v.Panes.B.Mount, "pane B has no location yet")
	require.Len(t, v.Mounts, 2)
	assert.Equal(t, MountInfo{Id: "3bfe363bcf148003", Label: "lading-src", Snapshots: 2}, v.Mounts[1])
	assert.Contains(t, v.Tabs, "find")
	assert.Equal(t, "none", v.Result.Phase, "no query, no result")
	assert.False(t, v.Connected)
}

func TestListEntriesIsBoundedAndReadsItsOwnPane(t *testing.T) {
	inst := opsFixture(t)
	inst.focus = paneIDB
	out, err := query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{Pane: "A"})
	require.NoError(t, err)
	assert.Equal(t, 3, out.Matched, "the focused pane is not the argument")
	require.Len(t, out.Entries, 3)
	assert.Equal(t, Entry{Name: "adr", Path: "doc/adr", IsDir: true}, out.Entries[0])
	assert.False(t, out.Truncated)

	out, err = query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{Pane: "a", Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, out.Matched)
	assert.Len(t, out.Entries, 2)
	assert.True(t, out.Truncated, "a cut listing says so")

	for i := range opsMaxEntries + 20 {
		inst.panes[paneIDB].rows = append(inst.panes[paneIDB].rows, fsbrowser.Entry{Name: "f", Path: "f" + string(rune('a'+i%26))})
	}
	out, err = query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{Pane: "B", Limit: opsMaxEntries + 50})
	require.NoError(t, err)
	assert.Len(t, out.Entries, opsMaxEntries)
	assert.True(t, out.Truncated)

	_, err = query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{Pane: "C"})
	assert.Equal(t, reasonPane, refusal(err).Reason)
	_, err = query[ListEntriesArgs, ListEntriesResult](t, inst, opListEntries, ListEntriesArgs{Pane: "A", Limit: -1})
	assert.NotNil(t, refusal(err))
}

func TestSetLocationSetsAndNeverToggles(t *testing.T) {
	inst := opsFixture(t)
	other := inst.mountRows[1]
	for range 2 {
		require.NoError(t, command(t, inst, opSetLocation, SetLocationArgs{Pane: "B", Mount: "0x3BFE363BCF148003", Dir: "src/x"}))
		b := &inst.panes[paneIDB]
		assert.Equal(t, other.id, b.mount)
		assert.True(t, b.followLatest)
		assert.Equal(t, "src/x", b.st.Dir())
	}
	assert.Equal(t, paneIDB, inst.target, "the pane addressed becomes the target, as a click in it would")
	v := inst.Operations().ResourceValue(resPanes).(PanesValue)
	assert.Equal(t, "src/x", v.B.Dir)

	// A snapshot pins; latest lets go again; an empty part keeps the pane's.
	snap := other.snapshots[1].Snap
	require.NoError(t, command(t, inst, opSetLocation, SetLocationArgs{Pane: "B", Snapshot: snap.Format(time.RFC3339Nano)}))
	assert.False(t, inst.panes[paneIDB].followLatest)
	assert.True(t, inst.panes[paneIDB].snap.Equal(snap))
	assert.Equal(t, "src/x", inst.panes[paneIDB].st.Dir(), "the directory stays: that is time travel, not a reload")
	require.NoError(t, command(t, inst, opSetLocation, SetLocationArgs{Pane: "B", Snapshot: "latest"}))
	assert.True(t, inst.panes[paneIDB].followLatest)

	// Refusals change nothing.
	assert.Equal(t, reasonNoMount, refusal(command(t, inst, opSetLocation, SetLocationArgs{Pane: "A", Mount: "0x1"})).Reason)
	assert.Equal(t, inst.mountRows[0].id, inst.panes[paneIDA].mount)
	assert.Equal(t, reasonBrowsePane, refusal(command(t, inst, opSetLocation, SetLocationArgs{Pane: "R", Dir: "x"})).Reason)
	assert.NotNil(t, refusal(command(t, inst, opSetLocation, SetLocationArgs{Pane: "A", Snapshot: "yesterday"})))
}

func TestSelectEntryChecksTheListing(t *testing.T) {
	inst := opsFixture(t)
	for range 2 {
		require.NoError(t, command(t, inst, opSelectEntry, SelectEntryArgs{Pane: "A", Path: "doc/README.md"}))
		assert.Equal(t, "doc/README.md", inst.panes[paneIDA].selected)
		assert.Equal(t, []string{"doc/README.md"}, inst.panes[paneIDA].st.Selection())
	}
	sel, err := query[PaneArgs, Selection](t, inst, opGetSelection, PaneArgs{Pane: "A"})
	require.NoError(t, err)
	assert.Equal(t, "doc/README.md", sel.Path)
	assert.Equal(t, "3bfe363bcf148002", sel.Mount)
	assert.NotEmpty(t, sel.Snapshot, "a pane following latest reports the mount's newest")

	require.NoError(t, command(t, inst, opSelectEntry, SelectEntryArgs{Pane: "A", Path: "doc/adr"}))
	assert.Equal(t, "", inst.panes[paneIDA].selected, "a directory is selected in the pane and no file for the tabs")
	assert.Equal(t, reasonNotListed, refusal(command(t, inst, opSelectEntry, SelectEntryArgs{Pane: "A", Path: "doc/missing.md"})).Reason)
	require.NoError(t, command(t, inst, opSelectEntry, SelectEntryArgs{Pane: "A"}))
	assert.Empty(t, inst.panes[paneIDA].st.Selection())
}

func TestShowTabAndTheResultsTabNeedAQuery(t *testing.T) {
	inst := opsFixture(t)
	require.NoError(t, command(t, inst, opShowTab, ShowTabArgs{Tab: "find"}))
	assert.Equal(t, dockTabFind, inst.pendingDockActivate)
	assert.Equal(t, "find", tabSlug(inst.raisedTab))
	assert.Equal(t, reasonNoTab, refusal(command(t, inst, opShowTab, ShowTabArgs{Tab: "nope"})).Reason)
	assert.Equal(t, reasonNoResultsTab, refusal(command(t, inst, opShowTab, ShowTabArgs{Tab: "results"})).Reason)

	ok, reason := inst.Operations().Snapshot().Available(opRunQuery)
	assert.False(t, ok)
	assert.Equal(t, reasonNoQuery, reason)
	assert.Equal(t, reasonNoQuery, refusal(command(t, inst, opRunQuery, struct{}{})).Reason)

	r := refusal(command(t, inst, opSetQuery, SetQueryArgs{Sql: "INSERT INTO t VALUES (1)"}))
	require.NotNil(t, r)
	assert.Contains(t, r.Reason, "not read-only")
	assert.Equal(t, "", inst.querySql)
	for range 2 {
		require.NoError(t, command(t, inst, opSetQuery, SetQueryArgs{Sql: "SELECT path FROM fs('*') WHERE ext = '.md'", Label: "markdown"}))
	}
	assert.Equal(t, "SELECT path FROM fs('*') WHERE ext = '.md'", inst.Operations().ResourceValue(resQuery))
	require.NoError(t, command(t, inst, opShowTab, ShowTabArgs{Tab: "results"}))
	assert.Equal(t, paneIDR, inst.focus)
	ok, _ = inst.Operations().Snapshot().Available(opRunQuery)
	assert.True(t, ok)

	// Without a store the lane never runs, so the run stays pending; a
	// lane that is idle with nothing armed is settled at once.
	require.NoError(t, command(t, inst, opRunQuery, struct{}{}))
	assert.True(t, inst.runArmed)
	settled, _, err := inst.Operations().(app.OperationsAsyncI).SettleAsync(app.OperationCall{}, opRunQuery, nil)
	require.NoError(t, err)
	assert.False(t, settled, "nothing landed yet")
}

func TestFindAndGrepRefuseWithoutAStore(t *testing.T) {
	inst := opsFixture(t)
	_, _, err := inst.Operations().Snapshot().ExternalRead(app.OperationCall{}, opFind, mustEncode(t, FindArgs{Pane: "A", Query: "adr"}))
	assert.Equal(t, reasonNoStore, refusal(err).Reason)
	_, _, err = inst.Operations().Snapshot().ExternalRead(app.OperationCall{}, opGrep, mustEncode(t, GrepArgs{Pane: "B", Pattern: "x"}))
	assert.Equal(t, reasonNoLocation, refusal(err).Reason, "pane B has no mount")
	_, _, err = inst.Operations().Snapshot().ExternalRead(app.OperationCall{}, opFind, mustEncode(t, FindArgs{Pane: "R", Query: "adr"}))
	assert.Equal(t, reasonBrowsePane, refusal(err).Reason)
}

func mustEncode[T any](t *testing.T, v T) (raw []byte) {
	t.Helper()
	raw, err := buscodec.Encode(v)
	require.NoError(t, err)
	return
}

// The catalog's find is the Find tab's statement with one match per token,
// each the token's effective source, capped one past the lane's cap.
func TestFindBatterySQL(t *testing.T) {
	loc := location{mount: identifier.TaggedId(0x3BFE363BCF148002), snap: time.Unix(0, 1755723885967744578)}
	b := search.ParseQuery(`adr \.md$ quantile(`)
	sql := findBatterySQL(loc, findScopeDir, "doc", b, ".md", 10, findRowCap+1)
	assert.Contains(t, sql, "fs(4322952322827452418, 1755723885967744578)")
	assert.Contains(t, sql, "startsWith(path, 'doc/')")
	assert.Equal(t, 3, strings.Count(sql, "match(path, "), "one match per token")
	assert.Contains(t, sql, `match(path, '(?i)adr')`)
	assert.Contains(t, sql, `match(path, '(?i)\\.md$')`)
	assert.Contains(t, sql, `quantile\\(`, "a token that does not compile is quoted")
	assert.Contains(t, sql, "LIMIT 501")
	assert.Contains(t, findBatterySQL(loc, findScopeAll, "doc", b, "", 0, 10), "fs('*')")
	assert.NotContains(t, findBatterySQL(loc, findScopeMount, "doc", search.ParseQuery(""), "", 0, 10), "match(")
}
