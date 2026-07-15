package adrcorpus

import "testing"

func findSub(subs []Subtask, marker string) (Subtask, bool) {
	for _, s := range subs {
		if s.Marker == marker {
			return s, true
		}
	}
	return Subtask{}, false
}

// TestExtractSubtasksShapes covers both declaration forms the corpus uses, the
// done glyph in each, and the line-offset accounting.
func TestExtractSubtasksShapes(t *testing.T) {
	body := "\n" + // line 1 of the body, i.e. line 6 of the file
		"## Decision\n" +
		"\n" +
		"### SD1 — Heading shape, done ✓\n" +
		"\n" +
		"### SD2 — Heading shape, not done\n" +
		"\n" +
		"- **SD3 — List shape, done.** ✓ Prose continues on the same line and\n" +
		"  wraps onto the next.\n" +
		"- **SD4 — List shape, not done.** Prose with no glyph.\n" +
		"- **M10 — A milestone.** ✓\n" +
		"#### Step 2 — A step.\n"
	subs := extractSubtasks(7, body, 5)

	if len(subs) != 6 {
		t.Fatalf("want 6 sub-items, got %d: %+v", len(subs), subs)
	}
	for _, tc := range []struct {
		marker string
		kind   string
		ord    int
		title  string
		done   bool
		shape  string
		line   int
	}{
		{"SD1", "SD", 1, "Heading shape, done", true, "heading", 9},
		{"SD2", "SD", 2, "Heading shape, not done", false, "heading", 11},
		{"SD3", "SD", 3, "List shape, done.", true, "list", 13},
		{"SD4", "SD", 4, "List shape, not done.", false, "list", 15},
		{"M10", "M", 10, "A milestone.", true, "list", 16},
		{"Step2", "Step", 2, "A step.", false, "heading", 17},
	} {
		s, ok := findSub(subs, tc.marker)
		if !ok {
			t.Errorf("%s: not extracted", tc.marker)
			continue
		}
		if s.Kind != tc.kind || s.Ordinal != tc.ord {
			t.Errorf("%s: want kind=%s ord=%d, got kind=%s ord=%d", tc.marker, tc.kind, tc.ord, s.Kind, s.Ordinal)
		}
		if s.Title != tc.title {
			t.Errorf("%s title: want %q, got %q", tc.marker, tc.title, s.Title)
		}
		if s.Done != tc.done {
			t.Errorf("%s done: want %v, got %v", tc.marker, tc.done, s.Done)
		}
		if s.Shape != tc.shape {
			t.Errorf("%s shape: want %s, got %s", tc.marker, tc.shape, s.Shape)
		}
		if s.Line != tc.line {
			t.Errorf("%s line: want %d (file-absolute), got %d", tc.marker, tc.line, s.Line)
		}
		if s.Num != 7 {
			t.Errorf("%s num: want 7, got %d", tc.marker, s.Num)
		}
	}
}

// TestExtractSubtasksRejectsProse pins the em-dash rule: a marker named in
// prose, in a dated Update heading, or inside a numeric range is not a
// declaration. Each of these is a real shape from the corpus.
func TestExtractSubtasksRejectsProse(t *testing.T) {
	body := "### 2026-05-23 — M3 landed; M3a deferred\n" +
		"- **M1 is unblocked.** No dash, so not a declaration.\n" +
		"- **M3 (per-pool LRU cache) is unblocked.**\n" +
		"- **Phase 0–1** — an en-dash range, not a declaration of Phase 0.\n" +
		"Prose naming SD7 and M2 inline.\n" +
		"| Bounded memory | ✗ | ✓ | ✓ |\n" +
		"- **SD9 — The one real declaration.**\n"
	subs := extractSubtasks(1, body, 0)

	if len(subs) != 1 {
		t.Fatalf("want exactly 1 sub-item (only SD9 is a declaration), got %d: %+v", len(subs), subs)
	}
	if subs[0].Marker != "SD9" || subs[0].Done {
		t.Errorf("want SD9 not-done, got %q done=%v", subs[0].Marker, subs[0].Done)
	}
}

// TestAggregateSubtaskEvidence covers the fold that drives the board's
// cited-but-undeclared dots: a citation's §qualifier matches a sub-item's
// marker exactly, and only that sub-item.
func TestAggregateSubtaskEvidence(t *testing.T) {
	adrs := []Adr{{
		Num: 94,
		Subtasks: []Subtask{
			{Num: 94, Marker: "SD1"}, {Num: 94, Marker: "SD2"}, {Num: 94, Marker: "SD10"},
		},
	}}
	refs := []CodeRef{
		{Num: 94, Qualifier: "SD1"}, {Num: 94, Qualifier: "SD1"},
		{Num: 94, Qualifier: "SD10"},
		{Num: 94, Qualifier: ""},    // unqualified: names the ADR, not a sub-item
		{Num: 93, Qualifier: "SD2"}, // another ADR's SD2 must not leak across
	}
	adrs = Aggregate(adrs, refs)

	want := map[string]int{"SD1": 2, "SD2": 0, "SD10": 1}
	for _, s := range adrs[0].Subtasks {
		if got := s.CodeRefs; got != want[s.Marker] {
			t.Errorf("%s: want %d citations, got %d", s.Marker, want[s.Marker], got)
		}
	}
	if adrs[0].SubtasksCited != 2 {
		t.Errorf("SubtasksCited: want 2 (SD1, SD10), got %d", adrs[0].SubtasksCited)
	}
}

// TestAggregateClearsStaleEvidence checks a re-Aggregate with no references
// zeroes the counts rather than leaving the previous fold's behind — the app
// re-folds on every Reload, and a failed scan must not look like evidence.
func TestAggregateClearsStaleEvidence(t *testing.T) {
	adrs := []Adr{{Num: 1, Subtasks: []Subtask{{Num: 1, Marker: "SD1"}}}}
	adrs = Aggregate(adrs, []CodeRef{{Num: 1, Qualifier: "SD1"}})
	if adrs[0].Subtasks[0].CodeRefs != 1 {
		t.Fatalf("setup: want 1 citation, got %d", adrs[0].Subtasks[0].CodeRefs)
	}
	adrs = Aggregate(adrs, nil)
	if got := adrs[0].Subtasks[0].CodeRefs; got != 0 {
		t.Errorf("stale evidence survived a re-aggregate: want 0, got %d", got)
	}
	if adrs[0].SubtasksCited != 0 {
		t.Errorf("stale SubtasksCited survived: want 0, got %d", adrs[0].SubtasksCited)
	}
}

// TestExtractSubtasksSkipsFencedCode is a regression test for the ADR that
// documents this convention: its worked example sits in a fenced block and was
// harvested as two real, done sub-items of the explaining ADR itself.
func TestExtractSubtasksSkipsFencedCode(t *testing.T) {
	body := "Mark one done with a ✓ after its title:\n" +
		"\n" +
		"```markdown\n" +
		"### SD3 — Subject taxonomy ✓\n" +
		"\n" +
		"- **SD1 — Provider registry + interface.** ✓ A `TableProvider` declares…\n" +
		"```\n" +
		"\n" +
		"- **SD7 — The one real declaration.**\n"
	subs := extractSubtasks(92, body, 0)

	if len(subs) != 1 {
		t.Fatalf("want 1 sub-item (the fenced example is not a declaration), got %d: %+v", len(subs), subs)
	}
	if subs[0].Marker != "SD7" {
		t.Errorf("want SD7, got %q", subs[0].Marker)
	}
}

// TestExtractSubtasksNestedFence checks a longer fence can quote a shorter one,
// and that an info string never closes a block.
func TestExtractSubtasksNestedFence(t *testing.T) {
	body := "````markdown\n" +
		"```go\n" +
		"### SD1 — Inside a quoted fence\n" +
		"```\n" +
		"### SD2 — Still inside the outer fence\n" +
		"````\n" +
		"### SD3 — Outside again\n"
	subs := extractSubtasks(1, body, 0)

	if len(subs) != 1 {
		t.Fatalf("want only SD3, got %d: %+v", len(subs), subs)
	}
	if subs[0].Marker != "SD3" {
		t.Errorf("want SD3, got %q", subs[0].Marker)
	}
}

// TestExtractSubtasksDedup covers a marker declared twice — an original plus a
// later Update that re-decides it and marks it done. The later done wins; the
// first declaration keeps title and line.
func TestExtractSubtasksDedup(t *testing.T) {
	body := "### SD2 — Original decision\n" +
		"\n" +
		"## Updates\n" +
		"\n" +
		"### SD2 — Pivot: a later re-decision ✓\n"
	subs := extractSubtasks(34, body, 0)

	if len(subs) != 1 {
		t.Fatalf("want 1 folded sub-item, got %d: %+v", len(subs), subs)
	}
	s := subs[0]
	if !s.Done {
		t.Errorf("want done=true (OR across declarations, so a later Update can mark it), got false")
	}
	if s.Title != "Original decision" || s.Line != 1 {
		t.Errorf("want first declaration's title/line, got %q line=%d", s.Title, s.Line)
	}
}

// TestSubtaskSubOrdinal covers M0a / M3.5: the sub-ordinal is kept in the
// marker but sorts with its parent's integer.
func TestSubtaskSubOrdinal(t *testing.T) {
	subs := extractSubtasks(1, "- **M0a — A sub-ordinal.**\n- **M3.5 — A dotted one.**\n", 0)
	if len(subs) != 2 {
		t.Fatalf("want 2, got %d: %+v", len(subs), subs)
	}
	for _, tc := range []struct {
		marker string
		ord    int
	}{{"M0a", 0}, {"M3.5", 3}} {
		s, ok := findSub(subs, tc.marker)
		if !ok {
			t.Errorf("%s: not extracted", tc.marker)
			continue
		}
		if s.Ordinal != tc.ord {
			t.Errorf("%s ordinal: want %d, got %d", tc.marker, tc.ord, s.Ordinal)
		}
	}
}

// TestParseDirSubtaskRollup checks the rollup that the board's dot tally reads.
func TestParseDirSubtaskRollup(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root+"/0001-with-subitems.md", `---
type: adr
status: accepted
date: 2026-05-01
---

# ADR-0001: With Sub-items

## Decision

- **SD1 — Done one.** ✓ Prose.
- **SD2 — Done two.** ✓
- **SD3 — Not done.**
`)
	writeFile(t, root+"/0002-none.md", `---
type: adr
status: proposed
date: 2026-05-02
---

# ADR-0002: No Sub-items

## Context

Nothing decomposed here.
`)
	adrs, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	a1, _ := byNum(adrs, 1)
	if a1.SubtasksTotal != 3 || a1.SubtasksDone != 2 {
		t.Errorf("ADR-0001 rollup: want 2/3, got %d/%d", a1.SubtasksDone, a1.SubtasksTotal)
	}
	a2, _ := byNum(adrs, 2)
	if a2.SubtasksTotal != 0 || a2.SubtasksDone != 0 {
		t.Errorf("ADR-0002 rollup: want 0/0, got %d/%d", a2.SubtasksDone, a2.SubtasksTotal)
	}
	if all := AllSubtasks(adrs); len(all) != 3 {
		t.Errorf("AllSubtasks: want 3 flattened rows, got %d", len(all))
	}
}

// TestQualifierNotADate pins the qualifier regex against the two shapes that
// used to be mis-captured as section pins.
func TestQualifierNotADate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root+"/code/x.go", "package code\n"+
		"// ADR-0026 §2026-05-12 follow-up: a dated Update, not a section.\n"+
		"// ADR-0032 §Q3 rationale, ADR-0071 §B1, ADR-0080 §SD3, ADR-0009 §4, ADR-0042 §M2.7\n")
	refs, err := ScanCodeRefs(root, "", "")
	if err != nil {
		t.Fatalf("ScanCodeRefs: %v", err)
	}
	want := map[int]string{26: "", 32: "Q3", 71: "B1", 80: "SD3", 9: "4", 42: "M2.7"}
	got := make(map[int]string, len(refs))
	for _, r := range refs {
		got[r.Num] = r.Qualifier
	}
	for num, w := range want {
		g, ok := got[num]
		if !ok {
			t.Errorf("ADR-%04d: no ref scanned", num)
			continue
		}
		if g != w {
			t.Errorf("ADR-%04d qualifier: want %q, got %q", num, w, g)
		}
	}
}
