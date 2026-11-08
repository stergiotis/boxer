package chat

import (
	"context"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
)

// chat_desktop_note.go: where the model stands, told at the start of a turn
// when it changed. The apps note names the apps it can work with, one line
// each; the desktop note says whether it has a task and which windows the
// task holds. Together they answer what the first calls of a conversation
// otherwise ask, without every app's whole catalog of operations entering
// the conversation.

// appsNote is the apps the model can work with, by id, name and summary.
func (inst *coordinator) appsNote(ctx context.Context) (note string) {
	if !inst.appsOn() {
		return
	}
	apps, err := inst.cli.Describe(ctx, agent.DescribeRequest{})
	if err != nil || len(apps) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("The apps you can work with, as id — name: what it does. \"help\" marks an app whose documentation read_help reads; \"no operations\" an app that opens but cannot be operated. Name an app by its id; describe_app with the app lists its operations.\n")
	for _, a := range apps {
		b.WriteString("- " + a.App + " — " + a.Display + ": " + a.Summary)
		switch {
		case a.Help && len(a.Operations) == 0:
			b.WriteString(" (help; no operations)")
		case a.Help:
			b.WriteString(" (help)")
		case len(a.Operations) == 0:
			b.WriteString(" (no operations)")
		}
		b.WriteByte('\n')
	}
	text := strings.TrimRight(b.String(), "\n")
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.appsTold == text {
		return
	}
	inst.appsTold = text
	return text
}

// desktopNote is the model's task and the windows it holds, by window,
// app, mode and load. Titles are left out: they are the apps' and the
// person's text, which a host's note does not carry.
func (inst *coordinator) desktopNote(ctx context.Context) (note string) {
	if !inst.appsOn() {
		return
	}
	var text string
	h := inst.handle()
	if h == "" {
		text = "You have no task yet, so no window is shared with you: request_access starts a task, and list_windows and the windows' operations answer once it has."
	} else {
		windows, err := inst.cli.List(ctx, h)
		if err != nil {
			return
		}
		if len(windows) == 0 {
			text = "Your task holds no window yet: open_window opens one of an app the grant names."
		} else {
			var b strings.Builder
			b.WriteString("Your task's windows, as window — app, mode, load:\n")
			for _, w := range windows {
				inst.noteWindowApp(w.Instance, w.App)
				b.WriteString("- " + strconv.FormatUint(w.Instance, 10) + " — " + w.App + ", " + w.Mode + ", " + w.Load)
				if w.Confined {
					b.WriteString(", sealed data")
				}
				b.WriteByte('\n')
			}
			text = strings.TrimRight(b.String(), "\n")
		}
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.deskTold == text {
		return
	}
	inst.deskTold = text
	return text
}

// appsOn reports whether the conversation offers Apps: without them the
// model has no windows to stand among.
func (inst *coordinator) appsOn() (on bool) {
	on, _ = inst.offers()
	return
}
