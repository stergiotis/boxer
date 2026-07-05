package bindings

import (
	"strconv"
	"strings"
)

// U64Edit is a hand-written, exact 64-bit unsigned-integer input built as a
// composition over TextEdit — NOT over DragValue/Slider.
//
// Why it exists: egui's DragValue and Slider are f64 scrubbers by
// construction. Both funnel every value (even for plain display, untouched)
// through emath::Numeric::to_f64 / from_f64, so a uint64 above 2^53 is
// silently rounded, and their hexadecimal/octal/binary formatters additionally
// cast the f64 to i64, saturating at i64::MAX (0x7FFFFFFFFFFFFFFF). That makes
// DragValueU64 / SliderU64 unusable for wide values — tagged ids, hashes,
// bitmasks — which are always > 2^53. There is no exact-integer widget upstream
// in egui; the idiomatic exact path is a text field parsed by hand, which is
// what this wraps. DragValueU64 / SliderU64 remain fine for small magnitudes
// (indent counts, page numbers, small enums).
//
// The value is read/written exactly via strconv.ParseUint / FormatUint across
// the whole uint64 range. Input is accepted as decimal or 0x-hex; display is
// decimal by default, or 0x-hex with Hex().
//
// Usage mirrors DragValueU64 — pass the current value in, bind the same
// variable out:
//
//	c.U64Edit(ids.PrepareStr("id"), myId).
//	    Hex().HintText("id — decimal or 0x-hex").DesiredWidth(320).
//	    SendRespVal(&myId)
type U64EditFluid struct {
	// eid is the effective widget id, derived once from the caller's id
	// creator (Derive consumes the prepared state, so it must not be
	// derived twice — the inner TextEdit is handed a pre-derived absolute
	// id instead).
	eid uint64
	val uint64

	hexDisplay     bool
	hint           string
	width          float32
	hasWidth       bool
	interactive    bool
	hasInteractive bool
}

// u64EditState is the per-widget draft backing one U64Edit. draft is the
// stable string bound to the inner TextEdit; reflects records the uint64 value
// that draft currently represents, so a genuine external change to the value
// (a preset button, a network update) can be told apart from the user's own
// in-progress typing — only the former re-seeds the field.
type u64EditState struct {
	draft    string
	reflects uint64
	init     bool
}

// u64EditStates holds draft state per effective widget id. The c.* API is
// strictly single-threaded (main render thread only — see the "Framework Data
// Race" pitfall), so a plain map needs no locking. Entries are retained for the
// process lifetime; for the bounded set of ids a UI declares this is
// negligible, matching the existing seenIds book-keeping. deferred: eviction of
// ids not seen for N frames if an app ever churns U64Edit ids unboundedly.
var u64EditStates = map[uint64]*u64EditState{}

// U64Edit begins an exact uint64 editor bound to the widget identified by id.
// val is the current value to display. The id creator is derived immediately
// (as TextEdit does), so the returned fluid owns a stable effective id for the
// remainder of the frame.
func U64Edit(id WidgetIdCreatorI, val uint64) U64EditFluid {
	return U64EditFluid{
		eid:         id.Derive(),
		val:         val,
		interactive: true,
	}
}

// Hex displays the value as lowercase 0x-hex instead of decimal. Input parsing
// always accepts either form regardless of this setting.
func (inst U64EditFluid) Hex() U64EditFluid {
	inst.hexDisplay = true
	return inst
}

// HintText sets the placeholder shown when the field is empty.
func (inst U64EditFluid) HintText(hint string) U64EditFluid {
	inst.hint = hint
	return inst
}

// DesiredWidth pins the field width in points (forwarded to the inner
// TextEdit). Unset lets the TextEdit use its default sizing.
func (inst U64EditFluid) DesiredWidth(width float32) U64EditFluid {
	inst.width = width
	inst.hasWidth = true
	return inst
}

// Interactive toggles whether the field accepts input (forwarded to the inner
// TextEdit). Unset leaves the TextEdit default (interactive).
func (inst U64EditFluid) Interactive(interactive bool) U64EditFluid {
	inst.interactive = interactive
	inst.hasInteractive = true
	return inst
}

// SendRespVal renders the field and, on a parse-valid edit, writes the value
// back into *val exactly. It returns the inner TextEdit's response flags.
//
// Semantics:
//   - HasChanged() fires on any text edit. The value is written to *val only
//     when the text parses as a uint64; on invalid input *val is left unchanged
//     and the draft keeps the user's raw text so they can correct it.
//   - When *val (the value passed to U64Edit) changes from outside — a preset
//     button, a background update — the field re-seeds to the new value and the
//     frontend's cached buffer is dropped via OverrideDatabindingSPtr (the
//     "Stubborn Text" override). The user's own typing never triggers a
//     re-seed, because writing a parsed edit records it in reflects.
//   - Standard one-frame FFI lag applies: a keystroke on frame N is visible in
//     *val on frame N+1. Call every frame for the binding to stay live.
func (inst U64EditFluid) SendRespVal(val *uint64) ResponseFlagsE {
	// The inner TextEdit derives ensureNotZeroIdHighEntropyFast(eid) from the
	// absolute id we hand it; key the draft state (and thus every databinding
	// lookup) by that same value so they all agree.
	key := ensureNotZeroIdHighEntropyFast(inst.eid)
	st := u64EditStates[key]
	if st == nil {
		st = &u64EditState{}
		u64EditStates[key] = st
	}

	reseeded := false
	if !st.init || st.reflects != inst.val {
		st.draft = formatU64(inst.val, inst.hexDisplay)
		st.reflects = inst.val
		st.init = true
		reseeded = true
	}

	te := TextEdit(MakeAbsoluteIdHighEntropy(inst.eid), st.draft, false)
	if inst.hint != "" {
		te = te.HintText(inst.hint)
	}
	if inst.hasWidth {
		te = te.DesiredWidth(inst.width)
	}
	if inst.hasInteractive {
		te = te.Interactive(inst.interactive)
	}
	// SendRespVal registers AddR9SDatabinding(key, &st.draft); it must run
	// before OverrideDatabindingSPtr so the override can find the binding.
	resp := te.SendRespVal(&st.draft)

	if reseeded {
		// Tell the frontend to drop its cached text and adopt the re-seeded
		// draft this sync, rather than writing its stale buffer back over it.
		CurrentApplicationState.StateManager.OverrideDatabindingSPtr(&st.draft)
	} else if resp.HasChanged() {
		if v, ok := parseU64(st.draft); ok {
			*val = v
			// Record what the draft now reflects so next frame's guard does
			// not mistake the user's own edit for an external change.
			st.reflects = v
		}
	}
	return resp
}

// parseU64 reads a uint64 typed as decimal or 0x-hex, exactly across the whole
// range. Unlike the f64-backed DragValue/Slider widgets it never rounds. A
// leading "0" is decimal (not octal) to avoid surprising users; an explicit
// "0x"/"0X" prefix selects hex. Empty or malformed input returns ok=false.
func parseU64(s string) (value uint64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		v, err := strconv.ParseUint(s[2:], 16, 64)
		return v, err == nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}

// formatU64 renders v for display: decimal, or lowercase 0x-hex when hex is
// set. Both forms round-trip through parseU64 for every uint64.
func formatU64(v uint64, hex bool) string {
	if hex {
		return "0x" + strconv.FormatUint(v, 16)
	}
	return strconv.FormatUint(v, 10)
}
