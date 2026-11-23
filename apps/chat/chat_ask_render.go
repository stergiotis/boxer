package chat

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/decision"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

const (
	// askNoteWidth is the width of a note and of the answer of one's own.
	askNoteWidth float32 = 320

	hintAskNote   = "a note on this choice (optional)"
	hintAskOther  = "or answer in your own words"
	askIncomplete = "Each question needs a choice or an answer of your own — or Skip."
)

// renderAsk draws an open ask_user in the pending bubble as a decision the
// turn waits on (ADR-0404 §SD3; asked of the person, so neither answer is
// primary, §SD4): each question with its header, options as radio buttons
// or checkboxes, a note under each chosen option and a line for an answer
// of the person's own, before Answer and Skip. Ids are scoped by the
// question's sequence, so a second question in one turn starts from clean
// widget state. Each level is keyed by a string of its own: the id stack
// combines scopes by XOR, under which nested PrepareSeq indices commute and
// cancel — question 0's option 0 and question 1's option 1 would be one
// widget.
func (inst *App) renderAsk(o *openAsk) {
	f := &o.form
	if len(o.questions) == 1 && o.questions[0].Buttons && !o.questions[0].MultiSelect {
		inst.renderAskButtons(o)
		return
	}
	for range c.IdScope(inst.ids.PrepareStr("ask")) {
		for range c.IdScope(inst.ids.PrepareStr("ask-" + strconv.FormatUint(o.seq, 10))) {
			questions := func() {
				for i := range o.questions {
					if i > 0 {
						c.Separator().Send()
					}
					for range c.IdScope(inst.ids.PrepareStr("question-" + strconv.Itoa(i))) {
						inst.renderAskQuestion(f, i, &o.questions[i])
					}
				}
			}
			if f.sent {
				questions()
				askAnswered()
				return
			}
			title := "The turn waits for your answer"
			if n := len(o.questions); n > 1 {
				title = "The turn waits for your answers to " + strconv.Itoa(n) + " questions"
			}
			in := decision.Input{
				Ids: inst.ids, ScopeKey: "ask-decision", Origin: decision.OriginRequest,
				Title: title,
				Follows: []string{
					"Answer sends your choices, notes and own words; the turn goes on with them.",
					"Skip goes on without an answer, and the model is told you skipped.",
				},
				Form: questions,
				Options: []decision.Option{
					{Key: "answer", Label: "Answer", Icon: icons.PhCheck, Role: decision.RoleAffirm},
					{Key: "skip", Label: "Skip", Role: decision.RoleDecline},
				},
			}
			if f.incomplete {
				in.Notes = []decision.Note{{Text: askIncomplete, Tone: styletokens.ToneWarning}}
			}
			switch decision.Render(in).Chosen {
			case "answer":
				o.submit()
			case "skip":
				o.skip()
			}
		}
	}
}

// askAnswered is the line an answered question shows until the turn takes it.
func askAnswered() {
	for range c.HorizontalTop().KeepIter() {
		c.Spinner().Send()
		c.Label("Answered").Selectable(false).Send()
	}
}

// renderAskButtons draws a harness question — a stop that fired, asking
// whether the turn goes on — as a decision answered on the click (ADR-0404
// §SD3, §SD4: its options are neither affirmative nor decline, so none
// is filled). Each option's
// description says what it does, as a sentence of its own.
func (inst *App) renderAskButtons(o *openAsk) {
	q := &o.questions[0]
	for range c.IdScope(inst.ids.PrepareStr("ask-buttons-" + strconv.FormatUint(o.seq, 10))) {
		title := q.Question
		if q.Header != "" {
			title = q.Header + ": " + q.Question
		}
		if o.form.sent {
			c.LabelAtoms(c.Atoms().BeginRichText(title).Strong().End().Keep()).Wrap().Selectable(true).Send()
			askAnswered()
			return
		}
		in := decision.Input{Ids: inst.ids, ScopeKey: "ask-buttons", Origin: decision.OriginRequest, Title: title}
		for j := range q.Options {
			opt := &q.Options[j]
			role := decision.RoleOther
			switch j {
			case 0:
				role = decision.RoleAffirm
			case 1:
				role = decision.RoleDecline
			}
			in.Options = append(in.Options, decision.Option{Key: "option-" + strconv.Itoa(j), Label: opt.Label, Tooltip: opt.Description, Role: role})
			if opt.Description != "" {
				in.Follows = append(in.Follows, opt.Label+": "+opt.Description+".")
			}
		}
		if res := decision.Render(in); res.Has {
			if j, err := strconv.Atoi(strings.TrimPrefix(res.Chosen, "option-")); err == nil {
				o.choose(0, j)
			}
		}
	}
}

func (inst *App) renderAskQuestion(f *askForm, i int, q *askQuestion) {
	// Wrapped: a question longer than the pane is read, not run off its edge
	// — a held turn's names its stop and the row that fired (ADR-0370).
	for range c.HorizontalWrapped().KeepIter() {
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
