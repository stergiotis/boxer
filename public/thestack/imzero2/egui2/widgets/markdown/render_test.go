package markdown

import (
	"testing"

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
