package attentionchrome

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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
	if q.Kind == attention.KindExitApproval {
		head += " asks you to approve an exit — the model's words follow"
	} else {
		head += " asks — the model's words follow"
	}
	c.Label(head).Wrap().Send()
	if q.Tainted {
		c.Label("This conversation has read untrusted content: what the model writes may have been steered by it.").Wrap().Send()
	}
	form := q.Form()
	quick := q.Quick()
	picked := inst.pickedOf(key, form)
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
	if !quick {
		c.Label("This form is answered in its window.").Wrap().Send()
	}
	for range c.HorizontalTop().KeepIter() {
		if quick {
			choices := choicesOf(picked)
			ready := q.ValidChoices(choices)
			for range c.EnabledUi(ready).KeepIter() {
				if c.Button(ids.PrepareStr("attention-q-answer-"+key), c.Atoms().Text("Answer").Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() && ready {
					decided = inst.q.Decide(key, false, choices)
				}
			}
			if c.Button(ids.PrepareStr("attention-q-skip-"+key), c.Atoms().Text("Skip").Keep()).SendResp().HasPrimaryClicked() {
				decided = inst.q.Decide(key, true, nil)
			}
		}
		if inst.raise != nil && instance != 0 {
			if c.Button(ids.PrepareStr("attention-q-open-"+key), c.Atoms().Text("Open").Keep()).SendResp().HasPrimaryClicked() {
				inst.raise(instance)
			}
		}
	}
	return
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
