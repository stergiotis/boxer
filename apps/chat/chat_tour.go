// Demo-registry enrollment for chat (ADR-0057): the window over a short
// seeded transcript and no model, without window chrome, so the screenshot
// driver and the design-system review tier see it. The window is not
// mounted — no host, no bus, no coordinator — so the composer's Send has no
// client to send to and the dock's other tabs say what they lack. The
// conversation's id and the messages' times are pinned, so the capture is
// stable on one machine; the times are drawn in the local zone.

package chat

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// tourStartMs is the transcript's first message, a fixed instant.
const tourStartMs int64 = 1_760_000_000_000

// tourEntries is the seeded transcript: a question and its answer, then a
// question the model answered after a tool call, whose reply carries a SQL
// block so the code block's actions show.
var tourEntries = []entry{
	{speaker: speakerUser, atMs: tourStartMs,
		text: "Which tables does play read for its history pane?"},
	{speaker: speakerModel, atMs: tourStartMs + 4_000,
		text: "Play's **History** pane reads the host's call record: `keelson('llm_calls')` for model calls and " +
			"the trail views beside `boxer.facts` for everything the host recorded. Nothing is kept in the window."},
	{speaker: speakerUser, atMs: tourStartMs + 61_000,
		text: "Count the windows open right now."},
	{speaker: speakerTool, atMs: tourStartMs + 63_000,
		text: "query_windows · completed · 1 row"},
	{speaker: speakerModel, atMs: tourStartMs + 66_000,
		text: "Three windows are open. The query:\n\n```sql\nSELECT count() AS windows FROM keelson('windows')\n```\n"},
}

func init() {
	registry.Register(registry.Demo{
		Name:     "chat-transcript",
		Category: "Tools",
		Title:    icons.PhChatCenteredDots + " Chat — a seeded transcript",
		Stage:    [2]float32{1000, 720},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindUX,
		Description: "The chat window (ADR-0265) over a short seeded conversation and no model: the bar with the " +
			"conversation's id, its badges and the turn chip at answered; the transcript as markdown with a tool line " +
			"and a SQL block's actions; the composer; and the dock's other tabs saying what this host lacks.",
		Init:           tourInit,
		RenderStateful: tourRender,
		SourceFunc:     (*App).render,
	})
}

// tourInit builds an unmounted window that believes the host offered a
// model, so the composer draws, and seeds its conversation.
func tourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	inst.answered = true
	inst.model = llm.Description{Configured: true, Model: "scripted", EndpointHost: "localhost", Local: true}
	inst.turnChip = inst.newTurnChip()
	conv := inst.conv
	conv.id = "chat-tour"
	conv.keep = true
	conv.begin(tourEntries[0].text, tourEntries[0].atMs, false)
	conv.entries = append(conv.entries, tourEntries[1:]...)
	return inst
}

func tourRender(ids *c.WidgetIdStack, state any) {
	inst, ok := state.(*App)
	if !ok || inst == nil {
		return
	}
	inst.ids = ids
	_ = inst.Frame(nil)
}
