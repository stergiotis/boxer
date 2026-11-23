package attentionchrome

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/decision"
)

// questionCards draws the cards of questions apps post (ADR-0324 §SD5). The
// model wrote the question and its options, so the card is the one host
// surface whose choices the model wrote: it heads itself with host-written
// text, draws the model's words as the model's, uses none of a grant card's
// vocabulary and offers Answer, Skip and Open only.
type questionCards struct {
	q       *attention.Questions
	display func(app.AppIdT) string
	raise   func(instance uint64)
	// picked are the options picked in each open card, per question.
	picked map[string][][]bool
}

var _ CardSourceI = (*questionCards)(nil)

func (inst *questionCards) OwnsCard(key string) bool { return attention.OwnsItem(key) }

func (inst *questionCards) ForgetCard(key string) { delete(inst.picked, key) }

func (inst *questionCards) RenderCard(key string, ids *c.WidgetIdStack) (decided bool) {
	q, from, instance, ok := inst.q.Lookup(key)
	if !ok {
		c.Label("This question is no longer waiting.").Send()
		return true
	}
	who := string(from)
	if inst.display != nil {
		who = inst.display(from)
	}
	head := who
	if q.Title != "" {
		head += " · " + q.Title
	}
	switch {
	case attention.IsHostQuestion(from):
		// The host's own question: no model wrote it.
		head = "This application asks"
		if q.Title != "" {
			head = q.Title + " — this application asks"
		}
	case q.Kind == attention.KindExitApproval:
		head += " asks you to approve an exit — the model's words follow"
	default:
		head += " asks — the model's words follow"
	}
	const taintedText = "This conversation has read untrusted content: what the model writes may have been steered by it."
	form := q.Form()
	quick := q.Quick()
	picked := inst.pickedOf(key, form)
	drawForm := func() {
		for qi, fq := range form {
			for range c.Frame(ids.PrepareStr("attention-q-frame-" + key + "-" + strconv.Itoa(qi))).KeepIter() {
				if fq.Header != "" {
					for rt := range c.RichTextLabel(fq.Header) {
						rt.Weak().Italics()
					}
				}
				for rt := range c.RichTextLabel(fq.Text) {
					rt.Italics()
				}
				if !quick {
					continue
				}
				for i, o := range fq.Options {
					text := o.Label
					if o.Description != "" {
						text += " — " + o.Description
					}
					id := ids.PrepareStr("attention-q-opt-" + key + "-" + strconv.Itoa(qi) + "-" + strconv.Itoa(i))
					if fq.Multi {
						// Bound to the pick itself: the value comes back at the
						// next sync, into the variable given here, so it must
						// outlive the frame.
						c.Checkbox(id, picked[qi][i], text).SendRespVal(&picked[qi][i])
						continue
					}
					clicked := picked[qi][i]
					if c.RadioButton(id, c.Atoms().Text(text).Keep(), picked[qi][i]).SendRespVal(&clicked).HasPrimaryClicked() {
						for j := range picked[qi] {
							picked[qi][j] = j == i
						}
					}
				}
			}
		}
	}
	if !quick {
		// No answer is given on the card, so there is no decision to draw:
		// the card says where the form is answered.
		c.Label(head).Wrap().Send()
		if q.Tainted {
			c.Label(taintedText).Wrap().Send()
		}
		drawForm()
		c.Label("This form is answered in its window.").Wrap().Send()
		inst.renderOpen(key, instance, ids)
		return
	}
	// Answer leads, as the affirmative does in every decision (ADR-0404
	// SD4). Whether Answer is enabled is read before
	// the form draws, so it follows a pick one frame late; the click is
	// checked against the picks after the form has drawn.
	in := decision.Input{
		Ids:      ids,
		ScopeKey: "attention-q-" + key,
		Origin:   decision.OriginRequest,
		Title:    head,
		Form:     drawForm,
		Unframed: true,
		Options: []decision.Option{
			{Key: "answer", Label: "Answer", Role: decision.RoleAffirm,
				Disabled: !q.ValidChoices(choicesOf(picked)),
				Why:      "Answer is off until each question above has a choice."},
			{Key: "skip", Label: "Skip", Role: decision.RoleDecline},
		},
	}
	if q.Tainted {
		in.Notes = []decision.Note{{Text: taintedText, Tone: styletokens.ToneWarning}}
	}
	switch decision.Render(in).Chosen {
	case "answer":
		if choices := choicesOf(picked); q.ValidChoices(choices) {
			decided = inst.q.Decide(key, false, choices)
		}
	case "skip":
		decided = inst.q.Decide(key, true, nil)
	}
	// Open is a card-level act, not an answer: it stays outside the
	// decision.
	inst.renderOpen(key, instance, ids)
	return
}

// renderOpen draws Open, which raises the window that asked.
func (inst *questionCards) renderOpen(key string, instance uint64, ids *c.WidgetIdStack) {
	if inst.raise != nil && instance != 0 {
		if c.Button(ids.PrepareStr("attention-q-open-"+key), c.Atoms().Text("Open").Keep()).SendResp().HasPrimaryClicked() {
			inst.raise(instance)
		}
	}
}

// pickedOf is the card's picks, shaped to its form.
func (inst *questionCards) pickedOf(key string, form []attention.FormQuestion) (picked [][]bool) {
	if inst.picked == nil {
		inst.picked = make(map[string][][]bool, 4)
	}
	picked = inst.picked[key]
	if len(picked) != len(form) {
		picked = make([][]bool, len(form))
		for i, fq := range form {
			picked[i] = make([]bool, len(fq.Options))
		}
		inst.picked[key] = picked
	}
	return
}

// choicesOf is the picks as the indices Decide takes.
func choicesOf(picked [][]bool) (choices [][]int) {
	choices = make([][]int, len(picked))
	for qi, row := range picked {
		for i, on := range row {
			if on {
				choices[qi] = append(choices[qi], i)
			}
		}
	}
	return
}
