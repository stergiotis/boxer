// Package attentionchrome draws the person's queue in host chrome (ADR-0324,
// proposed): a control in the shell's top panel listing what waits on the
// person, and a non-modal card per opened item. Both are drawn by the host,
// outside every app's window body, so neither an app nor an agent can draw
// or click them; cards sit on the foreground layer, above app windows
// (§SD2).
package attentionchrome

import (
	"slices"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// CardSourceI is a source that draws the body of its items' cards.
type CardSourceI interface {
	// OwnsCard reports whether key is one of the source's items.
	OwnsCard(key string) bool
	// RenderCard draws the body of key's card; decided says the person
	// decided it and the card closes.
	RenderCard(key string, ids *c.WidgetIdStack) (decided bool)
	// ForgetCard drops what the card fixed when it opened.
	ForgetCard(key string)
}

// ShelveFor is how long Shelve takes an item out of the queue, bounded by the
// item's own clock (ADR-0324 §SD4).
const ShelveFor = 15 * time.Minute

const cardWidth float32 = 520

// Chrome draws the queue of svc. Its methods run on the render goroutine.
type Chrome struct {
	svc     *attention.Service
	sources []CardSourceI
	// listOpen is whether the control's list is shown, levelsOpen whether
	// the grid of the person's levels is (levels.go).
	listOpen   bool
	levelsOpen bool
	// open are the keys of the open cards, in opening order; raise those
	// to be raised this frame.
	open  []string
	raise []string
	// entries and shelved are the queue as last read.
	entries []attention.Entry
	shelved []attention.Entry
	// notices are the highlights as last read (ADR-0323 §SD4): counted
	// beside what waits, never opened as cards.
	notices []attention.Notice
	read    time.Time
	display func(app.AppIdT) string
	raiseW  func(instance uint64)
}

// Options are what the chrome needs from the host: an app's display name,
// and how to raise a window.
type Options struct {
	Display func(app.AppIdT) string
	Raise   func(instance uint64)
}

// New is the chrome of svc; it draws the cards of the questions apps post
// (ADR-0324 §SD5) itself.
func New(svc *attention.Service, opts Options) (inst *Chrome) {
	inst = &Chrome{svc: svc, display: opts.Display, raiseW: opts.Raise}
	if svc.Questions != nil {
		inst.sources = append(inst.sources, &questionCards{q: svc.Questions, display: opts.Display, raise: opts.Raise})
	}
	return
}

// AddSource adds a source of the queue and what draws its cards.
func (inst *Chrome) AddSource(src attention.SourceI, cards CardSourceI) {
	inst.sources = append(inst.sources, cards)
	inst.svc.Queue.AddSource(src)
}

// refresh reads the queue once per frame: the control and the cards share
// the reading.
func (inst *Chrome) refresh(now time.Time) {
	if now.Sub(inst.read) < 50*time.Millisecond && !inst.read.IsZero() {
		return
	}
	inst.read = now
	inst.entries, inst.shelved = inst.svc.Queue.Snapshot(now)
	if inst.svc.Notices != nil {
		inst.notices = inst.svc.Notices.List()
	}
	for _, k := range inst.svc.Queue.TakeOpens(inst.entries) {
		inst.openCard(k)
	}
}

func (inst *Chrome) openCard(key string) {
	if !slices.Contains(inst.open, key) {
		inst.open = append(inst.open, key)
	}
	inst.raise = append(inst.raise, key)
}

func (inst *Chrome) closeCard(key string) {
	if i := slices.Index(inst.open, key); i >= 0 {
		inst.open = slices.Delete(inst.open, i, i+1)
	}
	if s := inst.sourceOf(key); s != nil {
		s.ForgetCard(key)
	}
}

func (inst *Chrome) sourceOf(key string) CardSourceI {
	for _, s := range inst.sources {
		if s.OwnsCard(key) {
			return s
		}
	}
	return nil
}

func (inst *Chrome) find(key string) (e attention.Entry, ok bool) {
	for _, x := range inst.entries {
		if x.Key == key {
			return x, true
		}
	}
	for _, x := range inst.shelved {
		if x.Key == key {
			return x, true
		}
	}
	return
}

func levelGlyph(l attention.LevelE) string {
	if l == attention.LevelCritical {
		return icons.PhWarning
	}
	return icons.PhHandPalm
}

// RenderControl draws the control in the shell's menu bar: the count of
// items waiting, and a menu of them, highest level first, with Next
// (ADR-0324 §SD1); then, quieter, the count of highlights.
func (inst *Chrome) RenderControl(ids *c.WidgetIdStack) {
	now := time.Now()
	inst.refresh(now)
	label := icons.PhCheck + " nothing waits on you"
	if n := len(inst.entries); n > 0 {
		label = levelGlyph(inst.entries[0].Level) + " your turn · " + strconv.Itoa(n)
		for _, e := range inst.entries {
			if e.NearBound {
				label += " · expiring"
				break
			}
		}
	}
	if n := len(inst.notices); n > 0 {
		// Quieter than what waits: a count, after it.
		label += "   " + icons.PhStar + " " + strconv.Itoa(n)
	}
	// A button and a host window rather than a menu: the list closes when
	// the person opens an item from it, which a menu here cannot be told.
	if c.Button(ids.PrepareStr("attention-control"), c.Atoms().Text(label).Keep()).Frame(false).Selected(inst.listOpen).SendResp().HasPrimaryClicked() {
		inst.listOpen = !inst.listOpen
	}
}

// renderList draws the list the control opens: Next, and every item with
// Open, Acknowledge and Shelve.
func (inst *Chrome) renderList(now time.Time, ids *c.WidgetIdStack) {
	if !inst.listOpen {
		return
	}
	wf := c.Window(ids.PrepareStr("attention-list"), c.WidgetText().Text("Waiting on you").Keep()).
		AlwaysOnTop(true).
		Collapsible(false).
		Resizable(false).
		TitleBar(true).
		DefaultOpen(true).
		DefaultPos(listX, listY)
	for range wf.KeepIter() {
		for range c.HorizontalTop().KeepIter() {
			if len(inst.entries) > 0 {
				if c.Button(ids.PrepareStr("attention-next"), c.Atoms().Text("Next "+icons.PhCaretRight).Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
					inst.next()
					inst.listOpen = false
				}
			}
			for range c.HoverText(tipLevels).KeepIter() {
				if c.Button(ids.PrepareStr("attention-levels-open"), c.Atoms().Text("Levels").Keep()).Selected(inst.levelsOpen).SendResp().HasPrimaryClicked() {
					inst.levelsOpen = !inst.levelsOpen
				}
			}
			if c.Button(ids.PrepareStr("attention-list-close"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
				inst.listOpen = false
			}
		}
		for _, e := range inst.entries {
			inst.renderRow(e, now, ids)
		}
		if len(inst.shelved) > 0 {
			c.Separator().Send()
			for rt := range c.RichTextLabel("shelved") {
				rt.Weak()
			}
			for _, e := range inst.shelved {
				inst.renderRow(e, now, ids)
			}
		}
		if len(inst.entries) == 0 && len(inst.shelved) == 0 {
			c.Label("Nothing waits on you.").Send()
		}
		if len(inst.notices) > 0 {
			c.Separator().Send()
			for rt := range c.RichTextLabel(icons.PhStar + " highlights — nothing waits on these") {
				rt.Weak()
			}
			for _, n := range inst.notices {
				inst.renderNotice(n, now, ids)
			}
		}
	}
}

// renderNotice draws a highlight: what happened in which conversation, with
// Open, which raises the window that reported it, and Dismiss. Either
// drops it.
func (inst *Chrome) renderNotice(n attention.Notice, now time.Time, ids *c.WidgetIdStack) {
	k := n.Conversation
	name := n.Title
	if name == "" {
		name = "conversation " + n.Conversation
	}
	text := n.Text
	if text == "" {
		text = n.Kind
	}
	where := string(n.App)
	if inst.display != nil {
		where = inst.display(n.App)
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label(icons.PhStar + " " + age(now.Sub(n.Created))).Send()
		swatch(n.Conversation)
		c.Label(text + " · " + name).Send()
		for rt := range c.RichTextLabel(where) {
			rt.Weak().Small()
		}
	}
	for range c.HorizontalTop().KeepIter() {
		if inst.raiseW != nil && n.Instance != 0 {
			if c.Button(ids.PrepareStr("attention-notice-open-"+k), c.Atoms().Text("Open").Keep()).Small().SendResp().HasPrimaryClicked() {
				inst.raiseW(n.Instance)
				inst.svc.Notices.Seen(n.Conversation)
				inst.listOpen = false
				inst.read = time.Time{}
			}
		}
		if c.Button(ids.PrepareStr("attention-notice-dismiss-"+k), c.Atoms().Text("Dismiss").Keep()).Small().SendResp().HasPrimaryClicked() {
			inst.svc.Notices.Seen(n.Conversation)
			inst.read = time.Time{}
		}
	}
}

// Where the list first opens: under the menu bar's control.
const (
	listX float32 = 160
	listY float32 = 28
)

// next opens the first item whose card is not open.
func (inst *Chrome) next() {
	for _, e := range inst.entries {
		if !slices.Contains(inst.open, e.Key) {
			inst.openCard(e.Key)
			return
		}
	}
	if len(inst.entries) > 0 {
		inst.raise = append(inst.raise, inst.entries[0].Key)
	}
}

func (inst *Chrome) renderRow(e attention.Entry, now time.Time, ids *c.WidgetIdStack) {
	k := e.Key
	for range c.HorizontalTop().KeepIter() {
		c.Label(levelGlyph(e.Level) + " " + e.Level.String() + " · " + age(now.Sub(e.Created))).Send()
		swatch(e.Conversation)
		c.Label(e.Summary).Send()
		if e.Acked && e.ShelvedUntil.IsZero() {
			for rt := range c.RichTextLabel("acknowledged") {
				rt.Weak().Small()
			}
		}
		if e.NearBound {
			for rt := range c.RichTextLabel(e.BoundWhy + " in " + age(e.Bound.Sub(now))) {
				rt.Small()
			}
		}
	}
	for range c.HorizontalTop().KeepIter() {
		if c.Button(ids.PrepareStr("attention-open-"+k), c.Atoms().Text("Open").Keep()).Small().SendResp().HasPrimaryClicked() {
			inst.openCard(k)
			inst.listOpen = false
		}
		inst.renderPersonActs(e, now, ids, "row")
	}
}

// renderPersonActs draws Acknowledge and Shelve, or Unshelve (ADR-0324
// §SD4).
func (inst *Chrome) renderPersonActs(e attention.Entry, now time.Time, ids *c.WidgetIdStack, scope string) {
	k := scope + "-" + e.Key
	q := inst.svc.Queue
	if !e.ShelvedUntil.IsZero() {
		c.Label("until " + e.ShelvedUntil.Local().Format("15:04")).Send()
		if c.Button(ids.PrepareStr("attention-unshelve-"+k), c.Atoms().Text("Unshelve").Keep()).Small().SendResp().HasPrimaryClicked() {
			q.Unshelve(e.Item, now)
			inst.read = time.Time{}
		}
		return
	}
	if !e.Acked {
		if c.Button(ids.PrepareStr("attention-ack-"+k), c.Atoms().Text("Acknowledge").Keep()).Small().SendResp().HasPrimaryClicked() {
			q.Acknowledge(e.Item, now)
			inst.read = time.Time{}
		}
	}
	tip := "Out of the queue for " + age(ShelveFor) + ", or until it " + e.BoundWhy + " if that is sooner: shelving extends no clock."
	if e.Bound.IsZero() {
		tip = "Out of the queue for " + age(ShelveFor) + "."
	}
	for range c.HoverText(tip).KeepIter() {
		if c.Button(ids.PrepareStr("attention-shelve-"+k), c.Atoms().Text("Shelve "+age(ShelveFor)).Keep()).Small().SendResp().HasPrimaryClicked() {
			q.Shelve(e.Item, now.Add(ShelveFor), now)
			inst.closeCard(e.Key)
			inst.read = time.Time{}
		}
	}
}

// RenderCards draws the open cards at the top level: non-modal windows on the
// foreground layer, framed with host-written text, the body drawn by the
// item's source (ADR-0324 §SD2). Call it once per frame, outside every app's
// window.
func (inst *Chrome) RenderCards(ids *c.WidgetIdStack) {
	now := time.Now()
	inst.refresh(now)
	inst.renderList(now, ids)
	inst.renderLevels(ids)
	raise := inst.raise
	inst.raise = inst.raise[:0]
	for _, key := range slices.Clone(inst.open) {
		e, ok := inst.find(key)
		src := inst.sourceOf(key)
		if !ok || src == nil {
			inst.closeCard(key)
			continue
		}
		title := levelGlyph(e.Level) + " Your turn"
		if e.Level == attention.LevelCritical {
			title += " — critical"
		}
		wf := c.Window(ids.PrepareStr("attention-card-"+key), c.WidgetText().Text(title).Keep()).
			AlwaysOnTop(true).
			Collapsible(false).
			Resizable(false).
			TitleBar(true).
			DefaultOpen(true)
		if slices.Contains(raise, key) {
			c.MoveWindowToTop(wf.Handle())
		}
		closeIt := false
		for range wf.KeepIter() {
			c.UiSetMinWidth(cardWidth)
			c.UiSetMaxWidth(cardWidth)
			for range c.HorizontalTop().KeepIter() {
				swatch(e.Conversation)
				for rt := range c.RichTextLabel(e.Summary) {
					rt.Strong()
				}
			}
			meta := "waiting " + age(now.Sub(e.Created))
			if !e.Bound.IsZero() {
				meta += " · " + e.BoundWhy + " in " + age(e.Bound.Sub(now))
			}
			if e.Conversation != "" {
				meta += " · conversation " + e.Conversation
			}
			for rt := range c.RichTextLabel(meta) {
				rt.Small().Weak()
			}
			c.Separator().Send()
			for range c.PushId(ids.PrepareStr("attention-card-body-" + key)).KeepIter() {
				if src.RenderCard(key, ids) {
					closeIt = true
				}
			}
			c.Separator().Send()
			for range c.HorizontalTop().KeepIter() {
				inst.renderPersonActs(e, now, ids, "card")
				if c.Button(ids.PrepareStr("attention-close-"+key), c.Atoms().Text("Close").Keep()).Small().SendResp().HasPrimaryClicked() {
					closeIt = true
				}
			}
		}
		if closeIt {
			inst.closeCard(key)
			inst.read = time.Time{}
		}
	}
}

// swatch is the conversation's colour (ADR-0324 background §3.12), as the
// chat's roster shows it; nothing for an item of no conversation. It adds
// to what names the conversation — a question's summary, the card's line
// beneath — and does not stand in for it.
func swatch(conversation string) {
	if conversation == "" {
		return
	}
	for range c.RichTextLabelColored(color.FromImage(attention.ConversationColor(conversation)), color.Transparent, "●") {
	}
}

func age(d time.Duration) string {
	switch {
	case d < 0:
		return "now"
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + " s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min"
	default:
		return strconv.Itoa(int(d/time.Hour)) + " h " + strconv.Itoa(int(d%time.Hour/time.Minute)) + " min"
	}
}
