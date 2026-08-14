package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanEnumHints(t *testing.T) {
	sql := "SET param_level = 0;\n" +
		"-- play: enum level 0=All levels,1=Macro,2=Meso,3=Micro,4=Block\n" +
		"--play: enum size_by bytes,count\n" +
		"  --  PLAY: ENUM catalog  =All catalogs, boxer , pebble  \n" +
		"SELECT 1"

	hints := scanEnumHints(sql)
	require.Len(t, hints, 3)

	assert.Equal(t, []enumOption{
		{Value: "0", Label: "All levels"},
		{Value: "1", Label: "Macro"},
		{Value: "2", Label: "Meso"},
		{Value: "3", Label: "Micro"},
		{Value: "4", Label: "Block"},
	}, hints["level"])

	// A bare option is its own label.
	assert.Equal(t, []enumOption{{Value: "bytes", Label: "bytes"}, {Value: "count", Label: "count"}}, hints["size_by"])

	// An empty value with a label is the "no filter" entry every browsing UI
	// opens with, and whitespace around either half is the author's formatting.
	assert.Equal(t, []enumOption{
		{Value: "", Label: "All catalogs"},
		{Value: "boxer", Label: "boxer"},
		{Value: "pebble", Label: "pebble"},
	}, hints["catalog"])
}

// A marker that is not yet a marker must not make the pane shout: this scan
// runs every frame over a buffer somebody may be halfway through typing.
func TestScanEnumHintsIgnoresWhatItCannotUse(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1 -- play: enum level 1=Macro", // not a line comment
		"-- play: enum",                        // no name, no options
		"-- play: enum level",                  // a name and no options
		"-- play: enum level   ",               // an empty option list
		"-- play: enum level ,,,",              // nothing but separators
		"-- play: enumlevel 1=Macro",           // not the marker
		"-- some other comment",
	} {
		assert.Emptyf(t, scanEnumHints(sql), "%q", sql)
	}
}

// Two hints for one slot is an authoring mistake either way; taking the first
// keeps the pane's answer independent of where the second was added.
func TestScanEnumHintsFirstWins(t *testing.T) {
	hints := scanEnumHints("-- play: enum k a\n-- play: enum k b\nSELECT 1")
	assert.Equal(t, []enumOption{{Value: "a", Label: "a"}}, hints["k"])
}

// A repeated value would render two choices that look different and do the
// same thing.
func TestParseEnumOptionsDropsRepeatedValues(t *testing.T) {
	assert.Equal(t, []enumOption{{Value: "a", Label: "first"}},
		parseEnumOptions("a=first, a=second"))
}

func TestEnumWidgetMatchesOnlyHintedSlots(t *testing.T) {
	w := newEnumWidget()
	slots := []paramSlot{{Name: "filter", Type: "String"}, {Name: "level", Type: "UInt8"}}

	_, ok := w.Matches(slots)
	assert.False(t, ok, "no hints, no claim — the slot belongs to the text field")

	w.SetEnumHints(map[string][]enumOption{"level": {{Value: "1", Label: "Macro"}}})
	idx, ok := w.Matches(slots)
	require.True(t, ok)
	assert.Equal(t, []int{1}, idx, "one slot per call, the first hinted one")

	// A hint naming nothing in the buffer claims nothing.
	w.SetEnumHints(map[string][]enumOption{"nosuchslot": {{Value: "1", Label: "One"}}})
	_, ok = w.Matches(slots)
	assert.False(t, ok)
}

func TestEnumLabelFor(t *testing.T) {
	opts := []enumOption{{Value: "", Label: "All levels"}, {Value: "1", Label: "Macro"}}

	label, known := enumLabelFor(opts, "1")
	assert.Equal(t, "Macro", label)
	assert.True(t, known)

	// The empty value is a value when the list offers it.
	label, known = enumLabelFor(opts, "")
	assert.Equal(t, "All levels", label)
	assert.True(t, known)

	// A hand-edited SET, or a list that changed under a saved value: show what
	// the buffer holds and mark it, rather than a blank control.
	label, known = enumLabelFor(opts, "7")
	assert.Equal(t, "7", label)
	assert.False(t, known)

	label, known = enumLabelFor([]enumOption{{Value: "a", Label: "a"}}, "")
	assert.Equal(t, "—", label, "an unset value still needs a hit area")
	assert.False(t, known)
}

// The marker has to survive the thing that rewrites the buffer around it: a
// knob's drift rebuilds the SET prelude the markers sit under, and a rewrite
// that dropped them would make the dropdown work exactly once.
func TestEnumHintsSurviveAPreludeRewrite(t *testing.T) {
	sql := "SET param_level = 0;\n" +
		"-- play: enum level 0=All levels,1=Macro\n" +
		"\nSELECT {level:UInt8}"

	slots, err := ExtractParamSlots(sql)
	require.NoError(t, err)
	out, changed := SyncParamPrelude(sql, slots, map[string]string{"level": "1"})
	require.True(t, changed)
	assert.Contains(t, out, "SET param_level = 1")
	assert.Equal(t, scanEnumHints(sql), scanEnumHints(out), "the options are the same after the rewrite")
}

// The typo case: its symptom is otherwise indistinguishable from having written
// no hint at all.
func TestOrphanEnumHints(t *testing.T) {
	slots := []paramSlot{{Name: "level"}}
	hints := map[string][]enumOption{
		"level":  {{Value: "1", Label: "Macro"}},
		"leve":   {{Value: "1", Label: "Macro"}},
		"catalg": {{Value: "b", Label: "b"}},
	}
	assert.Equal(t, []string{"catalg", "leve"}, orphanEnumHints(hints, slots), "sorted, so the line is stable")
	assert.Contains(t, orphanEnumNote([]string{"leve"}), "leve")
	assert.Empty(t, orphanEnumNote(nil))
	assert.Empty(t, orphanEnumHints(nil, slots))
}
