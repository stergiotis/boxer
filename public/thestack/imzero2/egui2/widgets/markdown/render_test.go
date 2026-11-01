package markdown

import (
	"iter"
	"testing"

	fffi "github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

const renderSrc = "# Title\n\nprose with [a link](/in-doc)\n\n" +
	"> quoted\n\n```sql\nSELECT 1\n```\n\n```go\npackage x\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"

// TestRenderHeadless renders a document with every option set under the
// discard channel: the widget must not panic, and a quiet frame reports no
// actions and no links.
func TestRenderHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	doc := Parse([]byte("---\nk: v\n---\n" + renderSrc))
	res := Render(Input{
		Ids: ids, ScopeKey: "t", Doc: doc,
		ScrollToSection:  "title",
		ActionLabels:     []string{"Copy", "Insert"},
		CodeActionFilter: func(_, lang string) bool { return lang == "sql" },
		LinkClaims:       func(url string) bool { return url == "/in-doc" },
		Frontmatter:      true,
	})
	if len(res.Actions) != 0 || len(res.Links) != 0 {
		t.Fatalf("quiet frame reported %+v", res)
	}
	filtered := Render(Input{Ids: ids, ScopeKey: "t-filtered", Doc: doc, SectionFilter: func(string) bool { return false }})
	if len(filtered.Actions) != 0 {
		t.Fatalf("filtered frame reported %+v", filtered)
	}
	if r := Render(Input{Doc: doc}); len(r.Actions) != 0 {
		t.Fatal("a nil Ids must draw nothing")
	}
	RenderFrontmatter(doc)
	RenderFrontmatter(nil)

	// The block-granular targets (ADR-0347 §SD4): an offset, and a block id
	// through the section slot. The harness records no ops, so this only
	// shows both paths render without fault and leave the result quiet;
	// the arming itself is TestArmBlockScroll.
	ided := Parse([]byte("first ^one\n\nsecond\n"))
	if r := Render(Input{Ids: ids, ScopeKey: "t-off", Doc: ided, ScrollToOffset: 12, ScrollToOffsetSet: true}); len(r.Links) != 0 {
		t.Fatalf("offset frame reported %+v", r)
	}
	if r := Render(Input{Ids: ids, ScopeKey: "t-id", Doc: ided, ScrollToSection: "^one"}); len(r.Links) != 0 {
		t.Fatalf("block-id frame reported %+v", r)
	}
	RenderBlock(Input{Ids: ids, ScopeKey: "t-blk", Doc: ided, ScrollToSection: "^one"}, 0)
	RenderBlock(Input{Ids: ids, ScopeKey: "t-blk2", Doc: ided, ScrollToOffset: 0, ScrollToOffsetSet: true}, 0)
}

// codeKeys lists the code blocks' id keys in document order.
func codeKeys(doc *Doc) (keys []uint64) {
	var walk func(segs []segment)
	walk = func(segs []segment) {
		for i := range segs {
			if segs[i].kind == segKindCodeBlock {
				keys = append(keys, segs[i].codeKey)
			}
			walk(segs[i].children)
		}
	}
	walk(doc.segments)
	return
}

// TestCodeKeysSurviveAnInsertionAbove is the property the keys exist for:
// a block inserted above leaves the ids of the blocks below it alone, which
// the document-order sequence they replaced did not (survey idiom I8).
func TestCodeKeysSurviveAnInsertionAbove(t *testing.T) {
	before := codeKeys(Parse([]byte(renderSrc)))
	after := codeKeys(Parse([]byte("```sh\necho new\n```\n\n" + renderSrc)))
	if len(before) != 2 || len(after) != 3 {
		t.Fatalf("block counts %d, %d", len(before), len(after))
	}
	if before[0] != after[1] || before[1] != after[2] {
		t.Fatalf("keys moved: before %x, after %x", before, after)
	}
	dup := codeKeys(Parse([]byte("```sql\nSELECT 1\n```\n\n```sql\nSELECT 1\n```\n")))
	if dup[0] == dup[1] || dup[0] == 0 || dup[1] == 0 {
		t.Fatalf("identical blocks must get distinct non-zero keys: %x", dup)
	}
}

// byteChannel discards every paint command and counts its bytes.
type byteChannel struct{ n *int }

func (inst byteChannel) SyncMultiUseMsg(_ uint64, b []byte) { *inst.n += len(b) }
func (inst byteChannel) SendSingleUseMsg(b []byte)          { *inst.n += len(b) }
func (byteChannel) FlushMessages()                          {}
func (byteChannel) ReceiveMsg() iter.Seq[*fffi.Unmarshaller] {
	return func(func(*fffi.Unmarshaller) bool) {}
}

// TestRenderSendsImagePixelsOnce pins the send-once contract: an image
// widget's first frame carries its pixels, later frames do not, a second
// id scope gets its own first send, and a second Doc gets its own content
// version so it never draws the first one's textures.
func TestRenderSendsImagePixelsOnce(t *testing.T) {
	n := 0
	typed.SetCurrentFffiVar(fffi.NewFffi2[*fffi.Unmarshaller](byteChannel{n: &n}))
	sm := c.CurrentApplicationState.StateManager
	sm.ScriptReset()
	t.Cleanup(sm.ScriptReset)
	const w, h = 256, 256
	const pixelBytes = w * h * 4
	parse := func() *Doc {
		r := &stubResolver{imagePayload: make([]uint32, w*h), imageW: w, imageH: h}
		return ParseWith([]byte("![[fig.png]]\n"), ParseOptions{Resolver: r})
	}
	ids := c.NewWidgetIdStack()
	frame := func(doc *Doc, scope string) int {
		n = 0
		Render(Input{Ids: ids, ScopeKey: scope, Doc: doc})
		sm.ScriptReset()
		return n
	}
	doc := parse()
	if got := frame(doc, "a"); got < pixelBytes {
		t.Fatalf("first frame sent %d bytes, want the %d pixel bytes", got, pixelBytes)
	}
	if got := frame(doc, "a"); got >= pixelBytes/16 {
		t.Fatalf("second frame sent %d bytes, want no pixels", got)
	}
	if got := frame(doc, "b"); got < pixelBytes {
		t.Fatalf("first frame in a second scope sent %d bytes, want the pixels", got)
	}
	other := parse()
	if other.imageVersion == doc.imageVersion {
		t.Fatalf("two Docs share image version %d", doc.imageVersion)
	}
	if got := frame(other, "a"); got < pixelBytes {
		t.Fatalf("a second Doc under the first one's scope sent %d bytes, want its pixels", got)
	}
}
