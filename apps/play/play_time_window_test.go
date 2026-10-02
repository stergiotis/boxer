package play

import (
	"testing"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stretchr/testify/require"
)

// With nothing brushed the Timeline publishes the unbounded window; a brush
// publishes its range in the DateTime64(3, 'UTC') raw form.
func TestTimelinePublishesTheBrushedWindow(t *testing.T) {
	d := NewTimelineDriver(c.NewWidgetIdStack(), nil, nil, nil)
	em := &recordingEmitter{}
	d.publishWindow(em)
	require.Equal(t, timelineWindowFloor, em.scalars[signalTimelineFrom])
	require.Equal(t, timelineWindowCeil, em.scalars[signalTimelineTo])

	from := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC).UnixMilli()
	to := time.Date(2026, 9, 30, 12, 30, 0, 0, time.UTC).UnixMilli()
	d.tl.SetBrush(to, from)
	d.publishWindow(em)
	require.Equal(t, "2026-09-30 10:00:00.000", em.scalars[signalTimelineFrom])
	require.Equal(t, "2026-09-30 12:30:00.000", em.scalars[signalTimelineTo])
}

// The unbrushed window is the declaration's seed, so a query reading it runs
// before the Timeline has rendered.
func TestTimelineWindowSeedsAreUnbounded(t *testing.T) {
	from, ok := reservedSignalIndex[signalTimelineFrom].Seed.raw()
	require.True(t, ok)
	require.Equal(t, timelineWindowFloor, from)
	to, ok := reservedSignalIndex[signalTimelineTo].Seed.raw()
	require.True(t, ok)
	require.Equal(t, timelineWindowCeil, to)
}

// The Map filters on its time column only while a window is brushed; the
// window's values ride the params, a new window restarts the ladder, and a
// bad column name is a control error rather than SQL.
func TestMapFiltersOnTheBrushedWindow(t *testing.T) {
	d := NewMapDriver(nil, nil)
	defer d.lane.close()
	g := newQueryGraph(nil, nil)
	em := graphEmitter{graph: g}
	settle := func() map[string]string {
		d.readWindow(g.signals())
		d.updateViewport(47, 48, 8, 9, 64, 64, em)
		return resolveSignalNamesWithDefaults(d.templateReads, nil, g.signals())
	}

	settle()
	require.NotContains(t, d.template, "tl_from", "no window, no predicate")

	em.Emit(signalTimelineFrom, timelineWindowFloor)
	em.Emit(signalTimelineTo, timelineWindowCeil)
	settle()
	require.NotContains(t, d.template, "tl_from", "the unbounded window is no window")

	em.Emit(signalTimelineFrom, "2026-09-30 10:00:00.000")
	em.Emit(signalTimelineTo, "2026-09-30 12:30:00.000")
	d.ladder.level = 1
	params := settle()
	require.Contains(t, d.template, "time BETWEEN {tl_from:DateTime64(3, 'UTC')} AND {tl_to:DateTime64(3, 'UTC')}")
	require.Equal(t, "2026-09-30 10:00:00.000", params["param_tl_from"])
	require.Equal(t, 0, d.ladder.level, "a new window starts the ladder over")
	require.Equal(t, "window 2026-09-30 10:00:00 – 12:30:00 UTC on time", d.windowStatus())
	require.Equal(t, "2026-09-30 23:00:00 – 2026-10-01 01:00:00 UTC", formatWindow("2026-09-30 23:00:00.000", "2026-10-01 01:00:00.000"))

	d.timeCol = ""
	settle()
	require.NotContains(t, d.template, "tl_from", "no time column filters nothing")

	d.timeCol = "time; DROP"
	settle()
	require.Equal(t, "time column must be a plain column name", d.controlErr)
}

// A Custom colour block outside Grammar1 leaves the template unparsed; the
// brushed window's slots still resolve, or the server rejects the raster.
func TestMapWindowResolvesWithAnUnparsedTemplate(t *testing.T) {
	d := NewMapDriver(nil, nil)
	defer d.lane.close()
	for i, r := range builtinRenders {
		if r.custom {
			d.renderIdx = i
		}
	}
	d.customColorSQL = "toUInt32(count() DIV 2)"
	g := newQueryGraph(nil, nil)
	em := graphEmitter{graph: g}
	em.Emit(signalTimelineFrom, "2026-09-30 10:00:00.000")
	em.Emit(signalTimelineTo, "2026-09-30 12:30:00.000")
	d.readWindow(g.signals())
	d.updateViewport(47, 48, 8, 9, 64, 64, em)
	_, _, err := extractSlotsAndParams(d.template)
	require.Error(t, err, "the case under test is the unparsed template")
	params := resolveSignalNamesWithDefaults(d.templateReads, nil, g.signals())
	require.Equal(t, "2026-09-30 10:00:00.000", params["param_tl_from"])
	require.Equal(t, "2026-09-30 12:30:00.000", params["param_tl_to"])
}
