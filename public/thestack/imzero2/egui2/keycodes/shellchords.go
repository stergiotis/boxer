package keycodes

// The shell's chords (ADR-0346 §SD5): the key combinations the window host
// owns for the whole process, under one modifier. Rust drains them once per
// frame into one bit set (fetchShellChords) and consumes the events, so no
// widget sees them; Go reads the set and acts. Like [Table], this is one
// definition for two sides: the interpreter's match arm is generated from it
// (definition.ShellChordsRustFile) and a test fails on any drift.
//
// The modifier is Alt — egui binds nothing to it, the window managers the
// host runs under leave Alt+letter to the application, and i3's default
// modifier is Alt. Shift extends a chord; a chord with Ctrl or the command
// key held is not the shell's. A key appears under more than one row where
// keyboards disagree on what Shift makes of it (Plus and Equals).

// ShellChordBit is a chord's position in the per-frame bit set.
type ShellChordBit uint8

const (
	// ChordFocusNext and ChordFocusPrev move the active window along the
	// layout's order (tiled) or the stacking (floating).
	ChordFocusNext ShellChordBit = iota
	ChordFocusPrev
	// ChordFocusLeft .. ChordFocusDown pick the nearest window in that
	// direction.
	ChordFocusLeft
	ChordFocusRight
	ChordFocusUp
	ChordFocusDown
	// ChordSwapNext and ChordSwapPrev swap the active window with its
	// neighbour in the order (tiled).
	ChordSwapNext
	ChordSwapPrev
	// ChordSnapLeft .. ChordSnapDown snap the active window to that half of
	// the work area, a second press to the quarter (floating).
	ChordSnapLeft
	ChordSnapRight
	ChordSnapUp
	ChordSnapDown
	// ChordPromote makes the active window the main (tiled).
	ChordPromote
	// ChordCycleLayout steps to the next tiled layout; on a floating desktop
	// it switches to tiled.
	ChordCycleLayout
	// ChordToggleFloat takes the active window out of the layout or puts it
	// back (tiled).
	ChordToggleFloat
	// ChordMonocle toggles monocle (tiled) or maximizes (floating).
	ChordMonocle
	// ChordRatioUp and ChordRatioDown move the main ratio.
	ChordRatioUp
	ChordRatioDown
	// ChordCountUp and ChordCountDown move the main count.
	ChordCountUp
	ChordCountDown
	// ChordPrevActive focuses the previously active window.
	ChordPrevActive
	shellChordCount
)

// ShellChordCount is how many chords there are; the bit set is a u64.
const ShellChordCount = int(shellChordCount)

// ShellChord is one row of the chord table: the bit it sets, its name, the
// key and whether Shift is held.
type ShellChord struct {
	Bit   ShellChordBit
	Name  string
	Key   Code
	Shift bool
}

// ShellChords is the chord table, the Rust match arm's source. A (Key,
// Shift) pair appears at most once; a bit may have several rows.
var ShellChords = []ShellChord{
	{ChordFocusNext, "FocusNext", KeyJ, false},
	{ChordFocusPrev, "FocusPrev", KeyK, false},
	{ChordFocusLeft, "FocusLeft", KeyH, false},
	{ChordFocusRight, "FocusRight", KeyL, false},
	{ChordFocusLeft, "FocusLeft", ArrowLeft, false},
	{ChordFocusRight, "FocusRight", ArrowRight, false},
	{ChordFocusUp, "FocusUp", ArrowUp, false},
	{ChordFocusDown, "FocusDown", ArrowDown, false},
	{ChordSwapNext, "SwapNext", KeyJ, true},
	{ChordSwapPrev, "SwapPrev", KeyK, true},
	{ChordSnapLeft, "SnapLeft", ArrowLeft, true},
	{ChordSnapRight, "SnapRight", ArrowRight, true},
	{ChordSnapUp, "SnapUp", ArrowUp, true},
	{ChordSnapDown, "SnapDown", ArrowDown, true},
	{ChordPromote, "Promote", Enter, false},
	{ChordCycleLayout, "CycleLayout", Space, false},
	{ChordToggleFloat, "ToggleFloat", Space, true},
	{ChordMonocle, "Monocle", KeyF, false},
	{ChordRatioUp, "RatioUp", Equals, false},
	{ChordRatioDown, "RatioDown", Minus, false},
	// Shift+= is Plus on most keyboards and Equals with Shift on some;
	// a keypad gives Plus without Shift.
	{ChordCountUp, "CountUp", Plus, false},
	{ChordCountUp, "CountUp", Plus, true},
	{ChordCountUp, "CountUp", Equals, true},
	{ChordCountDown, "CountDown", Minus, true},
	{ChordPrevActive, "PrevActive", Tab, false},
}

// Mask is the bit set with only this chord's bit.
func (inst ShellChordBit) Mask() uint64 { return 1 << uint64(inst) }

// Has is whether the bit set carries this chord.
func (inst ShellChordBit) Has(set uint64) bool { return set&inst.Mask() != 0 }
