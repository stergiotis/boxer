package attentionchrome

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// urgencyCards draw a window's request to make a conversation urgent, or to
// stop (ADR-0323 §SD4, update of 2026-10-12). Everything on the card is the
// host's: the request carries no model text, only the conversation's title
// as the app reported it. Only Make urgent here changes the levels.
type urgencyCards struct {
	u       *attention.Urgencies
	display func(app.AppIdT) string
	raise   func(instance uint64)
}

var _ CardSourceI = (*urgencyCards)(nil)

func (inst *urgencyCards) OwnsCard(key string) bool { return attention.OwnsUrgency(key) }

func (inst *urgencyCards) ForgetCard(string) {}

func (inst *urgencyCards) RenderCard(key string, ids *c.WidgetIdStack) (decided bool) {
	conv, title, on, from, instance, ok := inst.u.Lookup(key)
	if !ok {
		c.Label("This request is no longer waiting.").Send()
		return true
	}
	who := string(from)
	if inst.display != nil {
		who = inst.display(from)
	}
	name := title
	if name == "" {
		name = "conversation " + conv
	}
	if on {
		c.Label(who + " asks to make " + name + " urgent.").Wrap().Send()
		c.Label("What it asks of you would open at once, as critical — also while you are Busy — until you stop it. " +
			"Nothing changes unless you choose Make urgent here.").Wrap().Send()
	} else {
		c.Label(who + " asks to stop treating " + name + " as urgent.").Wrap().Send()
		c.Label("What it asks of you would wait in the queue again, at its usual level.").Wrap().Send()
	}
	accept, decline := "Make urgent", "Decline"
	if !on {
		accept, decline = "Stop urgency", "Keep urgent"
	}
	for range c.HorizontalTop().KeepIter() {
		if c.Button(ids.PrepareStr("attention-u-accept-"+key), c.Atoms().Text(accept).Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
			decided, _ = inst.u.Decide(key, true)
		}
		if c.Button(ids.PrepareStr("attention-u-decline-"+key), c.Atoms().Text(decline).Keep()).SendResp().HasPrimaryClicked() {
			decided, _ = inst.u.Decide(key, false)
		}
		if inst.raise != nil && instance != 0 {
			if c.Button(ids.PrepareStr("attention-u-open-"+key), c.Atoms().Text("Open").Keep()).SendResp().HasPrimaryClicked() {
				inst.raise(instance)
			}
		}
	}
	return
}
