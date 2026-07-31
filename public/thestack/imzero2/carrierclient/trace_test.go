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
	for _, verb := range []string{"capture", "key", "scroll", "cadence", "resize", "note"} {
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
