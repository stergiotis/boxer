package attentionchrome

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The person's status in host chrome (ADR-0328): the control
// names it unless Here, the list sets it, and the away log opens as one
// card when Away ends.

const tipStatus = "Your status (ADR-0328). Here: a critical decision and one left waiting too long open on their own. " +
	"Busy: only a critical one does. Away: nothing opens, and what arrives, leaves or is refused is kept for when you are back. " +
	"The host sets Away after a while without input and ends it at your next input; an Away you set stays until you change it. " +
	"Your status decides nothing and no model is told of it."

func statusGlyph(s attention.StatusE) string {
	switch s {
	case attention.StatusBusy:
		return icons.PhProhibit
	case attention.StatusAway:
		return icons.PhMoon
	}
	return icons.PhUser
}

func statusName(s attention.StatusE) string {
	switch s {
	case attention.StatusBusy:
		return "Busy"
	case attention.StatusAway:
		return "Away"
	}
	return "Here"
}

// statusPrefix is the control's word for a status other than Here.
func (inst *Chrome) statusPrefix() string {
	s, _ := inst.svc.Presence.Status()
	if s == attention.StatusHere {
		return ""
	}
	return statusGlyph(s) + " " + statusName(s) + " · "
}

// renderStatus draws the status selector in the list.
func (inst *Chrome) renderStatus(ids *c.WidgetIdStack) {
	p := inst.svc.Presence
	if p == nil {
		return
	}
	cur, byIdle := p.Status()
	for range c.HoverText(tipStatus).KeepIter() {
		for range c.HorizontalTop().KeepIter() {
			for _, s := range attention.AllStatuses {
				if c.SelectableLabel(ids.PrepareStr("attention-status-"+s.String()), s == cur, statusGlyph(s)+" "+statusName(s)).SendResp().HasPrimaryClicked() && s != cur {
					p.Set(s, time.Now())
				}
			}
			if byIdle {
				for rt := range c.RichTextLabel("set for idle time") {
					rt.Weak().Small()
				}
			}
		}
	}
}

func awayWhat(e attention.AwayEntry) string {
	switch e.What {
	case attention.AwayArrived:
		return "arrived"
	case attention.AwayLeft:
		return e.Why
	case attention.AwayRefused:
		return "refused: too many requests"
	case attention.AwayHighlight:
		return icons.PhStar + " highlight"
	}
	return e.What.String()
}

// renderAwayLog draws the away log's card while there is one to show.
func (inst *Chrome) renderAwayLog(now time.Time, ids *c.WidgetIdStack) {
	l, ok := inst.svc.Presence.Returned()
	if !ok {
		return
	}
	entries := slices.Clone(l.Entries)
	slices.SortStableFunc(entries, func(a, b attention.AwayEntry) int {
		if r := cmp.Compare(b.Level, a.Level); r != 0 {
			return r
		}
		return a.At.Compare(b.At)
	})
	wf := c.Window(ids.PrepareStr("attention-away-log"), c.WidgetText().Text("While you were away").Keep()).
		AlwaysOnTop(true).
		Collapsible(false).
		Resizable(false).
		TitleBar(true).
		DefaultOpen(true).
		DefaultPos(listX, listY)
	dismiss := false
	for range wf.KeepIter() {
		c.UiSetMinWidth(cardWidth)
		c.UiSetMaxWidth(cardWidth)
		for rt := range c.RichTextLabel("away " + l.From.Local().Format("15:04") + " – " + l.To.Local().Format("15:04") + " · " + age(l.To.Sub(l.From))) {
			rt.Small().Weak()
		}
		if len(entries) == 0 {
			c.Label("Nothing happened.").Send()
		}
		for i, e := range entries {
			inst.renderAwayEntry(i, e, ids)
		}
		if l.Dropped > 0 {
			for rt := range c.RichTextLabel("and " + strconv.Itoa(l.Dropped) + " more, not kept") {
				rt.Small().Weak()
			}
		}
		c.Separator().Send()
		if c.Button(ids.PrepareStr("attention-away-dismiss"), c.Atoms().Text("Dismiss").Keep()).SendResp().HasPrimaryClicked() {
			dismiss = true
		}
	}
	if dismiss {
		inst.svc.Presence.Dismiss()
	}
}

func (inst *Chrome) renderAwayEntry(i int, e attention.AwayEntry, ids *c.WidgetIdStack) {
	for range c.HorizontalTop().KeepIter() {
		c.Label(e.At.Local().Format("15:04")).Send()
		swatch(e.Conversation)
		for rt := range c.RichTextLabel(awayWhat(e)) {
			rt.Small()
		}
		c.Label(e.Summary).Send()
		switch {
		case e.What == attention.AwayArrived:
			if _, waiting := inst.find(e.Key); waiting {
				if c.Button(ids.PrepareStr("attention-away-open-"+strconv.Itoa(i)), c.Atoms().Text("Open").Keep()).Small().SendResp().HasPrimaryClicked() {
					inst.openCard(e.Key)
				}
			} else {
				for rt := range c.RichTextLabel("no longer waits") {
					rt.Weak().Small()
				}
			}
		case e.What == attention.AwayHighlight && inst.raiseW != nil && e.Instance != 0:
			if c.Button(ids.PrepareStr("attention-away-open-"+strconv.Itoa(i)), c.Atoms().Text("Open").Keep()).Small().SendResp().HasPrimaryClicked() {
				inst.raiseW(e.Instance)
				if inst.svc.Notices != nil {
					inst.svc.Notices.Seen(e.Conversation)
				}
				inst.read = time.Time{}
			}
		}
	}
	if e.What == attention.AwayRefused && e.Why != "" {
		for rt := range c.RichTextLabel(e.Why) {
			rt.Small().Weak()
		}
	}
}
