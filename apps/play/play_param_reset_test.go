package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetTestApp builds an instance around a buffer, the way an applet's mint
// does: the prelude it opens with is what Reset restores to.
func resetTestApp(sql string) *PlayApp {
	return NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), sql, nil)
}

// drafted seeds the slot bookkeeping a frame would have built, so the reset
// path can be exercised without one.
func drafted(inst *PlayApp, values map[string]string) {
	inst.paramSlots = nil
	inst.paramDrafts = make(map[string]*string, len(values))
	for name, v := range values {
		inst.paramSlots = append(inst.paramSlots, paramSlot{Name: name, Type: "String"})
		draft := v
		inst.paramDrafts[name] = &draft
	}
}

func TestCaptureParamDefaultsReadsThePrelude(t *testing.T) {
	inst := resetTestApp("SET param_level = 2;\nSET param_catalog = 'boxer';\nSELECT {level:UInt8}, {catalog:String}")
	assert.Equal(t, map[string]string{"level": "2", "catalog": "boxer"}, inst.paramDefaults)
}

// A buffer with no prelude, or one that does not parse, leaves no defaults —
// and the control is then absent rather than restoring something invented.
func TestCaptureParamDefaultsIsAbsentWithoutAPrelude(t *testing.T) {
	assert.Empty(t, resetTestApp("SELECT 1").paramDefaults)
	assert.Empty(t, resetTestApp("").paramDefaults)
	assert.Empty(t, resetTestApp("this is not sql").paramDefaults)
}

// The load-bearing property: the defaults are the values the buffer was LOADED
// with, so a widget's drift — which rewrites the prelude — must not move them.
// Recomputing them on demand would make Reset restore whatever the reader last
// did.
func TestParamDefaultsSurviveAPreludeRewrite(t *testing.T) {
	inst := resetTestApp("SET param_level = 2;\nSELECT {level:UInt8}")
	drafted(inst, map[string]string{"level": "4"})

	out, changed := SyncParamPrelude(inst.sql, inst.paramSlots, map[string]string{"level": "4"})
	require.True(t, changed)
	inst.sql = out
	assert.Contains(t, inst.sql, "4", "the buffer now carries the reader's value")

	assert.Equal(t, map[string]string{"level": "2"}, inst.paramDefaults)
	assert.True(t, inst.paramsMovedFromDefaults())

	inst.resetParamsToDefaults()
	assert.Equal(t, "2", *inst.paramDrafts["level"])
	assert.False(t, inst.paramsMovedFromDefaults())
}

// The gesture appears only when it would do something, which is also what makes
// its appearing the signal that a knob has been moved.
func TestParamsMovedFromDefaults(t *testing.T) {
	inst := resetTestApp("SET param_level = 2;\nSELECT {level:UInt8}")
	drafted(inst, map[string]string{"level": "2"})
	assert.False(t, inst.paramsMovedFromDefaults(), "at the default there is nothing to reset")

	*inst.paramDrafts["level"] = "3"
	assert.True(t, inst.paramsMovedFromDefaults())

	// A name the buffer never bound is live — its value belongs to a panel, and
	// taking it back to a value the buffer never stated would overrule it.
	drafted(inst, map[string]string{"tl_min": "17"})
	assert.False(t, inst.paramsMovedFromDefaults())
	inst.resetParamsToDefaults()
	assert.Equal(t, "17", *inst.paramDrafts["tl_min"], "a live name keeps its value")
}

// A reset asks for a run: the knobs are what the query reads, so leaving the
// old result up would make the button look like it had not worked.
func TestResetRequestsARun(t *testing.T) {
	inst := resetTestApp("SET param_level = 2;\nSELECT {level:UInt8}")
	drafted(inst, map[string]string{"level": "4"})
	require.False(t, inst.requestRun)

	inst.resetParamsToDefaults()
	assert.True(t, inst.requestRun)

	// Idempotent: a second reset moves nothing, so it asks for nothing.
	inst.requestRun = false
	inst.resetParamsToDefaults()
	assert.False(t, inst.requestRun)
}

// A whole-buffer swap is a new buffer, so its prelude is the new default.
func TestReplacedBufferBringsItsOwnDefaults(t *testing.T) {
	inst := resetTestApp("SET param_level = 2;\nSELECT {level:UInt8}")
	inst.ReplaceSql("SET param_level = 4;\nSELECT {level:UInt8}")
	inst.consumePendingSnippet()
	assert.Equal(t, map[string]string{"level": "4"}, inst.paramDefaults)
}
