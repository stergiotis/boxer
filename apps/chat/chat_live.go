package chat

import (
	"time"
	"unicode/utf8"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// The live view of a streamed answer (ADR-0286 §SD4). Deltas arrive a few
// times a second with however many words the provider wrote since the
// last, so drawing them as they arrive shows the text in jerks. A pacer
// lets the shown text trail what arrived: each time more arrives, it sets
// a rate that reveals the backlog over liveLag and holds it until the next
// arrival, so the text moves at the pace the model writes.
const (
	// liveLag is how far behind the arrived text the shown text aims to
	// stay; a backlog is revealed over about this long.
	liveLag = 250 * time.Millisecond
	// liveMinRate is the slowest reveal, in bytes a second, so the last
	// few words of a backlog do not crawl.
	liveMinRate = 60.0
	// liveParseEvery bounds how often the shown answer is parsed as
	// markdown while it grows.
	liveParseEvery = 33 * time.Millisecond
	// liveReasoningH bounds the reasoning shown under a running model
	// step; it scrolls to its latest line.
	liveReasoningH float32 = 96
	// scrollAlignBottom is ScrollToCursor's code for the bottom edge.
	scrollAlignBottom uint8 = 2
)

// pacer reveals a growing text: shown is how many of its bytes are drawn,
// at rate bytes a second, set when the text was known bytes long. key is
// the step whose text it paces; another step starts it over.
type pacer struct {
	key   int
	shown int
	known int
	rate  float64
	carry float64
	at    time.Time
}

// advance moves shown toward the text's length for the time since the
// last frame and returns the text to draw, cut at a rune start.
func (inst *pacer) advance(key int, text string, now time.Time) string {
	if key != inst.key || inst.shown > len(text) {
		*inst = pacer{key: key}
	}
	dt := 0.0
	if !inst.at.IsZero() {
		// A stalled frame does not release the whole backlog at once.
		dt = min(now.Sub(inst.at).Seconds(), 0.1)
	}
	inst.at = now
	if backlog := len(text) - inst.shown; backlog > 0 {
		if len(text) != inst.known {
			inst.known, inst.rate = len(text), max(liveMinRate, float64(backlog)/liveLag.Seconds())
		}
		inst.carry += inst.rate * dt
		n := int(inst.carry)
		inst.carry -= float64(n)
		inst.shown = min(len(text), inst.shown+n)
		for inst.shown < len(text) && !utf8.RuneStart(text[inst.shown]) {
			inst.shown++
		}
	} else {
		inst.carry = 0
	}
	return text[:inst.shown]
}

// liveView is the running turn's live answer and reasoning as drawn.
type liveView struct {
	answer    pacer
	reasoning pacer
	// doc is the shown answer parsed, of docN bytes, at parsed.
	doc    *markdown.Doc
	docN   int
	parsed time.Time
	// seen is set once an answer was drawn: the person watched it grow.
	seen bool
}

// answerDoc is the shown answer as markdown, parsed again when it grew and
// liveParseEvery passed, and at once when it is another step's.
func (inst *liveView) answerDoc(shown string, now time.Time) *markdown.Doc {
	if len(shown) != inst.docN && (now.Sub(inst.parsed) >= liveParseEvery || inst.doc == nil || len(shown) < inst.docN) {
		inst.doc, inst.docN, inst.parsed = markdown.Parse([]byte(shown)), len(shown), now
	}
	return inst.doc
}

// liveReasoning is the running model step's reasoning, paced, in a box of
// fixed height that keeps its latest line in view: it grows without moving
// what is around it.
func (inst *App) liveReasoning(i int, text string) {
	p := inst.pending
	if p == nil {
		return
	}
	shown := p.live.reasoning.advance(i, text, time.Now())
	for range c.PushId(inst.ids.PrepareStr("live-reasoning")).KeepIter() {
		for range c.ScrollArea().Vscroll(true).Hscroll(false).MaxHeight(liveReasoningH).KeepIter() {
			c.LabelAtoms(c.Atoms().BeginRichText(shown).Small().Weak().Italics().End().Keep()).Wrap().Selectable(false).Send()
			c.ScrollToCursor(scrollAlignBottom)
		}
	}
}
