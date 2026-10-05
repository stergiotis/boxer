package chat

import (
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	// trailShown is how many of the latest steps the waiting bubble lists
	// one by one; the earlier ones fold under one header.
	trailShown = 8
	// trailTextH bounds a step's arguments, result or reasoning; each
	// scrolls on its own.
	trailTextH float32 = 160

	tipTrail = "What the model did this turn, as it happens: each model call and each tool call, with how long it took. Open a step for its arguments and what came back."
	tipStep  = "Open for what was sent and what came back."
)

var atomsCopyStep = c.Atoms().Text(icons.PhCopy + " Copy step").Keep()

// trailNow is the running turn's trail, copied from the coordinator when it
// moved.
func (inst *App) trailNow() (steps []trailStep) {
	p := inst.pending
	if p == nil || inst.coord == nil {
		return nil
	}
	if got, ver, changed := inst.coord.trail.snapshot(p.trailVer); changed {
		p.trail, p.trailVer = got, ver
	}
	return p.trail
}

// renderTrail lists the running turn's steps under the waiting line: the
// latest one by one, the earlier folded, each openable to its details.
func (inst *App) renderTrail() {
	steps := inst.trailNow()
	if len(steps) == 0 {
		return
	}
	for range c.IdScope(inst.ids.PrepareStr("trail")) {
		for range c.HoverText(tipTrail).KeepIter() {
			weak(trailSummary(steps))
		}
		first := max(0, len(steps)-trailShown)
		if first > 0 {
			for range c.CollapsingHeader(inst.ids.PrepareStr("earlier"), c.WidgetText().Text(plural(first, "earlier step")).Keep()).KeepIter() {
				for i := range first {
					inst.renderStep(i, &steps[i])
				}
			}
		}
		for i := first; i < len(steps); i++ {
			inst.renderStep(i, &steps[i])
		}
	}
}

// trailSummary is the trail in a line: calls by kind, refusals, tokens.
func trailSummary(steps []trailStep) (s string) {
	var models, tools, refused int
	var in, out int64
	for i := range steps {
		st := &steps[i]
		if st.kind == stepModel {
			models++
			in, out = in+int64(st.inTokens), out+int64(st.outTokens)
			continue
		}
		tools++
		if st.refused {
			refused++
		}
	}
	s = plural(models, "model call") + " · " + plural(tools, "tool call")
	if refused > 0 {
		s += " · " + strconv.Itoa(refused) + " refused"
	}
	if in+out > 0 {
		s += " · " + tokens(in) + " in, " + tokens(out) + " out"
	}
	return
}

// renderStep is one step: its line, and under it, when opened, what was
// sent and what came back.
func (inst *App) renderStep(i int, s *trailStep) {
	for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
		if !s.done {
			for range c.HorizontalTop().KeepIter() {
				c.Spinner().Send()
				c.LabelAtoms(c.Atoms().BeginRichText(stepLine(s)).Small().End().Keep()).Selectable(false).Send()
			}
			c.RequestRepaint()
			return
		}
		if !s.hasDetail() {
			inst.stepLabel(s)
			return
		}
		for range c.HoverText(tipStep).KeepIter() {
			for range c.CollapsingHeader(inst.ids.PrepareStr("step"), c.WidgetText().Text(stepLine(s)).Keep()).KeepIter() {
				inst.renderStepDetail(s)
			}
		}
	}
}

// stepLabel is a step with nothing to open, in its tone.
func (inst *App) stepLabel(s *trailStep) {
	tone := styletokens.NeutralTextSecondary
	if s.refused || s.failed {
		tone = styletokens.ErrorDefault
	}
	for rt := range c.RichTextLabelColored(color.Hex(tone.AsHex()), color.Transparent, stepLine(s)) {
		rt.Small()
	}
}

func (inst *trailStep) hasDetail() bool {
	return inst.args != "" || inst.result != "" || inst.content != "" || inst.reasoning != ""
}

// stepLine is a step in a line: what it is, how long it took or has run,
// and how it ended.
func stepLine(s *trailStep) (line string) {
	took := s.took
	if !s.done {
		took = time.Since(s.started)
	}
	switch s.kind {
	case stepModel:
		line = icons.PhRobot + " model, round " + strconv.Itoa(s.round+1) + " · " + duration(took)
		switch {
		case !s.done:
			line += " · waiting for the answer"
		case s.failed:
			line += " · failed"
		default:
			line += " · " + tokens(int64(s.inTokens)) + " in, " + tokens(int64(s.outTokens)) + " out"
			if s.tools > 0 {
				line += " · asked for " + plural(s.tools, "tool call")
			} else {
				line += " · answered"
			}
		}
	default:
		name := s.name
		if s.title != "" {
			name = s.title + " (" + s.name + ")"
		}
		line = icons.PhGear + " " + name + " · " + duration(took)
		switch {
		case !s.done:
			line += " · running"
		case s.refused:
			line += " · refused"
		}
	}
	return
}

// renderStepDetail is what a step sent and got: a model call's text and
// reasoning, a tool call's arguments and result, each bounded and
// selectable, and Copy for the whole step.
func (inst *App) renderStepDetail(s *trailStep) {
	if s.activity != "" {
		weak(s.activity)
	}
	inst.stepText("content", "wrote", s.content)
	inst.stepText("reasoning", "reasoned", s.reasoning)
	inst.stepText("args", "arguments", s.args)
	inst.stepText("result", "came back", s.result)
	if c.Button(inst.ids.PrepareStr("copy-step"), atomsCopyStep).Small().SendResp().HasPrimaryClicked() {
		inst.copyText("the step", stepCopy(s))
	}
}

// stepText draws one part of a step under its caption, in a scroll area of
// its own.
func (inst *App) stepText(key string, caption string, text string) {
	if text == "" {
		return
	}
	weak(caption)
	for range c.PushId(inst.ids.PrepareStr("step-" + key)).KeepIter() {
		for range c.ScrollArea().Vscroll(true).Hscroll(false).MaxHeight(trailTextH).KeepIter() {
			c.LabelAtoms(c.Atoms().BeginRichText(text).Small().Monospace().End().Keep()).Wrap().Selectable(true).Send()
		}
	}
}

// stepCopy is a step as copied: its line and every part it has.
func stepCopy(s *trailStep) string {
	var b strings.Builder
	b.WriteString(stepLine(s))
	part := func(caption, text string) {
		if text != "" {
			b.WriteString("\n\n" + caption + ":\n" + text)
		}
	}
	part("line", s.activity)
	part("wrote", s.content)
	part("reasoned", s.reasoning)
	part("arguments", s.args)
	part("came back", s.result)
	return b.String()
}

// renderToolEntry is a landed tool call's line in the transcript, as
// chatview draws a system line, and its steps folded under Details.
func (inst *App) renderToolEntry(e *entry) {
	for range c.VerticalCentered().KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichText("⚙ " + e.text).Small().Weak().Italics().End().Keep()).
			Wrap().Selectable(false).Send()
	}
	if len(e.steps) == 0 {
		return
	}
	for range c.IdScope(inst.ids.PrepareStr("tool-steps")) {
		for range c.CollapsingHeader(inst.ids.PrepareStr("details"), c.WidgetText().Text("Details").Keep()).KeepIter() {
			for i := range e.steps {
				s := &e.steps[i]
				for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
					for rt := range c.RichTextLabel(stepLine(s)) {
						rt.Small().Strong()
					}
					inst.renderStepDetail(s)
				}
			}
		}
	}
}

// duration is a step's time as the person reads it.
func duration(d time.Duration) string {
	if d < time.Second {
		return strconv.FormatInt(d.Milliseconds(), 10) + " ms"
	}
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + " s"
}
