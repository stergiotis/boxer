package logviewer

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/errorview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fieldview"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

func sampleRows() (rows []factsstore.LogRow) {
	t0 := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	rows = []factsstore.LogRow{
		{AppId: "github.com/example/play", Level: "debug", Message: "starting", Ts: t0},
		{AppId: "github.com/example/play", Level: "info", Message: "query ok", Ts: t0.Add(time.Second)},
		{AppId: "github.com/example/imztop", Level: "warn", Message: "high cpu", Ts: t0.Add(2 * time.Second)},
		{AppId: "github.com/example/play", Level: "error", Message: "connection refused", Ts: t0.Add(3 * time.Second)},
	}
	return
}

// TestApplyFilters_LevelThreshold rejects rows below the threshold.
// "trace" is the no-floor sentinel; tightening to "warn" drops debug
// and info but keeps warn and error.
func TestApplyFilters_LevelThreshold(t *testing.T) {
	got := applyFilters(sampleRows(), zerolog.WarnLevel, "", "")
	require.Len(t, got, 2)
	assert.Equal(t, "high cpu", got[0].Message)
	assert.Equal(t, "connection refused", got[1].Message)
}

func TestApplyFilters_LevelTraceKeepsAll(t *testing.T) {
	got := applyFilters(sampleRows(), zerolog.TraceLevel, "", "")
	assert.Len(t, got, 4)
}

// TestApplyFilters_AppIdSubstring is case-insensitive and matches
// anywhere in the AppId. Imztop is filtered out when the needle is
// "play"; tightening to "PLAY" still works.
func TestApplyFilters_AppIdSubstring(t *testing.T) {
	got := applyFilters(sampleRows(), zerolog.TraceLevel, "play", "")
	require.Len(t, got, 3)
	for _, r := range got {
		assert.Contains(t, string(r.AppId), "play")
	}
	gotUpper := applyFilters(sampleRows(), zerolog.TraceLevel, "PLAY", "")
	assert.Len(t, gotUpper, 3, "filter must be case-insensitive")
}

// TestApplyFilters_MessageSubstring exercises the third filter slot.
func TestApplyFilters_MessageSubstring(t *testing.T) {
	got := applyFilters(sampleRows(), zerolog.TraceLevel, "", "connection")
	require.Len(t, got, 1)
	assert.Equal(t, "connection refused", got[0].Message)
}

// TestApplyFilters_AllSlots combines all three filters and asserts
// they intersect (logical AND).
func TestApplyFilters_AllSlots(t *testing.T) {
	got := applyFilters(sampleRows(), zerolog.WarnLevel, "play", "connection")
	require.Len(t, got, 1)
	assert.Equal(t, "connection refused", got[0].Message)
}

// TestApplyFilters_LevelInvalid documents the policy for empty /
// unparseable Level strings: a Trace threshold (the no-floor sentinel)
// short-circuits the parse, so malformed rows pass through. Any
// stricter threshold drops them — operators don't see broken events
// in the filtered tail.
func TestApplyFilters_LevelInvalid(t *testing.T) {
	rows := []factsstore.LogRow{{AppId: "x", Level: "", Message: "no level"}}
	keep := applyFilters(rows, zerolog.TraceLevel, "", "")
	assert.Len(t, keep, 1, "Trace threshold short-circuits the level check; malformed level passes")
	rej := applyFilters(rows, zerolog.WarnLevel, "", "")
	assert.Empty(t, rej, "any non-Trace threshold drops events with unparseable Level")
}

// TestShortAppId trims the github.com/<org>/<repo>/.../ prefix to the
// last segment when present; leaves other shapes intact.
func TestShortAppId(t *testing.T) {
	assert.Equal(t, "play",
		shortAppId("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/play"))
	assert.Equal(t, "play", shortAppId("github.com/example/foo/bar/play"))
	assert.Equal(t, "org.test.foo", shortAppId("org.test.foo"))
	assert.Equal(t, "", shortAppId(""))
}

// TestLevelTone covers the level→badge-tone mapping that drives the
// Level column's chip colour. Trace/debug fall to Neutral so quiet
// events stay quiet; info → Info; warn → Warning; error / fatal /
// panic → Error. Empty / unparseable strings fall through to Neutral
// (no parser panic, no random colour).
func TestLevelTone(t *testing.T) {
	cases := []struct {
		level string
		want  badge.ToneE
	}{
		{"trace", badge.ToneNeutral},
		{"debug", badge.ToneNeutral},
		{"info", badge.ToneInfo},
		{"warn", badge.ToneWarning},
		{"error", badge.ToneError},
		{"fatal", badge.ToneError},
		{"panic", badge.ToneError},
		{"", badge.ToneNeutral},
		{"not-a-level", badge.ToneNeutral},
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, levelTone(tc.level), "levelTone(%q)", tc.level)
	}
}

// TestLevelLabel uppercases known levels so the badge reads as a
// status code; empty levels render as "—" so the column never has
// holes that disturb the table grid.
func TestLevelLabel(t *testing.T) {
	assert.Equal(t, "INFO", levelLabel("info"))
	assert.Equal(t, "WARN", levelLabel("warn"))
	assert.Equal(t, "—", levelLabel(""))
	// `levelLabel` only uppercases — it doesn't validate. Garbage in,
	// garbage out is fine here; the table isn't a contract surface.
	assert.Equal(t, "MIXEDCASE", levelLabel("MIXEDcase"))
}

// TestRowTint picks tints only for warn-and-above; clean rows return
// ok=false so the per-cell renderer can skip the Frame entirely (the
// fast path that keeps unstyled rows cheap).
func TestRowTint(t *testing.T) {
	for _, lvl := range []string{"trace", "debug", "info", "", "garbage"} {
		_, ok := rowTint(lvl)
		assert.Falsef(t, ok, "rowTint(%q) must be a no-op", lvl)
	}
	for _, lvl := range []string{"warn", "error", "fatal", "panic"} {
		tint, ok := rowTint(lvl)
		require.Truef(t, ok, "rowTint(%q) must return a tint", lvl)
		// Sanity: the tint must be a literal colour value (alpha != 0
		// after the bitwise OR).
		assert.NotZerof(t, tint.Literal(), "tint literal for %q must be non-zero", lvl)
	}
}

// TestRowKey is stable across two snapshots that contain the same
// row, and distinct for rows that share a Ts but differ in Caller or
// Message. The selection-highlight comparison hangs off this — a
// flaky key would make the highlight blink frame to frame.
func TestRowKey(t *testing.T) {
	t0 := time.Date(2026, 5, 14, 12, 0, 0, 12345, time.UTC)
	r := factsstore.LogRow{Ts: t0, Caller: "x.go:1", Message: "hello"}
	assert.Equal(t, rowKey(r), rowKey(r), "key must be stable for equal rows")

	rDifferentCaller := r
	rDifferentCaller.Caller = "x.go:2"
	assert.NotEqual(t, rowKey(r), rowKey(rDifferentCaller), "caller must distinguish coincident-Ts rows")

	rDifferentMsg := r
	rDifferentMsg.Message = "world"
	assert.NotEqual(t, rowKey(r), rowKey(rDifferentMsg), "message must distinguish coincident-Ts rows")
}

// TestToFieldviewKind: the adapter that bridges the factsstore enum
// to the fieldview enum must map every primitive kind onto its
// counterpart and route Unknown to KindUnknown (fieldview falls
// back to the Str slot). A future kind added on either side without
// a matching arm here would silently land at KindUnknown — the
// table below guards against that drift.
//
// Per-kind value formatting + bytes truncation now live inside the
// fieldview package and are covered by its own viewer_test.
func TestToFieldviewKind(t *testing.T) {
	cases := map[factsstore.LogFieldKindE]fieldview.KindE{
		factsstore.LogFieldKindString:  fieldview.KindString,
		factsstore.LogFieldKindInt:     fieldview.KindInt,
		factsstore.LogFieldKindUint:    fieldview.KindUint,
		factsstore.LogFieldKindFloat:   fieldview.KindFloat,
		factsstore.LogFieldKindBool:    fieldview.KindBool,
		factsstore.LogFieldKindBytes:   fieldview.KindBytes,
		factsstore.LogFieldKindTime:    fieldview.KindTime,
		factsstore.LogFieldKindUnknown: fieldview.KindUnknown,
	}
	for src, want := range cases {
		assert.Equalf(t, want, toFieldviewKind(src),
			"toFieldviewKind(%d)", src)
	}
}

// TestToFieldviewFields: the per-row adapter must preserve every
// typed slot through the conversion (no information loss) and
// produce a fieldview.Field whose Kind drives downstream rendering.
// Length must match input — the adapter pre-sizes; future filtering
// would need its own test.
func TestToFieldviewFields(t *testing.T) {
	in := []factsstore.LogField{
		{Name: "method", Kind: factsstore.LogFieldKindString, Str: "GET"},
		{Name: "status", Kind: factsstore.LogFieldKindUint, Uint: 200},
		{Name: "ratio", Kind: factsstore.LogFieldKindFloat, Float: 1.5},
		{Name: "ok", Kind: factsstore.LogFieldKindBool, Bool: true},
		{Name: "blob", Kind: factsstore.LogFieldKindBytes, Bytes: []byte{0x01, 0x02}},
		{Name: "ts", Kind: factsstore.LogFieldKindTime, Time: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)},
	}
	out := toFieldviewFields(in)
	require.Len(t, out, len(in), "adapter must not drop fields silently")
	assert.Equal(t, "GET", out[0].Str)
	assert.Equal(t, fieldview.KindString, out[0].Kind)
	assert.Equal(t, uint64(200), out[1].Uint)
	assert.Equal(t, fieldview.KindUint, out[1].Kind)
	assert.Equal(t, 1.5, out[2].Float)
	assert.True(t, out[3].Bool)
	assert.Equal(t, []byte{0x01, 0x02}, out[4].Bytes)
	assert.False(t, out[5].Time.IsZero())
}

// TestToErrorviewContext: the bridge → errorview adapter must
// preserve every fact field through the conversion (no information
// loss) and translate a nil input into the zero Context (which the
// renderer short-circuits via IsEmpty). A regression in the field-
// mapping arm would silently drop structured-data diagnostics or
// stack frame info from the detail-pane tree.
func TestToErrorviewContext(t *testing.T) {
	t.Run("nil input maps to zero Context", func(t *testing.T) {
		got := toErrorviewContext(nil)
		assert.True(t, got.IsEmpty(), "nil ErrorContext must yield an empty errorview.Context")
	})

	t.Run("preserves every fact field", func(t *testing.T) {
		in := &factsstore.LogErrorContext{
			Streams: []factsstore.LogErrorStream{
				{Name: "no-stack", Facts: []factsstore.LogErrorFact{
					{Msg: "stackless error", Id: 1},
				}},
				{Name: "stack-0", Facts: []factsstore.LogErrorFact{
					{Msg: "outer", Id: 2, ParentId: 1},
					{Source: "x.go", Line: "42", Function: "DoThing", Id: 3, ParentId: 2},
					{Msg: "leaf", Data: []byte{0xa1, 0x63}, DataDiag: `{"k":"v"}`, Id: 4, ParentId: 2},
				}},
			},
		}
		out := toErrorviewContext(in)
		require.Len(t, out.Streams, 2)

		assert.Equal(t, "no-stack", out.Streams[0].Name)
		require.Len(t, out.Streams[0].Facts, 1)
		assert.Equal(t, "stackless error", out.Streams[0].Facts[0].Msg)
		assert.Equal(t, uint64(1), out.Streams[0].Facts[0].Id)

		st := out.Streams[1]
		assert.Equal(t, "stack-0", st.Name)
		require.Len(t, st.Facts, 3)
		assert.Equal(t, "outer", st.Facts[0].Msg)
		assert.Equal(t, uint64(1), st.Facts[0].ParentId)
		assert.Equal(t, "x.go", st.Facts[1].Source)
		assert.Equal(t, "42", st.Facts[1].Line)
		assert.Equal(t, "DoThing", st.Facts[1].Function)
		assert.Equal(t, "leaf", st.Facts[2].Msg)
		assert.Equal(t, []byte{0xa1, 0x63}, st.Facts[2].Data)
		assert.Equal(t, `{"k":"v"}`, st.Facts[2].DataDiag)
	})

	t.Run("output type is errorview.Context", func(t *testing.T) {
		var _ errorview.Context = toErrorviewContext(nil)
	})
}

// TestHumanAge picks a unit per magnitude — sub-minute renders as
// seconds, sub-hour as minutes, otherwise hours. Negative durations
// (clock skew) clamp to zero to avoid "-0.4s ago" surprises.
func TestHumanAge(t *testing.T) {
	assert.Equal(t, "0.0s ago", humanAge(-time.Second))
	assert.Equal(t, "1.5s ago", humanAge(1500*time.Millisecond))
	assert.Equal(t, "2.5m ago", humanAge(150*time.Second))
	assert.Equal(t, "3.0h ago", humanAge(3*time.Hour))
}

// TestManifestRegistered confirms the init() side-effect actually
// landed the AppI in DefaultRegistry. A renamed Id (or a missing init
// in a refactor) would silently make the widget unreachable from the
// launcher; the test guards that drift.
func TestManifestRegistered(t *testing.T) {
	a, ok := runtimeapp.Lookup("github.com/stergiotis/boxer/public/keelson/runtime/logviewer")
	require.True(t, ok, "logviewer AppI must be registered via init()")
	m := a.Manifest()
	assert.Equal(t, "Log viewer", m.Display)
	assert.Equal(t, runtimeapp.SurfaceWindowed, m.Surface)
}

// TestInstancesAreDistinct guards the multi-instance fix: two Open()
// calls must yield independent LogViewerApp values with disjoint seeds
// and disjoint filter state. Without this, two dock tiles of the
// logviewer would push identical Go-side widget ids into the same
// frame and trip the duplicate-id warning (the bug that motivated
// the per-instance refactor).
func TestInstancesAreDistinct(t *testing.T) {
	a1, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/keelson/runtime/logviewer")
	require.NoError(t, err)
	a2, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/keelson/runtime/logviewer")
	require.NoError(t, err)

	lv1, ok := a1.(*LogViewerApp)
	require.True(t, ok, "factory must yield *LogViewerApp")
	lv2, ok := a2.(*LogViewerApp)
	require.True(t, ok)

	assert.NotSame(t, lv1, lv2, "factory must allocate a fresh instance per Open")
	assert.NotSame(t, lv1.ids, lv2.ids, "each instance owns its own WidgetIdStack")

	// Filter state must be independent.
	lv1.filterAppId = "alpha"
	assert.Empty(t, lv2.filterAppId, "filter state must not leak between instances")

	// Selection state must be independent — clicking a row in tile 1
	// must not light up the same row in tile 2.
	lv1.hasSelected = true
	lv1.selected = factsstore.LogRow{Message: "in-tile-1"}
	assert.False(t, lv2.hasSelected, "selection must not leak between instances")
}

// TestSelectionSurvivesSinkTrim is the load-bearing claim of the
// detail pane: the selected row is stored by value, so trimming it
// out of the Sink's tail does not blank the pane. We simulate this
// by snapshotting → selecting → discarding the slice.
func TestSelectionSurvivesSinkTrim(t *testing.T) {
	rows := sampleRows()
	app := &LogViewerApp{}
	app.hasSelected = true
	app.selected = rows[2] // the warn row
	originalKey := rowKey(app.selected)

	// Trim the snapshot — the Sink would do this when the ring rolls
	// past the selected row.
	rows = nil
	_ = rows

	// Selection must still resolve to the same key.
	assert.Equal(t, originalKey, rowKey(app.selected))
	assert.True(t, app.hasSelected)
	assert.Equal(t, "high cpu", app.selected.Message)
}
