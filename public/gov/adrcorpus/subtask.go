package adrcorpus

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// doneGlyph is the marker an ADR writes to declare one of its own sub-items
// done. It is the glyph the corpus already reaches for when hand-tracking
// progress (the ADR-0042 codec inventory tables, the ADR-0012 option matrix).
const doneGlyph = "✓"

// Subtask is one row of the `subtask` table: a sub-item an ADR declares for
// itself — a subsidiary design decision (SD), a milestone (M), a phase, a step
// — plus whether the ADR declares it done.
//
// Done is *declared*, not inferred from code evidence like the rest of this
// package's implementation axis. That split is deliberate (ADR-0092 §Update
// 2026-07-15): the corpus decomposes overwhelmingly into SDs, and an SD is a
// design decision rather than a unit of work. Some are buildable, but many —
// an IP boundary, a performance posture, a naming rule — can never have a line
// of code to cite. Evidence cannot speak for those; the author can.
type Subtask struct {
	Num     int    // owning ADR number
	Marker  string // canonical marker, e.g. "SD3", "M12", "Phase 2"
	Kind    string // "SD" | "M" | "Phase" | "Step" | "Cut" | "Milestone"
	Ordinal int    // leading integer of the marker (3 in SD3, 0 in M0a)
	Title   string // declaration title, with the done glyph stripped
	Done    bool   // the ADR declares this sub-item done
	Shape   string // "heading" | "list" — which declaration form was used
	Line    int    // 1-based line of the declaration within the ADR file
	// CodeRefs counts source citations pinning this exact marker
	// ("ADR-0094 §SD4"), filled by [Aggregate]. It is *evidence*, not a
	// verdict: it says code names this sub-item, not that the sub-item is
	// finished — Done alone says that. The two are worth reading together,
	// since cited-but-not-declared is the drift case at this granularity, and
	// declared-but-uncited is normal for a decision with nothing to build.
	CodeRefs int
}

// markerPat matches a sub-item marker: a kind word, an optional space, and an
// ordinal that may carry a sub-ordinal (M0a, M3.5). Milestone precedes M so the
// longer word wins Go's leftmost-first alternation.
const markerPat = `(SD|Milestone|Phase|Step|Cut|M)[ ]?(\d+(?:\.\d+)?[a-z]?)`

// dashPat is the em-dash that separates a marker from its title. It is
// deliberately *only* the em-dash: the corpus writes all 677 of its
// declarations with one, and reserves the en-dash for numeric ranges, so
// accepting "–" too would read "- **Phase 0–1** — …" (phases 0 through 1) as a
// declaration of "Phase 0" titled "1".
const dashPat = `[ \t]*—[ \t]*`

var (
	// Heading shape, the form used for standalone sections:
	//
	//	### SD3 — Subject taxonomy ✓
	//
	// The marker is anchored immediately after the hashes, so a dated Update
	// heading that merely *mentions* a marker ("### 2026-05-23 — M3 landed")
	// is not a declaration and never matches.
	subHeadingRe = regexp.MustCompile(`^#{2,4}[ \t]+` + markerPat + dashPat + `(.+?)[ \t]*$`)
	// List shape, the form used under a Decision or "Subsidiary design
	// decisions" section, where prose usually continues on the same line:
	//
	//	- **SD1 — Provider registry + interface.** ✓ A `TableProvider` declares…
	//
	// The title runs to the closing `**`; the glyph, when present, sits just
	// past it.
	subListRe = regexp.MustCompile(`^[ \t]*[-*][ \t]+\*\*` + markerPat + dashPat + `(.+?)\*\*[ \t]*(` + doneGlyph + `)?`)
	// fenceRe matches a fenced code-block delimiter: three or more backticks
	// or tildes, indented at most three spaces (CommonMark). Group 2 is the
	// info string, which only an opening fence may carry.
	fenceRe = regexp.MustCompile("^[ ]{0,3}(`{3,}|~{3,})(.*)$")
)

// extractSubtasks harvests the sub-items an ADR declares for itself, in either
// shape, from body (whose first line is lineOffset+1 in the file).
//
// A declaration is a marker followed by an em-dash and a title. The dash is
// what separates a declaration from prose that merely names a marker — "- **M1
// is unblocked.**" and "- **M3 (per-pool LRU cache) is unblocked.**" are status
// remarks, not declarations, and the corpus writes every real declaration with
// a dash. A handful of oddballs are missed by that rule (a dual "SD4 + SD7 —"
// heading, a few parenthesised "Phase 5 (landed)" bullets); adding the dash to
// them is how an author brings them onto the board.
//
// A marker declared twice in one ADR — an original plus a later Update that
// re-decides it — folds into a single sub-item: the first declaration supplies
// the title, and Done is the OR across all of them, so a later Update marking
// it done wins.
func extractSubtasks(num int, body string, lineOffset int) (subs []Subtask) {
	byMarker := make(map[string]int) // marker -> index into subs
	upsert := func(s Subtask) {
		if i, dup := byMarker[s.Marker]; dup {
			subs[i].Done = subs[i].Done || s.Done
			return
		}
		byMarker[s.Marker] = len(subs)
		subs = append(subs, s)
	}
	lineNo := lineOffset
	// fence holds the open code fence's delimiter run, "" outside a block.
	// Fenced code is skipped: an ADR that *documents* this convention shows a
	// declaration in an example, and a line-oriented scan would otherwise
	// harvest the example as a real sub-item of the ADR doing the explaining.
	var fence string
	for line := range strings.SplitSeq(body, "\n") {
		lineNo++
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			run := m[1]
			switch {
			case fence == "":
				fence = run
			case run[0] == fence[0] && len(run) >= len(fence) && strings.TrimSpace(m[2]) == "":
				// A closing fence matches the opener's character, is at least
				// as long, and carries no info string — so a longer outer
				// fence can quote a shorter one.
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if m := subHeadingRe.FindStringSubmatch(line); m != nil {
			title, done := splitDoneGlyph(m[3])
			upsert(newSubtask(num, m[1], m[2], title, done, "heading", lineNo))
			continue
		}
		if m := subListRe.FindStringSubmatch(line); m != nil {
			upsert(newSubtask(num, m[1], m[2], strings.TrimSpace(m[3]), m[4] != "", "list", lineNo))
		}
	}
	return subs
}

func newSubtask(num int, kind, ord, title string, done bool, shape string, line int) Subtask {
	// The ordinal's leading integer; a sub-ordinal (M0a, M3.5) sorts with its
	// parent rather than getting a number of its own.
	lead := ord
	if i := strings.IndexFunc(ord, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		lead = ord[:i]
	}
	n, _ := strconv.Atoi(lead)
	return Subtask{
		Num: num, Marker: kind + ord, Kind: kind, Ordinal: n,
		Title: title, Done: done, Shape: shape, Line: line,
	}
}

// splitDoneGlyph peels a trailing done glyph off a heading's title text.
func splitDoneGlyph(title string) (stripped string, done bool) {
	t := strings.TrimSpace(title)
	if !strings.HasSuffix(t, doneGlyph) {
		return t, false
	}
	return strings.TrimSpace(strings.TrimSuffix(t, doneGlyph)), true
}

// sortSubtasks orders sub-items for display: by owning ADR, then kind, then
// ordinal, then marker — so SD1 … SD10 read in declaration order rather than
// lexically (SD10 before SD2).
func sortSubtasks(subs []Subtask) {
	sort.SliceStable(subs, func(i, j int) bool {
		a, b := subs[i], subs[j]
		if a.Num != b.Num {
			return a.Num < b.Num
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return a.Marker < b.Marker
	})
}

// AllSubtasks flattens the per-ADR sub-items into the rows of the `subtask`
// table, ordered for display.
func AllSubtasks(adrs []Adr) (subs []Subtask) {
	for _, a := range adrs {
		subs = append(subs, a.Subtasks...)
	}
	sortSubtasks(subs)
	return subs
}
