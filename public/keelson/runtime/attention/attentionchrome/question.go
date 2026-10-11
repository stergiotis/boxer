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
	// choice is the option picked in each open card, -1 for none.
	choice map[string]int
}

var _ CardSourceI = (*questionCards)(nil)

func (inst *questionCards) OwnsCard(key string) bool { return attention.OwnsItem(key) }

func (inst *questionCards) ForgetCard(key string) { delete(inst.choice, key) }

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
	for range c.Frame(ids.PrepareStr("attention-q-frame-" + key)).KeepIter() {
		if q.Header != "" {
			for rt := range c.RichTextLabel(q.Header) {
				rt.Weak().Italics()
			}
		}
		for rt := range c.RichTextLabel(q.Text) {
			rt.Italics()
		}
		if q.Quick() {
			pick, has := inst.choice[key]
			if !has {
				pick = -1
			}
			for i, o := range q.Options {
				text := o.Label
				if o.Description != "" {
					text += " — " + o.Description
				}
				clicked := pick == i
				if c.RadioButton(ids.PrepareStr("attention-q-opt-"+key+"-"+strconv.Itoa(i)), c.Atoms().Text(text).Keep(), pick == i).SendRespVal(&clicked).HasPrimaryClicked() {
					pick = i
				}
			}
			if inst.choice == nil {
				inst.choice = make(map[string]int, 4)
			}
			inst.choice[key] = pick
		}
	}
	if !q.Quick() {
		c.Label("This form is answered in its window.").Wrap().Send()
	}
	for range c.HorizontalTop().KeepIter() {
		if q.Quick() {
			pick := inst.choice[key]
			if c.Button(ids.PrepareStr("attention-q-answer-"+key), c.Atoms().Text("Answer").Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() && pick >= 0 {
				decided = inst.q.Decide(key, false, pick)
			}
			if c.Button(ids.PrepareStr("attention-q-skip-"+key), c.Atoms().Text("Skip").Keep()).SendResp().HasPrimaryClicked() {
				decided = inst.q.Decide(key, true, 0)
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
