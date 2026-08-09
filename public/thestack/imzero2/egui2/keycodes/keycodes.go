// Package keycodes is the key vocabulary imzero2 widgets capture over the FFI
// (ADR-0177 SD4). It is deliberately a SUBSET — the navigation and activation
// keys a widget needs — rather than a transcription of `egui::Key`: a subset is
// a registry to extend on demand, a full mirror is a standing obligation
// against an upstream enum for keys nobody has asked for.
//
// # One table, two sides
//
// [Table] is the single definition. The Go constants below are its entries, and
// the interpreter's `egui::Key` match arm is GENERATED from it at codegen time
// (`definition/egui2_definition_d_keys.go` walks this slice to build the Rust).
// So the wire code, the Go name and the egui key cannot drift from each other;
// adding a key is one row here plus a regeneration of both FFI sides.
//
// The ADR says "one IDL-side table". This package is that table, moved one step
// out: the IDL builds its Rust from it, and Go callers use it directly. Putting
// it in `definition` would have made the constants unreachable from `bindings`
// and from widget code, since `definition` is the generator's input and nothing
// else imports it.
//
// # Codes are wire values
//
// A [Code] crosses the FFI as a u8 and is therefore a CONTRACT. Append new keys
// with new numbers; never renumber, and never reuse the number of a key that is
// removed — a value that changes meaning compiles on both sides and lies at
// runtime, the same reasoning that retired rather than recycled `ResponseFlags`
// bit 30 (ADR-0176 M5).
package keycodes

// Code is a key's wire value, one byte.
type Code uint8

// The vocabulary. Numbers are wire values: append, never renumber.
const (
	Unknown   Code = 0
	ArrowUp   Code = 1
	ArrowDown Code = 2
	// ArrowLeft / ArrowRight are here for a tree's collapse / expand, which is
	// what a file manager binds them to; a list widget may ignore them.
	ArrowLeft  Code = 3
	ArrowRight Code = 4
	Home       Code = 5
	End        Code = 6
	PageUp     Code = 7
	PageDown   Code = 8
	Enter      Code = 9
	Space      Code = 10
	Escape     Code = 11
	// Tab is capturable but rarely SHOULD be: consuming it takes the key that
	// leaves the widget, so a capturing widget becomes a focus trap. ADR-0177
	// SD9 makes a container one focus stop; let Tab through unless the widget
	// genuinely owns an internal tab order.
	Tab       Code = 12
	Backspace Code = 13
	Delete    Code = 14
)

// Entry is one row of the vocabulary: the wire code, the Go constant's name,
// and the `egui::Key` variant the interpreter matches it from.
type Entry struct {
	Code    Code
	Name    string
	EguiKey string
}

// Table is the vocabulary, in wire order. The Rust match arm is built from it.
var Table = []Entry{
	{ArrowUp, "ArrowUp", "ArrowUp"},
	{ArrowDown, "ArrowDown", "ArrowDown"},
	{ArrowLeft, "ArrowLeft", "ArrowLeft"},
	{ArrowRight, "ArrowRight", "ArrowRight"},
	{Home, "Home", "Home"},
	{End, "End", "End"},
	{PageUp, "PageUp", "PageUp"},
	{PageDown, "PageDown", "PageDown"},
	{Enter, "Enter", "Enter"},
	{Space, "Space", "Space"},
	{Escape, "Escape", "Escape"},
	{Tab, "Tab", "Tab"},
	{Backspace, "Backspace", "Backspace"},
	{Delete, "Delete", "Delete"},
}

// Mask is the set of keys a widget declares it captures (ADR-0177 SD3). A
// widget states what it eats, so runtime-global shortcuts — F1, Ctrl+Enter —
// keep reaching their owners even while it has focus.
//
// One bit per [Code], so the vocabulary is capped at 64 keys. That is a
// deliberate ceiling rather than an oversight: past it, the thing being built
// is a keymap, and a keymap wants names and rebinding rather than a wider mask.
type Mask uint64

// MaskOf builds a mask from codes.
func MaskOf(codes ...Code) (m Mask) {
	for _, c := range codes {
		m |= Mask(1) << uint(c)
	}
	return
}

// Has reports whether the mask declares this code.
func (inst Mask) Has(c Code) bool {
	return inst&(Mask(1)<<uint(c)) != 0
}

// Navigation is the set a list- or tree-shaped widget wants: move, page, jump,
// and activate. Deliberately excludes Tab (see the constant) and Escape, which
// usually belongs to whatever the widget is inside.
var Navigation = MaskOf(ArrowUp, ArrowDown, ArrowLeft, ArrowRight,
	Home, End, PageUp, PageDown, Enter, Space)

// String names a code for logs and demos; unknown codes print their number.
func (inst Code) String() string {
	for _, e := range Table {
		if e.Code == inst {
			return e.Name
		}
	}
	return "Code(" + itoa(uint8(inst)) + ")"
}

func itoa(v uint8) string {
	if v == 0 {
		return "0"
	}
	var b [3]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
