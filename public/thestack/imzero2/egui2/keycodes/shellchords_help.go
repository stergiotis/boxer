package keycodes

import (
	"strings"
)

// Description says what the chord does, in the words of the help book's key
// reference (ShellChordsHelpPage). Every chord has one; a test fails on a
// chord without.
func (inst ShellChordBit) Description() (s string) {
	switch inst {
	case ChordFocusNext:
		s = "Focus the next window: in the layout's order when tiled, in the stacking when floating."
	case ChordFocusPrev:
		s = "Focus the previous window, in the same order."
	case ChordFocusLeft:
		s = "Focus the nearest window to the left."
	case ChordFocusRight:
		s = "Focus the nearest window to the right."
	case ChordFocusUp:
		s = "Focus the nearest window above."
	case ChordFocusDown:
		s = "Focus the nearest window below."
	case ChordSwapNext:
		s = "Swap the active window with the next one in the layout (tiled)."
	case ChordSwapPrev:
		s = "Swap the active window with the previous one in the layout (tiled)."
	case ChordSnapLeft:
		s = "Snap the active window to the left half of the work area; again for the quarter (floating)."
	case ChordSnapRight:
		s = "Snap the active window to the right half; again for the quarter (floating)."
	case ChordSnapUp:
		s = "Snap the active window to the top half; again for the quarter (floating)."
	case ChordSnapDown:
		s = "Snap the active window to the bottom half; again for the quarter (floating)."
	case ChordPromote:
		s = "Make the active window the main one (tiled)."
	case ChordCycleLayout:
		s = "Step to the next tiled layout; on a floating desktop, switch to tiled."
	case ChordToggleFloat:
		s = "Take the active window out of the layout, or put it back (tiled)."
	case ChordMonocle:
		s = "Show only the active window (tiled), or maximise it (floating)."
	case ChordRatioUp:
		s = "Widen the main area."
	case ChordRatioDown:
		s = "Narrow the main area."
	case ChordCountUp:
		s = "Put one more window in the main area."
	case ChordCountDown:
		s = "Put one fewer window in the main area."
	case ChordPrevActive:
		s = "Focus the window that was active before this one."
	}
	return
}

// ShellChordsHelpPath is where ShellChordsHelpPage is committed, relative
// to the repository root: the help app's own book, which covers the
// desktop the apps run on.
const ShellChordsHelpPath = "public/keelson/runtime/helphost/help/reference/desktop-keys.md"

// ShellChordsHelpPage renders the chord table as the help book's key
// reference: one row per chord, every key combination that sets it. It is
// committed at ShellChordsHelpPath and rebuilt and compared by a test, so
// the page cannot drift from the table the host matches on.
func ShellChordsHelpPage() string {
	var b strings.Builder
	b.WriteString(`---
type: reference
audience: end-user
status: draft
title: Desktop keys
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Desktop keys

These keys belong to the desktop, not to the window that has focus: the
window never sees them. They all use Alt. A key that differs between
keyboards is listed under each spelling. The table is generated from the
one the desktop matches keys on.

| Keys | What it does |
| --- | --- |
`)
	keys := make(map[ShellChordBit][]string, ShellChordCount)
	for _, c := range ShellChords {
		keys[c.Bit] = append(keys[c.Bit], chordLabel(c))
	}
	for bit := ShellChordBit(0); bit < shellChordCount; bit++ {
		b.WriteString("| ")
		b.WriteString(strings.Join(keys[bit], ", "))
		b.WriteString(" | ")
		b.WriteString(bit.Description())
		b.WriteString(" |\n")
	}
	return b.String()
}

func chordLabel(c ShellChord) string {
	parts := []string{"Alt"}
	if c.Shift {
		parts = append(parts, "Shift")
	}
	parts = append(parts, keyLabel(c.Key))
	return strings.Join(parts, "+")
}

// keyLabel is how a key is printed on a keycap or spoken.
func keyLabel(k Code) string {
	switch k {
	case Plus:
		return "Plus"
	case Minus:
		return "Minus"
	case Equals:
		return "Equals"
	case ArrowLeft:
		return "Left"
	case ArrowRight:
		return "Right"
	case ArrowUp:
		return "Up"
	case ArrowDown:
		return "Down"
	}
	for _, e := range Table {
		if e.Code == k {
			return strings.TrimPrefix(strings.TrimPrefix(e.Name, "Key"), "Digit")
		}
	}
	return "?"
}
