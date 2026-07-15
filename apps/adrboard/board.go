package adrboard

import (
	"fmt"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/kanban"
)

// statusOrder is the decision lifecycle, left to right: an ADR is proposed,
// then accepted, and eventually leaves play by being superseded, withdrawn or
// deferred. A status the corpus grows that is not listed here gets its own
// column appended after these, so a new vocabulary word shows up on the board
// rather than vanishing from it.
var statusOrder = []string{"proposed", "accepted", "superseded", "withdrawn", "deferred"}

// Dot kinds, in the order they read along a card: settled, then evidenced, then
// nothing known. Three, not two, because "not declared done" hides a real
// distinction the corpus can answer — a sub-item that source code names by its
// §marker is visibly being built even though nobody has marked it, and one that
// nothing cites is either unbuilt or unbuildable. Collapsing those two would
// make the board read 0/674 while a sixth of the corpus is demonstrably
// realized, which is no signal at all.
//
// Only Done is a claim about completion, and only an author can make it. Cited
// is evidence and stays visibly weaker: it marks the sub-items worth reviewing
// for a ✓, not ones that have earned it.
const (
	dotDone uint64 = iota + 1
	dotCited
	dotUnknown
)

// noStatusTitle is the column an ADR with an empty frontmatter status lands in.
// It is only added when something needs it.
const noStatusTitle = "(no status)"

func tokenColor(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

// dotLegend names the three dot kinds, in card order.
//
// NeutralSubtle would be the tempting name for the muted one and is the wrong
// token: it is a *background* tone (L=0.22, RGB 27/27/27) and lands within
// three points of the card's own NeutralBgSurface fill (29/32/33), rendering
// the dots invisible. Dots are foreground marks and need a text tone.
func dotLegend() []kanban.DotKind {
	return []kanban.DotKind{
		{
			ID: dotDone, Color: tokenColor(styletokens.SuccessDefault), Label: "Declared done",
			Tooltip: "The ADR marks this sub-item done with a ✓ on its declaration — the only claim of completion",
		},
		{
			ID: dotCited, Color: tokenColor(styletokens.WarningDefault), Label: "Cited in code, undeclared",
			Tooltip: "Source cites this sub-item by its §marker but no ✓ declares it done — worth a look, not proof",
		},
		{
			ID: dotUnknown, Color: tokenColor(styletokens.NeutralTextDisabled), Label: "Neither",
			Tooltip: "No ✓ and nothing cites it — unbuilt, or a decision with nothing to build (milestones carry no §pins)",
		},
	}
}

// buildBoard maps the parsed corpus onto a kanban model: one card per ADR,
// filed in the column of its frontmatter status, with its sub-item progress as
// a two-kind dot tally along the card's bottom edge.
//
// Sub-items are deliberately not cards. The corpus decomposes overwhelmingly
// into subsidiary design *decisions* rather than units of work (ADR-0092
// §Update 2026-07-15), so putting them in lifecycle columns would file a
// policy decision — an IP boundary, a performance posture — as un-started work
// forever. As a tally they read as what they are: how much of what this ADR
// decided has been declared done.
func buildBoard(adrs []adrcorpus.Adr) *kanban.Model {
	cols, colOf := buildColumns(adrs)
	cards := make([]kanban.Card, 0, len(adrs))
	for i, a := range adrs {
		cards = append(cards, kanban.Card{
			// Position, not the ADR number. Card ids must be unique — the
			// widget scopes each card's widget ids by its id — and must be
			// non-zero, since it reads a zero ParentID as "no parent". The
			// number is the tempting choice and is not in fact unique: the
			// corpus can carry two ADRs on one number when they are authored
			// concurrently, and two cards sharing an id collide silently
			// (every widget inside the second one warns and misbehaves). The
			// number is on the card's face; nothing needs it as the id.
			ID:       uint64(i + 1),
			ColumnID: colOf[a.Status],
			Title:    fmt.Sprintf("ADR-%04d — %s", a.Num, a.Title),
			Subtitle: cardSubtitle(a),
			Dots:     cardDots(a),
		})
	}
	m := kanban.NewModel(cols, cards)
	m.DotLegend = dotLegend()
	return m
}

// buildColumns yields the lanes and a status→column-id lookup. The canonical
// lifecycle columns are always present (an empty one still says "nothing is
// withdrawn"); unknown statuses are appended in first-seen order.
func buildColumns(adrs []adrcorpus.Adr) (cols []kanban.Column, colOf map[string]uint64) {
	colOf = make(map[string]uint64, len(statusOrder)+1)
	add := func(title string) {
		id := uint64(len(cols) + 1)
		cols = append(cols, kanban.Column{ID: id, Title: title})
		colOf[title] = id
	}
	for _, s := range statusOrder {
		add(s)
	}
	for _, a := range adrs {
		if _, known := colOf[a.Status]; known {
			continue
		}
		if a.Status == "" {
			add(noStatusTitle)
			colOf[""] = colOf[noStatusTitle]
			continue
		}
		add(a.Status)
	}
	return cols, colOf
}

// cardSubtitle carries the one fact the rest of the card cannot show. For a
// superseded ADR that is what replaced it; otherwise it is the freshness date.
func cardSubtitle(a adrcorpus.Adr) string {
	if a.SupersededBy != "" {
		return "→ " + a.SupersededBy
	}
	return a.LastDate
}

// cardDots buckets an ADR's sub-items into the three dot kinds and tallies
// each, done first so the card reads left-to-right as progress. The buckets are
// disjoint and sum to the sub-item count: Done wins over Cited, because a ✓ is
// a claim and evidence is only a hint — an author's mark is never downgraded by
// what the code does or doesn't say.
//
// Read from a.Subtasks rather than the ADR-level rollups: SubtasksDone and
// SubtasksCited overlap (a sub-item can be both), so they cannot be subtracted
// into buckets. An ADR that declared no sub-items carries no dots at all.
func cardDots(a adrcorpus.Adr) (dots []kanban.DotTally) {
	var done, cited, unknown int
	for _, s := range a.Subtasks {
		switch {
		case s.Done:
			done++
		case s.CodeRefs > 0:
			cited++
		default:
			unknown++
		}
	}
	for _, b := range []struct {
		id uint64
		n  int
	}{{dotDone, done}, {dotCited, cited}, {dotUnknown, unknown}} {
		if b.n > 0 {
			dots = append(dots, kanban.DotTally{ID: b.id, Count: b.n})
		}
	}
	return dots
}

// corpusSummary is the one-line readout above the board. Cited counts only the
// undeclared ones, matching the amber dots rather than Adr.SubtasksCited (which
// overlaps Done).
func corpusSummary(adrs []adrcorpus.Adr) string {
	var subs, done, cited, withSubs int
	for _, a := range adrs {
		subs += a.SubtasksTotal
		if a.SubtasksTotal > 0 {
			withSubs++
		}
		for _, s := range a.Subtasks {
			switch {
			case s.Done:
				done++
			case s.CodeRefs > 0:
				cited++
			}
		}
	}
	return fmt.Sprintf("%d ADRs · %d sub-items across %d of them · %d declared done · %d cited in code, undeclared",
		len(adrs), subs, withSubs, done, cited)
}
