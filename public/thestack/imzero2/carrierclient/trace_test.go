package carrierclient

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTraceSkipsCommentsAndBlanks(t *testing.T) {
	in := `
# a trace reads like a script, so comments have to survive

{"do":"click","name":"Panes"}

  {"do":"capture","text":"panes-open","settleMs":400}
`
	steps, err := ParseTrace(strings.NewReader(in))
	require.NoError(t, err)
	require.Len(t, steps, 2)
	assert.Equal(t, "click", steps[0].Do)
	assert.Equal(t, "Panes", steps[0].Name)
	assert.Equal(t, 400, steps[1].SettleMs)
}

func TestParseTraceDragStep(t *testing.T) {
	steps, err := ParseTrace(strings.NewReader(
		`{"do":"drag","x":10,"y":20,"toX":110,"toY":60,"steps":8,"durationMs":200}`))
	require.NoError(t, err)
	require.Len(t, steps, 1)
	st := steps[0]
	assert.Equal(t, "drag", st.Do)
	assert.False(t, st.hasAnchor(), "a coordinate drag names no widget")
	assert.Equal(t, float32(110), st.ToX)
	assert.Equal(t, float32(60), st.ToY)
	assert.Equal(t, 8, st.Steps)
	assert.Equal(t, 200, st.DurationMs)
	assert.Equal(t, "drag (10,20) -> (110,60)", st.describe())
	// Anchored, X/Y read as the delta and the log line says so.
	assert.Equal(t, "drag \"Slider\" by (120,0)", Step{Do: "drag", Name: "Slider", X: 120}.describe())
}

func TestDragPathEndsAtTheTargetWithStepsPoints(t *testing.T) {
	path := dragPath(0, 0, 100, 50, 4)
	require.Len(t, path, 4)
	assert.Equal(t, [2]float32{25, 12.5}, path[0])
	assert.Equal(t, [2]float32{100, 50}, path[3])
	// steps < 1 still reaches the end point, in one move.
	assert.Equal(t, [][2]float32{{100, 50}}, dragPath(0, 0, 100, 50, 0))
}

func TestParseTraceRejectsAVerblessStep(t *testing.T) {
	// A step with no verb is a typo that would otherwise run as a silent no-op.
	_, err := ParseTrace(strings.NewReader(`{"name":"Panes"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no \"do\" verb")
}

func TestParseTraceReportsTheOffendingLine(t *testing.T) {
	in := "{\"do\":\"click\",\"name\":\"a\"}\n{ not json }\n"
	_, err := ParseTrace(strings.NewReader(in))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 2")
}

func TestStepAnchorDetection(t *testing.T) {
	assert.True(t, Step{Name: "Run"}.hasAnchor())
	assert.True(t, Step{ID: 7}.hasAnchor())
	assert.True(t, Step{Role: "button"}.hasAnchor())
	assert.True(t, Step{Contains: "row"}.hasAnchor())
	// A coordinate click names no widget — that is the point of it.
	assert.False(t, Step{X: 10, Y: 20}.hasAnchor())
}

func TestRequiresAnchorExcludesClick(t *testing.T) {
	// click falls back to a coordinate for painter-only widgets; the others
	// have nothing to aim at without a node.
	assert.False(t, requiresAnchor("click"))
	for _, verb := range []string{"type", "set_value", "focus", "scroll_into_view", "wait"} {
		assert.True(t, requiresAnchor(verb), verb)
	}
	for _, verb := range []string{"capture", "key", "scroll", "drag", "hover", "cadence", "resize", "note"} {
		assert.False(t, requiresAnchor(verb), verb)
	}
}

func TestStepDescribeIsReadable(t *testing.T) {
	// The readable half of the ADR-0127 dual-layer step: what a log line and a
	// failure message show, so a reviewer never has to decode a node id.
	assert.Equal(t, `click "Panes"`, Step{Do: "click", Name: "Panes"}.describe())
	assert.Equal(t, `type (text_input) "SELECT 1"`,
		Step{Do: "type", Role: "text_input", Text: "SELECT 1"}.describe())
	assert.Equal(t, `click ~"row 3"`, Step{Do: "click", Contains: "row 3"}.describe())
	assert.Equal(t, "click #42", Step{Do: "click", ID: 42}.describe())
}

func TestStepLocatorCarriesNth(t *testing.T) {
	n := 2
	loc := Step{Name: "Close", Nth: &n}.locator()
	assert.True(t, loc.HasNth)
	assert.Equal(t, 2, loc.Nth)
	// Absent nth must stay absent, or every ambiguous locator would silently
	// resolve to the first match.
	assert.False(t, Step{Name: "Close"}.locator().HasNth)
}

func TestParseTraceClickButtonAndCount(t *testing.T) {
	steps, err := ParseTrace(strings.NewReader(`{"do":"click","name":"Row 3","button":"secondary","count":2,"modifiers":4}`))
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "secondary", steps[0].Button)
	assert.Equal(t, 2, steps[0].Count)
	assert.Equal(t, uint32(4), steps[0].Modifiers)
}

func TestPointerButtonNames(t *testing.T) {
	for name, want := range map[string]uint32{
		"": ButtonPrimary, "primary": ButtonPrimary, "left": ButtonPrimary,
		"secondary": ButtonSecondary, "right": ButtonSecondary,
		"middle": ButtonMiddle, "extra1": ButtonExtra1, "extra2": ButtonExtra2,
	} {
		got, err := pointerButton(name)
		require.NoError(t, err, name)
		assert.Equal(t, want, got, name)
	}
	_, err := pointerButton("tertiary")
	require.Error(t, err)
}

func TestParseStepsReadsAnArrayAndChecksTheVerb(t *testing.T) {
	steps, err := ParseSteps([]byte(`[{"do":"click","name":"a,b"},{"do":"tree","text":"rows"}]`))
	require.NoError(t, err)
	require.Len(t, steps, 2)
	assert.Equal(t, "a,b", steps[0].Name)

	_, err = ParseSteps([]byte(`[{"name":"Run"}]`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no \"do\" verb")
}

func TestParseTraceReadAndExpect(t *testing.T) {
	steps, err := ParseTrace(strings.NewReader(`{"do":"read","valueContains":"zoom","role":"label","pattern":"zoom (?P<z0>[\\d.]+)"}
{"do":"expect","of":"z1","minus":"z0","approx":1,"tol":0.011}
{"do":"click","x":360,"y":230,"xFrom":"ox","yFrom":"oy"}`))
	require.NoError(t, err)
	require.Len(t, steps, 3)
	assert.Equal(t, `zoom (?P<z0>[\d.]+)`, steps[0].Pattern)
	require.NotNil(t, steps[1].Approx)
	assert.Equal(t, 1.0, *steps[1].Approx)
	assert.Nil(t, steps[1].Eq, "an absent comparison stays absent: eq 0 is a real assertion")
	assert.Equal(t, "ox", steps[2].XFrom)
	assert.True(t, requiresAnchor("read"))
}

func TestParseTraceCaptureSidecars(t *testing.T) {
	steps, err := ParseTrace(strings.NewReader(
		`{"do":"capture","text":"chart","sidecars":["svg","tree"]}`))
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.True(t, steps[0].wants(SidecarSVG))
	assert.True(t, steps[0].wants(SidecarTree))

	_, err = ParseTrace(strings.NewReader(`{"do":"capture","text":"c","sidecars":["pdf"]}`))
	require.Error(t, err, "a sidecar nobody writes is refused at parse time, not after the run")
	_, err = ParseTrace(strings.NewReader(`{"do":"click","name":"Run","sidecars":["svg"]}`))
	require.Error(t, err, "only a capture writes sidecars")
	_, err = ParseSteps([]byte(`[{"do":"tree","sidecars":["tree"]}]`))
	require.Error(t, err, "ParseSteps applies the same checks")
}

func TestSidecarFileMirrorsTheHostNaming(t *testing.T) {
	assert.Equal(t, "a.png", SidecarFile("a", ""))
	assert.Equal(t, "a.png", SidecarFile("a.png", ""))
	assert.Equal(t, "a.svg", SidecarFile("a.PNG", SidecarSVG))
	assert.Equal(t, "a.tree.jsonl", SidecarFile("a", SidecarTree))
	assert.Equal(t, "a.b.svg", SidecarFile("a.b", SidecarSVG))
}
