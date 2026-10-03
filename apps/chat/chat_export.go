package chat

// The conversation as a markdown document, handed to mdedit (ADR-0265
// §SD4): the title, what the window talked to and kept, then every message
// under a heading of its own — so mdedit's outline walks the conversation —
// with a failed turn's failure as a callout. The document is the
// transcript as the window shows it: the branch it is on, not the ones a
// regenerate or an edit left behind.

import (
	"context"
	"strings"
	"time"

	mdeditlaunch "github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// transcriptMeta is what the transcript's header says beside the
// conversation itself.
type transcriptMeta struct {
	model llm.Description
	loc   *time.Location
}

// transcriptMarkdown is the conversation as markdown.
func transcriptMarkdown(conv *conversation, meta transcriptMeta) (md string) {
	loc := meta.loc
	if loc == nil {
		loc = time.UTC
	}
	var b strings.Builder
	title := conv.title
	if title == "" {
		title = "Chat"
	}
	b.WriteString("# " + title + "\n\n")
	if meta.model.Configured {
		b.WriteString("- **Model:** " + meta.model.Model + " at " + meta.model.EndpointHost + "\n")
	}
	if conv.startedAt > 0 {
		b.WriteString("- **Started:** " + time.UnixMilli(conv.startedAt).In(loc).Format("2006-01-02 15:04 MST") + "\n")
	}
	label, _ := keepBadge(conv)
	if conv.notKept != "" {
		label += " (" + conv.notKept + ")"
	}
	b.WriteString("- **Kept:** " + label + "\n")
	b.WriteString("- **Conversation:** `" + conv.id + "`\n")
	if conv.apps {
		b.WriteString("- **Apps:** on\n")
	}
	inTools := false
	for _, e := range conv.entries {
		at := time.UnixMilli(e.atMs).In(loc).Format("15:04")
		if e.speaker == speakerTool {
			if !inTools {
				b.WriteString("\n*Tool calls:*\n\n")
				inTools = true
			}
			b.WriteString("- " + e.text + "\n")
			continue
		}
		inTools = false
		who := "You"
		if e.speaker == speakerModel {
			who = "Model"
		}
		head := "\n## " + who + " · " + at
		if e.edited {
			head += " (edited)"
		}
		b.WriteString(head + "\n\n")
		b.WriteString(strings.TrimRight(e.text, "\n") + "\n")
		if e.failed {
			b.WriteString("\n> [!failure] Not answered: " + oneLine(e.reason) + "\n")
			for _, l := range failureLines(&e) {
				b.WriteString("> " + oneLine(l) + "\n")
			}
		}
	}
	return b.String()
}

// oneLine keeps a line inside the callout it is written into.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// openInMdedit hands the conversation to a new mdedit window, which holds
// it as a document of its own, unsaved.
func (inst *App) openInMdedit() {
	conv := inst.conv
	if len(conv.entries) == 0 {
		inst.setNote("nothing to open in mdedit yet", true)
		return
	}
	l := mdeditlaunch.MdeditLaunch{Text: transcriptMarkdown(conv, transcriptMeta{model: inst.model, loc: time.Local}), Name: conv.title}
	bus := inst.bus
	inst.runAction("chat-open-mdedit", "open the conversation in mdedit", func(ctx context.Context) (note string, err error) {
		cfg, err := buscodec.Encode(l)
		if err != nil {
			err = eh.Errorf("encode the mdedit launch config: %w", err)
			return
		}
		if _, err = windowhost.RequestOpen(bus, mdeditlaunch.AppId, mdeditlaunch.Kind, cfg); err != nil {
			err = eh.Errorf("open mdedit: %w", err)
			return
		}
		return "opened the conversation in mdedit", nil
	})
}
