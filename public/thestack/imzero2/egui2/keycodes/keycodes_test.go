package keycodes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The vocabulary is a WIRE CONTRACT (ADR-0177 SD4): the interpreter's match arm
// is generated from Table, and a widget's captured events carry these numbers.
// These tests exist to make a renumbering loud, since nothing else would catch
// one — both sides would still compile, and the wrong key would arrive.

func TestTableCodesAreStableWireValues(t *testing.T) {
	// Pinned literals, deliberately not derived from the constants: a test that
	// read them back from the same constants it guards would pass through any
	// renumbering.
	want := map[string]Code{
		"ArrowUp": 1, "ArrowDown": 2, "ArrowLeft": 3, "ArrowRight": 4,
		"Home": 5, "End": 6, "PageUp": 7, "PageDown": 8,
		"Enter": 9, "Space": 10, "Escape": 11, "Tab": 12,
		"Backspace": 13, "Delete": 14,
	}
	require.Len(t, Table, len(want), "a key was added or removed; extend the pinned map deliberately")
	for _, e := range Table {
		w, ok := want[e.Name]
		require.True(t, ok, "%s is not in the pinned map", e.Name)
		assert.Equal(t, w, e.Code, "%s changed its wire value", e.Name)
	}
}

func TestTableHasNoDuplicatesAndNoZero(t *testing.T) {
	seenCode := map[Code]string{}
	seenName := map[string]bool{}
	seenEgui := map[string]bool{}
	for _, e := range Table {
		assert.NotEqual(t, Unknown, e.Code, "%s uses the reserved zero code", e.Name)
		if prev, dup := seenCode[e.Code]; dup {
			t.Errorf("%s and %s share wire code %d", prev, e.Name, e.Code)
		}
		seenCode[e.Code] = e.Name
		assert.False(t, seenName[e.Name], "duplicate name %s", e.Name)
		assert.False(t, seenEgui[e.EguiKey], "duplicate egui::Key %s", e.EguiKey)
		seenName[e.Name], seenEgui[e.EguiKey] = true, true
	}
}

func TestMaskRoundTrips(t *testing.T) {
	m := MaskOf(ArrowUp, PageDown, Enter)
	assert.True(t, m.Has(ArrowUp))
	assert.True(t, m.Has(PageDown))
	assert.True(t, m.Has(Enter))
	assert.False(t, m.Has(ArrowDown))
	assert.False(t, m.Has(Escape))
	assert.Zero(t, Mask(0), "the zero mask captures nothing, so a widget that declares none pays none")
}

func TestNavigationSetIsWhatAListWants(t *testing.T) {
	for _, c := range []Code{ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Home, End, PageUp, PageDown, Enter, Space} {
		assert.True(t, Navigation.Has(c), "%s should be in Navigation", c)
	}
	// Consuming Tab would make a capturing widget a focus trap (SD9), and
	// Escape usually belongs to whatever the widget sits inside.
	assert.False(t, Navigation.Has(Tab))
	assert.False(t, Navigation.Has(Escape))
}

func TestCodeStringNamesEveryTableEntry(t *testing.T) {
	for _, e := range Table {
		assert.Equal(t, e.Name, e.Code.String())
	}
	assert.Equal(t, "Code(200)", Code(200).String(), "an unknown code prints its number rather than lying")
	assert.Equal(t, "Code(0)", Unknown.String())
}

// The mask is 64 bits wide, so the vocabulary cannot silently outgrow it.
func TestVocabularyFitsTheMask(t *testing.T) {
	for _, e := range Table {
		require.Less(t, int(e.Code), 64,
			"%s does not fit the 64-bit mask; past this the thing being built is a keymap", e.Name)
	}
}
