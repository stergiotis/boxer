package chat

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

const (
	// askNoteWidth is the width of a note and of the answer of one's own.
	askNoteWidth float32 = 320

	hintAskNote   = "a note on this choice (optional)"
	hintAskOther  = "or answer in your own words"
	askIncomplete = "Each question needs a choice or an answer of your own — or Skip."
)

var (
	atomsAnswer = c.Atoms().Text(icons.PhCheck + " Answer").Keep()
	atomsSkip   = c.Atoms().Text("Skip").Keep()
)

// renderAsk draws an open ask_user in the pending bubble: each question with
// its header, options as radio buttons or checkboxes, a note under each
// chosen option and a line for an answer of the person's own; then Answer
// and Skip. Ids are scoped by the question's sequence, so a second question
// in one turn starts from clean widget state. Each level is keyed by a
// string of its own: the id stack combines scopes by XOR, under which
// nested PrepareSeq indices commute and cancel — question 0's option 0 and
// question 1's option 1 would be one widget.
func (inst *App) renderAsk(o *openAsk) {
	f := &o.form
	for range c.IdScope(inst.ids.PrepareStr("ask")) {
		for range c.IdScope(inst.ids.PrepareStr("ask-" + strconv.FormatUint(o.seq, 10))) {
			for i := range o.questions {
				if i > 0 {
					c.Separator().Send()
				}
				for range c.IdScope(inst.ids.PrepareStr("question-" + strconv.Itoa(i))) {
					inst.renderAskQuestion(f, i, &o.questions[i])
				}
			}
			if f.sent {
				for range c.HorizontalTop().KeepIter() {
					c.Spinner().Send()
					c.Label("Answered").Selectable(false).Send()
				}
				return
			}
			if f.incomplete {
				for rt := range c.RichTextLabel(askIncomplete) {
					rt.Small().Weak()
				}
			}
			// Right-aligned, Answer at the edge and Skip beside it: a
			// right-to-left row places its first child rightmost.
			for range c.UiWithLayout().MainDirRightToLeft().CrossAlignMin().KeepIter() {
				if c.Button(inst.ids.PrepareStr("answer"), atomsAnswer).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
					o.submit()
				}
				if c.Button(inst.ids.PrepareStr("skip"), atomsSkip).SendResp().HasPrimaryClicked() {
					o.skip()
				}
			}
		}
	}
}

func (inst *App) renderAskQuestion(f *askForm, i int, q *askQuestion) {
	for range c.HorizontalTop().KeepIter() {
		if q.Header != "" {
			badge.New(inst.ids.PrepareStr("header"), q.Header).Tone(badge.ToneInfo).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
		for rt := range c.RichTextLabel(q.Question) {
			rt.Strong()
		}
	}
	how := "choose one"
	if q.MultiSelect {
		how = "choose any"
	}
	for rt := range c.RichTextLabel(how) {
		rt.Small().Weak()
	}
	for j := range q.Options {
		opt := &q.Options[j]
		for range c.IdScope(inst.ids.PrepareStr("option-" + strconv.Itoa(j))) {
			if q.MultiSelect {
				c.Checkbox(inst.ids.PrepareStr("pick"), f.chosen[i][j], opt.Label).SendRespVal(&f.chosen[i][j])
			} else if selector.RadioValue(inst.ids.PrepareStr("pick"), &f.single[i], j).Text(opt.Label).SendResp() {
				for k := range f.chosen[i] {
					f.chosen[i][k] = k == j
				}
			}
			if opt.Description == "" && !f.chosen[i][j] {
				continue
			}
			for range c.Indent(inst.ids.PrepareStr("under")).KeepIter() {
				if opt.Description != "" {
					for rt := range c.RichTextLabel(opt.Description) {
						rt.Small().Weak()
					}
				}
				if f.chosen[i][j] {
					c.TextEdit(inst.ids.PrepareStr("note"), f.notes[i][j], false).
						HintText(hintAskNote).DesiredWidth(askNoteWidth).Interactive(!f.sent).
						SendRespVal(&f.notes[i][j])
				}
			}
		}
	}
	c.TextEdit(inst.ids.PrepareStr("other"), f.other[i], false).
		HintText(hintAskOther).DesiredWidth(askNoteWidth).Interactive(!f.sent).
		SendRespVal(&f.other[i])
}
