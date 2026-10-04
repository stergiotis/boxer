// Package keycodes is the key vocabulary imzero2 widgets capture over the FFI
// (ADR-0177 SD4, extended by ADR-0279 §SD2). It is deliberately a SUBSET — the
// navigation and activation keys a widget needs, and the digits, letters and
// punctuation typed into one — rather than a transcription of `egui::Key`: a subset is
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
	// Printable keys (ADR-0279 §SD2): digits, letters and the punctuation a
	// calculator or an editor wants, as physical keys — a shifted character
	// such as "*" arrives as its key with Shift in the modifier byte. Codes
	// 61–63 are free; past them the mask has to widen.
	Digit0       Code = 15
	Digit1       Code = 16
	Digit2       Code = 17
	Digit3       Code = 18
	Digit4       Code = 19
	Digit5       Code = 20
	Digit6       Code = 21
	Digit7       Code = 22
	Digit8       Code = 23
	Digit9       Code = 24
	KeyA         Code = 25
	KeyB         Code = 26
	KeyC         Code = 27
	KeyD         Code = 28
	KeyE         Code = 29
	KeyF         Code = 30
	KeyG         Code = 31
	KeyH         Code = 32
	KeyI         Code = 33
	KeyJ         Code = 34
	KeyK         Code = 35
	KeyL         Code = 36
	KeyM         Code = 37
	KeyN         Code = 38
	KeyO         Code = 39
	KeyP         Code = 40
	KeyQ         Code = 41
	KeyR         Code = 42
	KeyS         Code = 43
	KeyT         Code = 44
	KeyU         Code = 45
	KeyV         Code = 46
	KeyW         Code = 47
	KeyX         Code = 48
	KeyY         Code = 49
	KeyZ         Code = 50
	Plus         Code = 51
	Minus        Code = 52
	Equals       Code = 53
	Period       Code = 54
	Comma        Code = 55
	Slash        Code = 56
	Colon        Code = 57
	Quote        Code = 58
	OpenBracket  Code = 59
	CloseBracket Code = 60
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
	{Digit0, "Digit0", "Num0"},
	{Digit1, "Digit1", "Num1"},
	{Digit2, "Digit2", "Num2"},
	{Digit3, "Digit3", "Num3"},
	{Digit4, "Digit4", "Num4"},
	{Digit5, "Digit5", "Num5"},
	{Digit6, "Digit6", "Num6"},
	{Digit7, "Digit7", "Num7"},
	{Digit8, "Digit8", "Num8"},
	{Digit9, "Digit9", "Num9"},
	{KeyA, "KeyA", "A"},
	{KeyB, "KeyB", "B"},
	{KeyC, "KeyC", "C"},
	{KeyD, "KeyD", "D"},
	{KeyE, "KeyE", "E"},
	{KeyF, "KeyF", "F"},
	{KeyG, "KeyG", "G"},
	{KeyH, "KeyH", "H"},
	{KeyI, "KeyI", "I"},
	{KeyJ, "KeyJ", "J"},
	{KeyK, "KeyK", "K"},
	{KeyL, "KeyL", "L"},
	{KeyM, "KeyM", "M"},
	{KeyN, "KeyN", "N"},
	{KeyO, "KeyO", "O"},
	{KeyP, "KeyP", "P"},
	{KeyQ, "KeyQ", "Q"},
	{KeyR, "KeyR", "R"},
	{KeyS, "KeyS", "S"},
	{KeyT, "KeyT", "T"},
	{KeyU, "KeyU", "U"},
	{KeyV, "KeyV", "V"},
	{KeyW, "KeyW", "W"},
	{KeyX, "KeyX", "X"},
	{KeyY, "KeyY", "Y"},
	{KeyZ, "KeyZ", "Z"},
	{Plus, "Plus", "Plus"},
	{Minus, "Minus", "Minus"},
	{Equals, "Equals", "Equals"},
	{Period, "Period", "Period"},
	{Comma, "Comma", "Comma"},
	{Slash, "Slash", "Slash"},
	{Colon, "Colon", "Colon"},
	{Quote, "Quote", "Quote"},
	{OpenBracket, "OpenBracket", "OpenBracket"},
	{CloseBracket, "CloseBracket", "CloseBracket"},
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
