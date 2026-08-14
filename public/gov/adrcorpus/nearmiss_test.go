package adrcorpus

import "testing"

// TestFindNearMissesClosedEarly pins the shape that hid 45 declared sub-items
// across ten ADRs: the bold closing before the em-dash instead of after the
// title. It renders identically to a declaration and parses as nothing.
func TestFindNearMissesClosedEarly(t *testing.T) {
	body := "- **M0** — Cover lane script.\n" +
		"- **Phase 2** — single-ray overlay.\n"
	nm := FindNearMisses(body, 0)
	if len(nm) != 2 {
		t.Fatalf("want 2 near misses, got %d: %+v", len(nm), nm)
	}
	if nm[0].Marker != "M0" || nm[0].Line != 1 {
		t.Errorf("first: want M0 at line 1, got %q at %d", nm[0].Marker, nm[0].Line)
	}
	if nm[1].Marker != "Phase 2" {
		t.Errorf("second: want 'Phase 2', got %q", nm[1].Marker)
	}
}

// TestFindNearMissesUnterminated pins the second shape, which the corpus sweep
// for the first one missed entirely: a title wrapped onto a continuation line.
// Parsing is line-oriented, so the closing ** never lands on the marker's line.
// Eleven SDs across seven ADRs were invisible this way.
func TestFindNearMissesUnterminated(t *testing.T) {
	body := "- **SD2 — Read-side dispatch picks the matching accessor\n" +
		"  method.** The emitter and the dispatcher both consult it.\n"
	nm := FindNearMisses(body, 0)
	if len(nm) != 1 {
		t.Fatalf("want 1 near miss, got %d: %+v", len(nm), nm)
	}
	if nm[0].Marker != "SD2" {
		t.Errorf("want SD2, got %q", nm[0].Marker)
	}
}

// A well-formed declaration must not be reported. This is the property that
// keeps the rule usable: it fires only where the subtask table is actually
// missing a row.
func TestFindNearMissesIgnoresValidDeclarations(t *testing.T) {
	body := "- **SD1 — Provider registry.** ✓ A `TableProvider` declares…\n" +
		"### SD3 — Subject taxonomy ✓\n" +
		"- **M0 — Cover lane script + measured overhead.** Recorded here.\n"
	if nm := FindNearMisses(body, 0); len(nm) != 0 {
		t.Fatalf("valid declarations reported as near misses: %+v", nm)
	}
}

// The shapes extractSubtasks deliberately rejects as prose must not be
// reported either — otherwise the rule would push authors to turn status
// remarks into declarations, which is the opposite of what the em-dash
// discipline is for.
func TestFindNearMissesIgnoresProse(t *testing.T) {
	body := "- **M1 is unblocked.** No dash, so not a declaration.\n" +
		"- **M3 (per-pool LRU cache) is unblocked.**\n" +
		"- **Phase 0–1** — an en-dash range, not a declaration of Phase 0.\n" +
		"- **M2/M3** — a combined recap naming two markers.\n" +
		"Prose naming SD7 and M2 inline.\n"
	if nm := FindNearMisses(body, 0); len(nm) != 0 {
		t.Fatalf("prose reported as near misses: %+v", nm)
	}
}

// Fenced code is skipped, for the same reason extractSubtasks skips it: a doc
// that documents this convention shows the broken form in an example.
func TestFindNearMissesSkipsFencedCode(t *testing.T) {
	body := "```md\n- **M0** — the shape this rule flags.\n```\n"
	if nm := FindNearMisses(body, 0); len(nm) != 0 {
		t.Fatalf("fenced example reported: %+v", nm)
	}
}

func TestFindNearMissesReportsFileRelativeLines(t *testing.T) {
	nm := FindNearMisses("- **M0** — x.\n", 40)
	if len(nm) != 1 || nm[0].Line != 41 {
		t.Fatalf("want line 41 with offset 40, got %+v", nm)
	}
}
