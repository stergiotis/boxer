package widgets

import (
	"encoding/hex"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/cbordiag"
)

// =============================================================================
// cbordiag widget demo — CBOR bytes as RFC 8949 §8 diagnostic notation
// (ADR-0219 §SD6)
//
// Three fixtures through one Renderer whose options are wired to live
// controls: a leeway-canonical-wire-shaped entity (tag comments and a
// path annotation hook label its parts), the RFC 8949 Appendix A nesting
// examples as a CBOR sequence, and a malformed item that shows the
// degradation — the parsed prefix, the failure, the remainder as hex.
// =============================================================================

type cbordiagDemoState struct {
	tagComments    bool
	floatPrecision bool
	width          uint64
	// One State per view: the compact toggle and the memo belong to the
	// place a view is drawn, not to the Renderer.
	stEntity, stSequence, stMalformed cbordiag.State
}

// The fixtures, as the bytes a wire would carry. Decoded once at init.
var (
	// [1, {0: [255, 1000]}, {"f32": [[[[0, 7]], 1.5]], "sx-u64": [[[[1, h'6162']], "abc"]],
	//  "time": 1001({1: 1363896240, -9: 40}), "set": 258([1, 2])}] — the
	// shapes the leeway canonical forms write.
	cbordiagEntity = mustHex("8301a1008218ff1903e8a463663332818281820007f93e006673782d7536348182818201426162636162636474696d65d903e9a2011a514b67b028182863736574d90102820102")
	// RFC 8949 Appendix A: [_ 1, [2, 3], [_ 4, 5]], {_ "a": 1, "b": [_ 2, 3]},
	// (_ h'0102', h'030405'), 1(1363896240.5)
	cbordiagSequence = mustHex("9f018202039f0405ffffbf61610161629f0203ffff5f42010243030405ffc1fb41d452d9ec200000")
	// [1, 2, <reserved additional information>]
	cbordiagMalformed = mustHex("8301021c")
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// cbordiagAnnotate labels the entity fixture the way a leeway-aware host
// would: the three positions of the entity item and each tagged slot.
func cbordiagAnnotate(path []diag.PathElem) string {
	switch len(path) {
	case 0:
		return "entity"
	case 1:
		switch path[0].Index {
		case 0:
			return "version"
		case 1:
			return "plains"
		case 2:
			return "tagged"
		}
	case 2:
		if path[0].Index == 2 && path[1].Kind == diag.PathElemKey {
			return "slot"
		}
	}
	return ""
}

func init() {
	registry.Register(registry.Demo{
		Name:        "cbordiag",
		Category:    "Text & code",
		Title:       icons.IconBracketsCurly + " CBOR diagnostic notation",
		Stage:       [2]float32{1024, 700},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "widgets/cbordiag over the cbor/diag printer: CBOR bytes as RFC 8949 §8 diagnostic notation, pretty-printed one element per line past the line width, with known tags labelled in comments and a host hook annotating positions. Compact mode reproduces the fxamacker library's single-line notation byte for byte. Malformed input degrades: the parsed prefix, one error span, the remainder as hex.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &cbordiagDemoState{tagComments: true, width: 48}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoCborDiag(ids, state.(*cbordiagDemoState))
		},
		SourceFunc: demoCborDiag,
	})
}

func demoCborDiag(ids *c.WidgetIdStack, st *cbordiagDemoState) {
	for range c.HorizontalTop().KeepIter() {
		c.Checkbox(ids.PrepareStr("tag-comments"), st.tagComments, "tag comments").SendRespVal(&st.tagComments)
		c.Checkbox(ids.PrepareStr("float-precision"), st.floatPrecision, "float precision suffix").SendRespVal(&st.floatPrecision)
		c.Label("width").Send()
		c.DragValueU64(ids.PrepareStr("width"), st.width).SendRespVal(&st.width)
		st.width = min(max(st.width, 16), 160)
	}
	opts := diag.Options{
		Width:          int(st.width),
		TagComments:    st.tagComments,
		FloatPrecision: st.floatPrecision,
	}
	for range c.CollapsingHeader(ids.PrepareStr("entity"), c.WidgetText().Text("leeway-shaped entity, annotated").Keep()).DefaultOpen(true).KeepIter() {
		annotated := opts
		annotated.Annotate = cbordiagAnnotate
		st.stEntity.Verdict = "annotated by the host's path hook"
		cbordiag.New(ids, "cd-entity").Render(&st.stEntity, cbordiagEntity, annotated)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sequence"), c.WidgetText().Text("RFC 8949 Appendix A nesting, as a sequence").Keep()).DefaultOpen(true).KeepIter() {
		seq := opts
		seq.Sequence = true
		cbordiag.New(ids, "cd-seq").Render(&st.stSequence, cbordiagSequence, seq)
	}
	for range c.CollapsingHeader(ids.PrepareStr("malformed"), c.WidgetText().Text("malformed (graceful degradation)").Keep()).KeepIter() {
		cbordiag.New(ids, "cd-bad").Render(&st.stMalformed, cbordiagMalformed, opts)
	}
}
