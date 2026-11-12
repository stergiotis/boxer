package chat

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func stopNames(set stopSet) (names []string) {
	for _, s := range set {
		names = append(names, s.spec.Name)
	}
	return
}

// A preset's stops are in force first and are not the person's to drop or
// to shadow; the person's own are (ADR-0370 §SD6).
func TestAPresetsStopsAreFixedAndThePersonsAreTheirs(t *testing.T) {
	p := auditTrailPreset()
	p.Stops = []Stop{{Name: "box", Then: "ask", After: "15m"}}
	a, err := NewPreset(p)
	require.NoError(t, err)
	inst := a.(*App)
	require.NoError(t, inst.takeStop(Stop{Name: "size", Then: "stop", ArtefactBytes: 1000}))
	assert.Equal(t, []string{"box", "size"}, stopNames(inst.stopsInForce()))
	require.ErrorContains(t, inst.takeStop(Stop{Name: "box", Then: "pause", After: "1h"}), "share a name")
	require.ErrorContains(t, inst.takeStop(Stop{Name: "bad", Then: "ask", SQL: "SELECT 1 FROM secrets"}), "reads secrets")

	inst.removeStop("box")
	inst.removeStop("size")
	assert.Equal(t, []string{"box"}, stopNames(inst.stopsInForce()), "the preset's stop stays")
	assert.Equal(t, stopOriginPreset, inst.stopsInForce()[0].origin)

	p.Stops = []Stop{{Name: "box", Then: "ask"}}
	_, err = NewPreset(p)
	require.ErrorContains(t, err, "exactly one of")
	plain := auditTrailPreset()
	assert.Equal(t, (&plain).digest(), (&p).digest(), "stops are not in what a run is pinned to")
}

// A seed gives a window stops of the person's own; one that does not
// construct gives none and says why.
func TestTheSeedGivesAWindowItsOwnStops(t *testing.T) {
	inst := newApp()
	inst.seedStops(`[{"name":"box","then":"ask","after":"15m"},{"name":"done","then":"end","artefact_match":"^## Done"}]`)
	assert.Equal(t, []string{"box", "done"}, stopNames(inst.stopsInForce()))
	assert.Empty(t, inst.note.text)

	inst = newApp()
	inst.seedStops(`[{"name":"box","then":"ask"}]`)
	assert.Empty(t, inst.stopsInForce())
	assert.Contains(t, inst.note.text, "BOXER_CHAT_STOPS")
	assert.True(t, inst.note.failed)

	inst = newApp()
	inst.seedStops(`{"name":"box"}`)
	assert.Contains(t, inst.note.text, "not a JSON list")
}

// A stop that ends the conversation closes its run and leaves the window
// refusing a turn; one that closes it asks the host, once, and a host with
// no window to close leaves the ended conversation in view (ADR-0370 §SD4,
// §SD9).
func TestAStopEndsTheConversationAndClosesTheWindow(t *testing.T) {
	inst := newApp()
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.landStops(&turnResult{})
	assert.Empty(t, inst.conv.ended)
	assert.False(t, inst.coord.scope.IsRetired())

	inst.landStops(&turnResult{ended: stopActionEnd, endedWhy: `the stop "done" fired`})
	assert.Equal(t, `the stop "done" fired`, inst.conv.ended)
	assert.True(t, inst.coord.scope.IsRetired(), "the run closes")
	assert.Empty(t, inst.stops.closeAsked, "end leaves the window")
	assert.False(t, inst.wakeReady())

	inst = newApp()
	inst.focused = true
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.landStops(&turnResult{ended: stopActionClose, endedWhy: `the stop "fin" fired`})
	fc := app.NewStaticFrameContext(app.NewStaticMountContext(ManifestId, zerolog.Nop(), nil, nil, nil), nil)
	var asked []string
	fc.SetWindowClose(func(reason string) { asked = append(asked, reason) })
	inst.syncStopClose(fc)
	inst.syncStopClose(fc)
	assert.Equal(t, []string{`the stop "fin" fired`}, asked)

	inst = newApp()
	inst.focused = true
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.landStops(&turnResult{ended: stopActionClose, endedWhy: "x"})
	inst.syncStopClose(app.NewStaticFrameContext(app.NewStaticMountContext(ManifestId, zerolog.Nop(), nil, nil, nil), nil))
	assert.Contains(t, inst.note.text, "closes no window")
	assert.NotEmpty(t, inst.conv.ended)
}

// A resumed run brings back the person's stops as it last recorded them,
// without the ones that were a preset's, and its firings.
func TestAResumeBringsBackThePersonsStops(t *testing.T) {
	set, err := compileStops([]Stop{{Name: "box", Then: "ask", After: "15m"}}, stopOriginPreset)
	require.NoError(t, err)
	own, err := compileStops([]Stop{{Name: "size", Then: "stop", ArtefactBytes: 9}}, stopOriginPerson)
	require.NoError(t, err)
	entries := []agentrun.Entry{
		{Ordinal: 1, Kind: agentrun.KindIgnorable, Name: stopsEntryName, Form: stopsFormSet, Body: append(set, own...).record()},
		firedEntry(t, 2, "k1", stopFormFired, stopFiring{Stop: "size", Then: "stop", Rows: []string{"r"}, Shown: []string{`{"steps":1}`}}),
	}
	inst := newApp()
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.adoptStops(inst.coord, entries)
	assert.Equal(t, []string{"size"}, stopNames(inst.stopsInForce()))
	require.Len(t, inst.coord.stopEvents(), 1)
	assert.Equal(t, "size", inst.coord.stopEvents()[0].firing.Stop)
}

// The stops in force are written to the run once, and again only when they
// change (ADR-0370 §SD6).
func TestTheStopsInForceAreRecordedWhenTheyChange(t *testing.T) {
	model := &scriptedModel{}
	coord, cli, req := questionsRig(t, model)
	coord.setStopRunner(&scriptedStops{})
	coord.setStops(mustStops(t, Stop{Name: "box", Then: "ask", After: "15m"}))
	for range 2 {
		_, err := runTurn(t.Context(), cli, coord, req, nil)
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"stops/set"}, stopForms(coord))
	coord.setStops(nil)
	_, err := runTurn(t.Context(), cli, coord, req, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"stops/set", "stops/set"}, stopForms(coord), "removing the last stop is a change the record keeps")
	rec, _, ok := lastStopsRecord(coord.journalNow().Entries())
	require.True(t, ok)
	assert.Empty(t, rec.Stops)
}

// Editing a stop's statement keeps its name and action and makes it a stop
// declared by its statement; a statement that is not admitted leaves the
// stop as it was, and a preset's stop is not the person's to edit
// (ADR-0370 §SD6, §SD8).
func TestEditingAStopsStatement(t *testing.T) {
	p := auditTrailPreset()
	p.Stops = []Stop{{Name: "box", Then: "ask", After: "15m"}}
	a, err := NewPreset(p)
	require.NoError(t, err)
	inst := a.(*App)
	require.NoError(t, inst.takeStop(Stop{Name: "size", Then: "stop", ArtefactBytes: 1000}))
	before := inst.stops.own[0]
	assert.Contains(t, before.sql, "\n", "a spelled statement is laid out over lines, for the editor")

	edited := "SELECT intDiv(bytes, 500) AS steps\nFROM artefact\nWHERE bytes >= 2000 ;\n"
	require.NoError(t, inst.editStop("size", edited))
	after := inst.stops.own[0]
	assert.Equal(t, "size", after.spec.Name)
	assert.Equal(t, stopActionStop, after.then)
	assert.Equal(t, "sql", after.kind)
	assert.Equal(t, cleanStopSQL(edited), after.sql)
	assert.Zero(t, after.spec.ArtefactBytes, "it is declared by its statement now")
	assert.Equal(t, edited, after.spec.SQL, "the declaration keeps what was written")

	require.ErrorContains(t, inst.editStop("size", "SELECT * FROM file('/etc/passwd', 'LineAsString')"), "must only read")
	assert.Equal(t, after, inst.stops.own[0], "a refused edit changes nothing")
	require.ErrorContains(t, inst.editStop("box", "SELECT turn FROM clock"), "not a stop of your own")
	require.ErrorContains(t, inst.editStop("gone", "SELECT turn FROM clock"), "not a stop of your own")

	var check stopCheck
	assert.Empty(t, check.of("size", "SELECT turn FROM clock"))
	assert.Contains(t, check.of("size", "SELECT turn FROM"), "does not parse")
	assert.Contains(t, check.of("size", "SELECT name FROM secrets"), "reads secrets")
	assert.Equal(t, uint32(stopEditorRowsMin), stopEditorRows("SELECT 1"))
	assert.Equal(t, uint32(stopEditorRowsMax), stopEditorRows(strings.Repeat("x\n", 40)))
}

// The Stops tab draws under the discard channel in each of its shapes: the
// Simple view's folded text, a preset's read-only statement, the person's
// statement editors with an edit pending, and the form with its editor. A
// mismatched id stack compiles and vets clean and panics here.
func TestTheStopsTabRendersInEveryShape(t *testing.T) {
	t.Cleanup(scenetest.Install())
	p := auditTrailPreset()
	p.Stops = []Stop{{Name: "box", Then: "ask", After: "15m"}, {Name: "ddl", Then: "pause", ToolMatch: "^query$", ArgumentsMatch: `(?i)\bdrop\b`}}
	a, err := NewPreset(p)
	require.NoError(t, err)
	inst := a.(*App)
	require.NoError(t, inst.takeStop(Stop{Name: "size", Then: "stop", ArtefactBytes: 1000}))
	require.NoError(t, inst.takeStop(Stop{Name: "said", Then: "end", JournalMatch: "DONE"}))
	inst.coord = newCoordinator(nil, nil, inst.conv.id)
	inst.coord.loadStopEvents([]agentrun.Entry{
		firedEntry(t, 1, "k1", stopFormFired, stopFiring{Stop: "size", Then: "stop", Shown: []string{`{"steps":1}`}, More: 2, Point: stopPointRequest}),
		firedEntry(t, 2, "k2", stopFormFired, stopFiring{Stop: "box", Then: "stop", Error: "the pool is gone", Point: stopPointReply}),
	})
	frame := func() {
		for range c.IdScope(inst.ids.PrepareStr("stops-body")) {
			inst.renderStopsTab()
		}
		c.CurrentApplicationState.StateManager.ScriptReset()
	}

	inst.simple = true
	frame()
	assert.Empty(t, inst.stops.editors, "the Simple view draws no editor")

	inst.simple = false
	frame()
	assert.Empty(t, inst.stops.editors, "nor does a tab that is not in front")

	inst.tabSeen[tabStops] = true
	frame()
	require.Len(t, inst.stops.editors, 2, "one editor per stop of the person's own, none for the preset's")
	assert.Equal(t, inst.stops.own[0].sql, inst.stops.editors["size"].draft)

	// An edit that is admitted, one that is not, and the form's editor.
	inst.stops.editors["size"].draft = "SELECT intDiv(bytes, 2000) AS steps FROM artefact WHERE bytes >= 2000"
	inst.stops.editors["said"].draft = "SELECT name FROM secrets"
	inst.stops.kind, inst.stops.value = stopKindSQL, "SELECT turn FROM"
	frame()
	assert.Empty(t, inst.stops.editors["size"].check.reason)
	assert.Contains(t, inst.stops.editors["said"].check.reason, "reads secrets")
	assert.Contains(t, inst.stops.formCheck.reason, "does not parse")
	require.NotNil(t, inst.stops.formEd)

	inst.removeStop("said")
	frame()
	assert.Len(t, inst.stops.editors, 1)
}
