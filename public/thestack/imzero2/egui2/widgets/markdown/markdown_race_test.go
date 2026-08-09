package markdown

import (
	"strings"
	"sync"
	"testing"
)

// raceSource is one document per goroutine-shape the concurrent test wants to
// exercise: shared content (so the intern table and the codeview memo are hit
// on the same keys from several goroutines at once), and distinct content (so
// they are hit on different keys). Both matter — a lock taken only on the miss
// path would pass the first and fail the second.
func raceSource(variant int) (src []byte) {
	var b strings.Builder
	b.WriteString("---\ntitle: doc\n---\n\n# Heading\n\n")
	b.WriteString("Prose with **bold**, `code`, a [link](https://example.com), #tag,\n")
	b.WriteString("a [[Wikilink]] and an ![[embed.png]].\n\n")
	b.WriteString("- bullet\n- [x] task\n\n1. one\n2. two\n\n")
	b.WriteString("> [!note] callout\n> body\n\n")
	b.WriteString("| a | b |\n|---|---|\n| 1 | 2 |\n\n")
	// The fenced blocks are the interesting part: every one of them goes
	// through codeview's package-level prepared-job memo, which is the only
	// shared mutable state on the parse path.
	b.WriteString("```go\npackage main\n\nfunc main() {}\n```\n\n")
	b.WriteString("```sql\nSELECT 1;\n```\n\n")
	b.WriteString("```json\n{\"a\": 1}\n```\n\n")
	b.WriteString("```markdown\n# nested\n```\n\n")
	if variant >= 0 {
		// Distinct tail ⇒ distinct memo keys and distinct interned content.
		b.WriteString("```go\n// variant ")
		b.WriteString(strings.Repeat("x", variant+1))
		b.WriteString("\nfunc v() {}\n```\n")
	}
	src = []byte(b.String())
	return
}

// TestParse_IsSafeOffTheRenderGoroutine pins the contract stated in the
// package doc: [Parse] may run on any goroutine, concurrently.
//
// This is worth a test rather than a sentence because the claim reads like it
// should be false — the lowering calls c.Atoms() and .Keep(), which look like
// FFI. They are not: those write into a sync.Pool-backed Go buffer and intern
// the bytes through unique.Make; only Render's Send() reaches the FFFI sink.
// The one piece of shared mutable state involved is codeview's package-level
// prepared-job memo, and it takes a mutex precisely for this.
//
// ADR-0178 asserted the opposite ("the parse cannot leave the render
// goroutine") and two shipping consumers had been contradicting it in
// production all along. Run under -race this is what makes the corrected
// claim checkable instead of asserted.
func TestParse_IsSafeOffTheRenderGoroutine(t *testing.T) {
	const goroutines = 8
	const iterations = 12

	// Shared input: every goroutine parses byte-identical content, so they
	// collide on the same intern entries and the same memo keys.
	shared := raceSource(-1)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(g int) {
			defer wg.Done()
			for i := range iterations {
				doc := Parse(shared)
				if len(doc.segments) == 0 {
					t.Errorf("goroutine %d iteration %d: shared doc lowered to nothing", g, i)
					return
				}
				if dropped := doc.Dropped(); len(dropped) != 0 {
					t.Errorf("goroutine %d iteration %d: dropped %+v", g, i, dropped)
					return
				}
				// Distinct input: fresh memo keys and fresh intern entries,
				// which is the path a shared-key-only test would miss.
				own := Parse(raceSource(g*iterations + i))
				if len(own.segments) != len(doc.segments)+1 {
					t.Errorf("goroutine %d iteration %d: variant doc has %d segments, want %d",
						g, i, len(own.segments), len(doc.segments)+1)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestParse_ConcurrentAndSequentialAgree is the correctness half: -race proves
// there is no data race, this proves the result does not depend on who else is
// parsing. A memo that handed back another key's entry under contention would
// be race-free and wrong.
func TestParse_ConcurrentAndSequentialAgree(t *testing.T) {
	const goroutines = 6
	srcs := make([][]byte, goroutines)
	wantSegments := make([]int, goroutines)
	wantHeadings := make([][]HeadingInfo, goroutines)
	for i := range goroutines {
		srcs[i] = raceSource(i)
		seq := Parse(srcs[i])
		wantSegments[i] = len(seq.segments)
		wantHeadings[i] = seq.Headings()
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			got := Parse(srcs[i])
			if len(got.segments) != wantSegments[i] {
				t.Errorf("doc %d: %d segments concurrently, %d sequentially",
					i, len(got.segments), wantSegments[i])
			}
			gotHeadings := got.Headings()
			if len(gotHeadings) != len(wantHeadings[i]) {
				t.Errorf("doc %d: %d headings concurrently, %d sequentially",
					i, len(gotHeadings), len(wantHeadings[i]))
				return
			}
			for h := range gotHeadings {
				if gotHeadings[h] != wantHeadings[i][h] {
					t.Errorf("doc %d heading %d: got %+v want %+v",
						i, h, gotHeadings[h], wantHeadings[i][h])
				}
			}
		}(i)
	}
	wg.Wait()
}
