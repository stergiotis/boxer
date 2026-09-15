package widgets

import (
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/chatview"
)

// Two registrations so the screenshot tour captures both layouts whole
// (ADR-0239): the dialogue, with a viewer picked over a two-party model, and
// the group, over a three-party one with nobody as the viewer.
func init() {
	registry.Register(registry.Demo{
		Name: "chatview-dialogue", Category: "Layout & widgets", Title: icons.PhChatCircle + " chat: dialogue",
		Stage:       [2]float32{880, 1000},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "A two-party conversation laid out like a phone's SMS view: the viewer's bubbles on the right, the other party's on the left, no names. Consecutive messages of one sender cluster (the tail corner marks the cluster's last), a day separator marks the date change, a system line sits centred, a reply carries a quote strip above it (click to jump), an edited message says so on hover, a deleted one shows a placeholder, and reactions are pills under the bubble with the reactors on hover. The view follows the tail until you scroll up; \"newest\" pins it again. Click a bubble to select it.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &chatDemoState{model: chatDemoModel(false), viewer: 0}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoChatview(ids, state.(*chatDemoState))
		},
		SourceFunc: demoChatview,
	})
	registry.Register(registry.Demo{
		Name: "chatview-group", Category: "Layout & widgets", Title: icons.PhChatCircle + " chat: group",
		Stage:       [2]float32{880, 1000},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "The same widget over a three-party conversation with nobody as the viewer: every bubble on the left under its sender's name in the sender's colour, with an initials disc beside each cluster's first message. Pick a viewer in the toggle row to move one party's bubbles to the right.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			return &chatDemoState{model: chatDemoModel(true), viewer: -1}
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoChatview(ids, state.(*chatDemoState))
		},
		SourceFunc: demoChatview,
	})
}

type chatDemoState struct {
	model  *chatview.Model
	state  chatview.State
	viewer int32
	last   chatview.Result
}

// chatDemoModel is a fixed conversation — every edge the widget draws — over
// two parties, or three when group is set.
func chatDemoModel(group bool) *chatview.Model {
	base := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC).UnixMilli()
	const minute = int64(60 * 1000)
	m := &chatview.Model{
		Participants: []chatview.Participant{{Name: "Ada Lovelace"}, {Name: "Bob"}},
	}
	add := func(offset int64, sender int32, body string, reply int32, flags chatview.FlagsE, status chatview.StatusE) {
		m.TimeMS = append(m.TimeMS, base+offset)
		m.Sender = append(m.Sender, sender)
		m.Body = append(m.Body, body)
		m.ReplyTo = append(m.ReplyTo, reply)
		m.Flags = append(m.Flags, flags)
		m.Status = append(m.Status, status)
		m.EditedMS = append(m.EditedMS, 0)
	}
	add(0, 0, "Morning! Did the ingest finish overnight?", -1, 0, chatview.StatusRead)
	add(40*1000, 0, "The dashboard still shows yesterday's numbers, and the on-call channel is quiet.", -1, 0, chatview.StatusRead)
	add(4*minute+10*1000, 1, "It finished at 03:12 — the view was cached.", 1, chatview.FlagEdited, 0)
	m.EditedMS[len(m.EditedMS)-1] = base + 5*minute
	add(4*minute+30*1000, 1, "Refreshing it now.", -1, 0, 0)
	if group {
		m.Participants = append(m.Participants, chatview.Participant{Name: "Cy"})
		add(12*minute, -1, "Cy joined the conversation", -1, chatview.FlagSystem, 0)
		add(13*minute+15*1000, 2, "Here is what it looks like after the refresh — the gap closed.", -1, 0, 0)
	} else {
		add(12*minute, -1, "Messages are end-to-end encrypted.", -1, chatview.FlagSystem, 0)
	}
	add(20*minute, 0, "oops wrong chat", -1, chatview.FlagDeleted, chatview.StatusSent)
	add(24*60*minute-30*minute, 0, "Thanks — closing the ticket.", 2, 0, chatview.StatusDelivered)
	add(24*60*minute-29*minute, 1, "👍", -1, 0, 0)

	// Reactions on the second message, and one on the reply.
	n := m.Len()
	m.ReactionOff = make([]int32, n+1)
	for i := 1; i <= n; i++ {
		switch i - 1 {
		case 1:
			m.ReactionKey = append(m.ReactionKey, "👍", "❤️")
			m.ReactionCount = append(m.ReactionCount, 2, 1)
			m.ReactionWho = append(m.ReactionWho, "Bob, Cy", "Bob")
		case 2:
			m.ReactionKey = append(m.ReactionKey, "🎉")
			m.ReactionCount = append(m.ReactionCount, 1)
			m.ReactionWho = append(m.ReactionWho, "Ada Lovelace")
		}
		m.ReactionOff[i] = int32(len(m.ReactionKey))
	}
	return m
}

func demoChatview(ids *c.WidgetIdStack, st *chatDemoState) {
	for range c.HorizontalTop().KeepIter() {
		for rt := range c.RichTextLabel("viewer:") {
			rt.Weak()
		}
		if c.SelectableLabel(ids.PrepareStr("v-none"), st.viewer < 0, "nobody").SendResp().HasPrimaryClicked() {
			st.viewer = -1
		}
		for i, p := range st.model.Participants {
			if c.SelectableLabel(ids.PrepareSeq(uint64(0x50+i)), st.viewer == int32(i), p.Name).SendResp().HasPrimaryClicked() {
				st.viewer = int32(i)
			}
		}
		if st.last.Clicked >= 0 {
			for rt := range c.RichTextLabel("clicked #" + strconv.Itoa(int(st.last.Clicked))) {
				rt.Small().Weak()
			}
		}
	}
	st.last = chatview.Render(chatview.Input{
		Ids:      ids,
		ScopeKey: "chatview",
		Model:    st.model,
		State:    &st.state,
		Viewer:   st.viewer,
		Location: time.UTC,
	})
}
