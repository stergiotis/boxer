package definition

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir/idl"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// Focus and keyboard capture (ADR-0177). Two things live here: the Rust half of
// the key vocabulary, generated from the one Go table so the two cannot drift,
// and the `requestFocus` proc.
//
// The `.Focusable()` / `.CaptureKeys()` methods are on the nodes they apply to
// (frame, endETable) rather than here, because a method has to be declared on
// its own builder.

// focusIdExpr is the egui id a focusable imzero2 widget registers under, and
// the one `requestFocus` asks for. It MUST be identical in both places or focus
// silently does nothing (ADR-0177 SD7): an imzero2 widget id is only the r7
// read-back key, while egui allocates interaction ids from `ui.next_auto_id()`,
// so `request_focus` on an id egui never registered is a no-op with no warning.
//
// Salted rather than bare, so the focus registration cannot collide with a
// Frame's `.with("sense")` interaction on the same widget id.
func focusIdExpr(idExpr string) string {
	return `egui::Id::new(` + idExpr + `).with("imzero-focus")`
}

// KeyCodesRustPath is where [KeyCodesRustFile]'s content is committed.
const KeyCodesRustPath = "rust/imzero2/src/imzero2/keycodes.rs"

// KeyCodesRustFile builds the Rust half of the key vocabulary from
// [keycodes.Table]. One table, two sides — SD4's point.
//
// It is not written by `egui2gen generate`, which emits only interpreter.rs and
// enums_out.rs; adding a third output would mean generator surgery for one
// small file. Instead the content is committed and a test
// (egui2_definition_d_keys_test.go) rebuilds it from the table and fails on any
// difference. That is a stronger guarantee than regeneration for this case: it
// is checked on every `go test`, where a drifted file would otherwise survive
// until someone happened to regenerate.
func KeyCodesRustFile() string {
	var b strings.Builder
	b.WriteString("// Key vocabulary for ADR-0177 focus-scoped capture.\n")
	b.WriteString("//\n")
	b.WriteString("// GENERATED from public/thestack/imzero2/egui2/keycodes/keycodes.go's Table.\n")
	b.WriteString("// Do not edit by hand: definition/egui2_definition_d_keys_test.go rebuilds this\n")
	b.WriteString("// from the Go table and fails if the two disagree, which is what stops the wire\n")
	b.WriteString("// code, the Go constant and the egui variant drifting apart.\n\n")
	b.WriteString("/// Map an egui key to its imzero2 wire code. 0 is \"outside the vocabulary\";\n")
	b.WriteString("/// the capture mask cannot name it, so it is never captured.\n")
	b.WriteString("pub fn imzero_key_code(k: egui::Key) -> u8 {\n    match k {\n")
	for _, e := range keycodes.Table {
		b.WriteString("        egui::Key::" + e.EguiKey + " => " +
			strconv.Itoa(int(e.Code)) + ", // " + e.Name + "\n")
	}
	// Unknown is the reserved zero. A key outside the vocabulary is never
	// captured (the mask cannot name it), so this arm is unreachable in
	// practice and exists to keep the match total.
	b.WriteString("        _ => 0,\n    }\n}\n")
	return b.String()
}

// keyCaptureHelperRust is the shared apply-time capture, called by every node
// that offers `.CaptureKeys()`. It is a string rather than a Rust `fn` because
// it needs the node's own `resp` and `{{Id}}`, and because the interpreter's
// generated region is assembled from these strings.
//
// Consuming is the point (SD2): `consume_key` removes the event from egui's
// queue, so an enclosing ScrollArea does not also scroll on ↑/↓/PageUp/PageDown.
// The mask matches on the KEY ALONE and reports the modifiers alongside (SD5),
// which sidesteps `consume_key`'s `matches_logically` extra-modifier ordering
// hazard rather than re-encountering it per adopter.
// maskBitExpr builds the Rust test for "the mask names any of these codes",
// with the bit numbers taken from the Go constants rather than written out. The
// numbers are a wire contract (SD4); spelling them by hand here would be a
// second place for them to drift from.
func maskBitExpr(codes ...keycodes.Code) string {
	parts := make([]string, 0, len(codes))
	for _, c := range codes {
		parts = append(parts, "(1u64 << "+strconv.Itoa(int(c))+")")
	}
	return "(capture_keys_mask & (" + strings.Join(parts, " | ") + ")) != 0"
}

func keyCaptureHelperRust(idExpr string, respExpr string) string {
	return `
if capture_keys_mask != 0 && ` + respExpr + `.has_focus() {
    // Consuming the event is NOT enough on its own, and this is the part that
    // is easy to get wrong: egui latches its focus-navigation direction in
    // Focus::begin_pass, from the RAW input, before any widget runs. By the
    // time this apply code removes an arrow key from the queue, egui has
    // already decided to move focus at end_pass, so the widget would capture
    // one keypress and immediately lose focus — capturing exactly once and
    // then appearing dead.
    //
    // The filter is egui's own mechanism for this, and it takes the same
    // declaration SD3 already makes: a mask naming the vertical arrows means
    // "they act on me", which is precisely what vertical_arrows encodes. So the
    // filter is DERIVED from the mask rather than being a second thing to keep
    // in sync — a widget that captures ↑/↓ keeps focus on them, and one that
    // does not still Tabs and arrows away normally.
    //
    // set_focus_lock_filter requires focus to have been held since last frame,
    // so the filter takes effect on the second frame of focus. The first
    // keypress in the same frame focus arrives can still navigate; egui's own
    // TextEdit has the same edge and it has not been worth more machinery.
    ui.memory_mut(|m| m.set_focus_lock_filter(` + respExpr + `.id, egui::EventFilter {
        tab: ` + maskBitExpr(keycodes.Tab) + `,
        horizontal_arrows: ` + maskBitExpr(keycodes.ArrowLeft, keycodes.ArrowRight) + `,
        vertical_arrows: ` + maskBitExpr(keycodes.ArrowUp, keycodes.ArrowDown) + `,
        escape: ` + maskBitExpr(keycodes.Escape) + `,
    }));
    let mods_now = ui.input(|inp| inp.modifiers);
    let mods_byte = (mods_now.shift as u8)
        | ((mods_now.ctrl as u8) << 1)
        | ((mods_now.alt as u8) << 2)
        | ((mods_now.command as u8) << 3);
    // Collect first, mutate after: consuming inside the read closure would
    // borrow the input state twice.
    let mut hits: Vec<(egui::Key, u8)> = Vec::new();
    ui.input(|inp| {
        for ev in &inp.events {
            if let egui::Event::Key { key, pressed: true, .. } = ev {
                let code = crate::imzero2::keycodes::imzero_key_code(*key);
                if code != 0 && (capture_keys_mask & (1u64 << code)) != 0 {
                    hits.push((*key, code));
                }
            }
        }
    });
    for (key, code) in hits {
        // Remove it from the queue so nothing downstream also acts on it.
        ui.input_mut(|inp| {
            inp.events.retain(|ev| !matches!(ev,
                egui::Event::Key { key: k, pressed: true, .. } if *k == key));
        });
        self.r26_key_capture_push(` + idExpr + `, code, mods_byte);
    }
}
`
}

// definitionsKeysFetchers drains R26 (SD6). Separate from definitionsFetcher()
// so ADR-0177's pieces sit together; the generator concatenates several such
// per-feature fetcher sets already.
func definitionsKeysFetchers() (nodes []ir.NodeI) {
	// Drains every key captured last frame, as parallel arrays: one row per
	// EVENT, not per widget, so a widget appears as many times as it captured.
	// Go re-groups by id in StateManager.Sync.
	//
	// Drain (`.drain(..)`) rather than copy: the register is per-frame and the
	// clear in prepare_next_frame is a safety net, not the normal path.
	nodes = append(nodes, idl.NewFetcherNode("fetchR26KeyCaptures").
		WithApplyCodeClientRust(rustClientCode(`
let len = self.r26_key_capture_ids.len();
debug_assert_eq!(len, self.r26_key_capture_codes.len());
debug_assert_eq!(len, self.r26_key_capture_mods.len());
self.io.write_plain_u64h(len, self.r26_key_capture_ids.drain(..))?;
self.io.write_plain_u8h(len, self.r26_key_capture_codes.drain(..))?;
self.io.write_plain_u8h(len, self.r26_key_capture_mods.drain(..))?;
{{SendMessage}}
`)).
		AddReturnValue("ids", ctabb.U64h).
		AddReturnValue("codes", ctabb.U8h).
		AddReturnValue("mods", ctabb.U8h).
		Build())
	return
}

func definitionsKeys() (nodes []*ir.ProceduralNode) {
	nodes = make([]*ir.ProceduralNode, 0, 2)

	// requestFocus — ADR-0177 SD7. Moves egui's keyboard focus to a widget that
	// registered itself focusable via `.Focusable()`. On any other id this
	// silently does nothing, which is the trap SD7 exists to name: the id must
	// be one egui knows, and the only imzero2 ids egui knows are the ones
	// `.Focusable()`'s interact rect registered.
	//
	// One-shot: call it on the frame you want focus to move, not every frame.
	// Re-requesting every frame would pin focus and make the widget impossible
	// to Tab out of.
	//
	// Goes through {{EguiContext}}, NOT the outer Ui. Focus lives in Memory,
	// which hangs off the Context, so a Ui is not needed — and reaching it as
	// `ui.ctx()` means gating the body on `u.is_some()`, which turns every
	// dispatch where the interpreter holds no Ui into a SILENT no-op. That is
	// the same class of failure as SD7's id mismatch: nothing logs, focus
	// simply never moves. The Context parameter is never optional, so this form
	// cannot be skipped.
	nodes = append(nodes, idl.NewProceduralNode("requestFocus").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("id", ctabb.U64).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					{{EguiContext}}.memory_mut(|m| m.request_focus(`+focusIdExpr("id")+`));
`)).
		Build())

	// surrenderFocus — the other half, and not in the ADR's M0 list. It is here
	// because without it a widget that takes focus on click has no way to give
	// it back on Escape, and the alternative a caller would reach for (request
	// focus on some other id) needs an id it may not have.
	nodes = append(nodes, idl.NewProceduralNode("surrenderFocus").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("id", ctabb.U64).Build()).
		WithApplyCodeClientRust(rustClientCode(`
					{{EguiContext}}.memory_mut(|m| m.surrender_focus(`+focusIdExpr("id")+`));
`)).
		Build())
	return
}
