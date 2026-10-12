package chat

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Looking and watching (ADR-0323 §SD4, §SD7, proposed). The window keeps
// what the person has seen of its conversation: the entries there were when
// it last had focus. What lands while it has none is marked "new since you
// looked" when they come back, until they leave again, and the highest level
// it reached is the roster's for this window. Watch is "tell me when this
// turn ends": the conversation's finished turn raised to highlight, which
// the window reports to the host when it ends unseen; the host's control
// counts it beside what waits on the person.

// lookState is what the person has seen of the window's conversation.
type lookState struct {
	conv string
	// to is how many entries they have seen.
	to int
	// mark is the first entry they had not seen when they came back, -1
	// for none.
	mark int
	// focused is whether the window had focus last frame.
	focused bool
	// level is the highest level of what landed unseen.
	level attention.LevelE
	// noticed is the conversation whose notice the host keeps from this
	// window, empty for none.
	noticed string
}

// syncLooked follows the window's focus and its conversation's entries. The
// render goroutine calls it once per frame.
func (inst *App) syncLooked() {
	l := &inst.looked
	n := len(inst.conv.entries)
	if l.conv != inst.conv.id {
		l.conv, l.to, l.mark, l.level = inst.conv.id, n, -1, attention.LevelUnspecified
	}
	if n < l.to {
		// Edited or regenerated: the entries shrank under the marker.
		l.to = n
		if l.mark >= n {
			l.mark = -1
		}
	}
	if inst.focused {
		if !l.focused && l.to < n {
			l.mark = inst.firstUnseen(l.to)
		}
		l.to, l.level = n, attention.LevelUnspecified
		if l.noticed != "" {
			inst.seen(l.noticed)
			l.noticed = ""
		}
	} else {
		if l.focused {
			// They left: what they saw is behind them.
			l.mark = -1
		}
		if l.to < n {
			// Marked at once, for a window seen beside the focused one.
			l.mark = inst.firstUnseen(l.to)
		}
	}
	l.focused = inst.focused
}

// firstUnseen is the first entry from i the person did not write, -1 for
// none: their own message lands with its turn, and they have seen it.
func (inst *App) firstUnseen(i int) int {
	for ; i < len(inst.conv.entries); i++ {
		if inst.conv.entries[i].speaker != speakerUser {
			return i
		}
	}
	return -1
}

// seen tells the host the person looked at conversation, off the frame.
func (inst *App) seen(conversation string) {
	if bus := inst.bus; bus != nil {
		go func() {
			if err := attention.Seen(bus, conversation); err != nil {
				inst.log.Debug().Err(err).Msg("chat: tell the host the conversation was seen")
			}
		}()
	}
}

// renderLookedMark is the transcript's row above the first entry that
// landed unseen.
func (inst *App) renderLookedMark() {
	for range c.HorizontalTop().KeepIter() {
		for rt := range c.RichTextLabel(icons.PhArrowDown + " new since you looked") {
			rt.Small().Strong()
		}
	}
}

// watchKey is the override Watch sets: the conversation's finished turns.
var watchKey = attention.Key{Kind: "turn"}

// watching is whether the person watches the conversation, as the
// window's copy of the table says.
func (inst *App) watching() bool {
	l := &inst.levels
	return l.fetched && l.conv == inst.conv.id && l.snap.Conversation[watchKey] == attention.LevelHighlight
}

// setWatch raises the conversation's finished turns to highlight, or clears
// the override. The window's copy changes at once; the host announces the
// change and the window reads the table again.
func (inst *App) setWatch(on bool) {
	bus, conv := inst.bus, inst.conv.id
	if bus == nil {
		return
	}
	level := attention.LevelUnspecified
	if on {
		level = attention.LevelHighlight
	}
	l := &inst.levels
	if l.snap.Conversation == nil {
		l.snap.Conversation = make(map[attention.Key]attention.LevelE, 1)
	} else {
		l.snap.Conversation = cloneLevels(l.snap.Conversation)
	}
	if on {
		l.snap.Conversation[watchKey] = level
	} else {
		delete(l.snap.Conversation, watchKey)
	}
	go func() {
		if err := attention.SetLevel(bus, conv, watchKey, level); err != nil {
			inst.log.Warn().Err(err).Msg("chat: the host did not take the watch")
		}
	}()
}

func cloneLevels(m map[attention.Key]attention.LevelE) (out map[attention.Key]attention.LevelE) {
	out = make(map[attention.Key]attention.LevelE, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return
}

const tipWatch = "Watch: tell me when this conversation's turn ends. Its finished turns are raised to highlight (ADR-0323), " +
	"and one that ends while you are in another window shows in the host's control, beside what waits on you. Click again to stop."

// renderWatchToggle is the Watch button, in the title's row; absent without
// a host to tell.
func (inst *App) renderWatchToggle() {
	if inst.bus == nil || !inst.levels.fetched {
		return
	}
	on := inst.watching()
	label := icons.PhBellSimple + " Watch"
	if on {
		// The word changes too: a selected small button reads much like an
		// unselected one.
		label = icons.PhBellRinging + " Watching"
	}
	for range c.HoverText(tipWatch).KeepIter() {
		if c.Button(inst.ids.PrepareStr("watch-toggle"), c.Atoms().Text(label).Keep()).Small().Selected(on).SendResp().HasPrimaryClicked() {
			inst.setWatch(!on)
		}
	}
}

// turnEnded classifies a turn that landed: while the window has no focus
// its level counts as unseen, and a highlight is reported to the host.
func (inst *App) turnEnded(text string) {
	if inst.focused {
		return
	}
	ev := attention.Event{Kind: watchKey.Kind, Conversation: inst.conv.id}
	level := inst.levels.snap.Classify(ev)
	inst.looked.level = max(inst.looked.level, level)
	if level < attention.LevelHighlight || inst.bus == nil {
		return
	}
	bus, n := inst.bus, attention.Notice{Conversation: inst.conv.id, Title: inst.conv.title, Kind: ev.Kind, Text: text}
	inst.looked.noticed = n.Conversation
	go func() {
		if _, err := attention.Notify(bus, n); err != nil {
			inst.log.Debug().Err(err).Msg("chat: report the turn's end to the host")
		}
	}()
}
