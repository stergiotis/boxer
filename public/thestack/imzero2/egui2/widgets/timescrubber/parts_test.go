package timescrubber

import (
	"testing"

	"github.com/stretchr/testify/assert"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

// A hidden part takes its keys with it; Home and End stay while either of
// the two parts they serve is shown (ADR-0251 §SD10).
func TestAHiddenPartTakesItsKeys(t *testing.T) {
	mask := func(hide PartE) keycodes.Mask { return (&Scrubber{Opts: Options{Hide: hide}}).keyMask() }
	all := keycodes.MaskOf(keycodes.Space, keycodes.ArrowLeft, keycodes.ArrowRight,
		keycodes.Home, keycodes.End, keycodes.PageUp, keycodes.PageDown, keycodes.Delete)
	assert.Equal(t, all, mask(0))
	assert.Equal(t, all&^keycodes.MaskOf(keycodes.Space), mask(PartPlay))
	assert.Equal(t, all&^keycodes.MaskOf(keycodes.Delete), mask(PartRange), "Home and End still step")
	assert.Equal(t, keycodes.MaskOf(keycodes.Space, keycodes.Home, keycodes.End, keycodes.Delete), mask(PartStepping), "Shift+Home and Shift+End still set the range")
	assert.Equal(t, keycodes.MaskOf(keycodes.Space), mask(PartStepping|PartRange))
	assert.Equal(t, keycodes.Mask(0), mask(AllParts))
	assert.Equal(t, all, mask(PartRate|PartMode|PartNow|PartTimeField|PartReadout), "parts without keys take none")
}

// Hiding every part leaves the strip alone: the frame costs fewer messages,
// and hiding the parts of one row only costs fewer still than hiding none.
func TestHiddenPartsAreNotDrawn(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	steps := stepsAt(0, 1, 2, 3, 4, 5)
	count := func(opts Options) int {
		opts.NoKeyboard = true
		sc := New(c.NewWidgetIdStack(), "parts", opts)
		sm := c.CurrentApplicationState.StateManager
		sm.ScriptReset()
		sc.Render(sceneW, steps)
		sm.ScriptReset()
		zero()
		sc.Render(sceneW, steps)
		return messages()
	}
	full, transport, readout, none := count(Options{}), count(Options{Hide: transportParts}), count(Options{Hide: PartTimeField | PartReadout}), count(Options{Hide: AllParts})
	assert.Less(t, transport, full, "the transport row is gone")
	assert.Less(t, readout, full, "the readout row is gone")
	assert.Less(t, none, transport)
	assert.Less(t, none, readout)
	compact, compactBare := count(Options{Compact: true}), count(Options{Compact: true, Hide: PartPlay | PartReadout})
	assert.Less(t, compactBare, compact, "the one-line form honours it too")
}
