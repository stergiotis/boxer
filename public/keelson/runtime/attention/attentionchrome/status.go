package attentionchrome

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pager"
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

// awayPageSize is the lines of the away card one page shows: past it the card
// pages rather than growing with what happened. A quarter of the log's bound,
// so the pager has at most four page buttons and stays within cardWidth.
const awayPageSize = attention.MaxAwayLog / 4

// awayLine is one line of the away card: the entries that differ only in
// their time and queue key, counted, with their first and last time.
type awayLine struct {
	attention.AwayEntry
	// Last is the time of the latest entry, Count how many there are.
	Last  time.Time
	Count int
	// Keys are the entries' queue keys in the log's order, which is the
	// order they arrived in, for Open.
	Keys []string
}

type awayLineKey struct {
	what         attention.AwayKindE
	why          string
	kind         string
	app          string
	instance     uint64
	conversation string
	summary      string
	level        attention.LevelE
}

// awayLines folds entries alike but for their time and key into one line,
// ordered by level then by the first one's time.
func awayLines(entries []attention.AwayEntry) (lines []awayLine) {
	at := make(map[awayLineKey]int, len(entries))
	for _, e := range entries {
		k := awayLineKey{what: e.What, why: e.Why, kind: e.Kind, app: string(e.App), instance: e.Instance,
			conversation: e.Conversation, summary: e.Summary, level: e.Level}
		i, ok := at[k]
		if !ok {
			i = len(lines)
			at[k] = i
			lines = append(lines, awayLine{AwayEntry: e, Last: e.At})
		}
		l := &lines[i]
		l.Count++
		if e.At.Before(l.At) {
			l.At = e.At
		}
		if e.At.After(l.Last) {
			l.Last = e.At
		}
		if e.Key != "" {
			l.Keys = append(l.Keys, e.Key)
		}
	}
	slices.SortStableFunc(lines, func(a, b awayLine) int {
		if r := cmp.Compare(b.Level, a.Level); r != 0 {
			return r
		}
		return a.At.Compare(b.At)
	})
	return
}

// renderAwayLog draws the away log's card while there is one to show.
func (inst *Chrome) renderAwayLog(now time.Time, ids *c.WidgetIdStack) {
	l, ok := inst.svc.Presence.Returned()
	if !ok {
		return
	}
	lines := awayLines(l.Entries)
	if inst.awayPager == nil || inst.awayPagerIds != ids {
		inst.awayPager = pager.New(ids, "attention-away-pager", pager.Options{PageSize: awayPageSize, Unit: "lines", HideSizeCombo: true})
		inst.awayPagerIds = ids
	}
	if !l.From.Equal(inst.awayFrom) {
		inst.awayFrom = l.From
		inst.awayPager.Reset()
	}
	inst.awayPager.Configure(int64(len(lines)))
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
		head := "away " + l.From.Local().Format("15:04") + " – " + l.To.Local().Format("15:04") + " · " + age(l.To.Sub(l.From))
		if n := len(l.Entries); n > len(lines) {
			head += " · " + strconv.Itoa(n) + " entries in " + strconv.Itoa(len(lines)) + " lines"
		}
		for rt := range c.RichTextLabel(head) {
			rt.Small().Weak()
		}
		if len(lines) == 0 {
			c.Label("Nothing happened.").Send()
		}
		start, end := inst.awayPager.Range()
		for i := start; i < end; i++ {
			inst.renderAwayLine(int(i), lines[i], ids)
		}
		if l.Dropped > 0 {
			for rt := range c.RichTextLabel("and " + strconv.Itoa(l.Dropped) + " more, not kept") {
				rt.Small().Weak()
			}
		}
		if len(lines) > awayPageSize {
			inst.awayPager.Render()
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

// awayWhen is a line's time, or its span when its entries' times differ.
func awayWhen(e awayLine) string {
	s := e.At.Local().Format("15:04")
	if last := e.Last.Local().Format("15:04"); last != s {
		s += "–" + last
	}
	return s
}

func (inst *Chrome) renderAwayLine(i int, e awayLine, ids *c.WidgetIdStack) {
	for range c.HorizontalTop().KeepIter() {
		c.Label(awayWhen(e)).Send()
		swatch(e.Conversation)
		what := awayWhat(e.AwayEntry)
		if e.Count > 1 {
			what += " ×" + strconv.Itoa(e.Count)
		}
		for rt := range c.RichTextLabel(what) {
			rt.Small()
		}
		c.Label(e.Summary).Send()
		switch {
		case e.What == attention.AwayArrived:
			// Open the first that still waits.
			key, waits := "", 0
			for _, k := range e.Keys {
				if _, ok := inst.find(k); ok {
					if waits == 0 {
						key = k
					}
					waits++
				}
			}
			if waits > 0 {
				if e.Count > 1 {
					for rt := range c.RichTextLabel(strconv.Itoa(waits) + " still wait") {
						rt.Weak().Small()
					}
				}
				if c.Button(ids.PrepareStr("attention-away-open-"+strconv.Itoa(i)), c.Atoms().Text("Open").Keep()).Small().SendResp().HasPrimaryClicked() {
					inst.openCard(key)
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
