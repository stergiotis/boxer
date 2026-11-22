package chat

import (
	"cmp"
	"context"
	"slices"
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

// appsNote is the apps the model can work with, by id, name and summary,
// each with the tools that reach it directly and its advice on when to use
// it; the apps that give advice come first, in the order the host lists
// them. An app with no operations gives none: it cannot be used as advised.
func (inst *coordinator) appsNote(ctx context.Context) (note string) {
	if !inst.appsOn() {
		return
	}
	apps, err := inst.cli.Describe(ctx, agent.DescribeRequest{})
	if err != nil || len(apps) == 0 {
		return
	}
	directs := inst.directOps(ctx)
	slices.SortStableFunc(apps, func(a, b agent.AppOperations) int {
		return cmp.Compare(boolRank(!advises(a)), boolRank(!advises(b)))
	})
	var b strings.Builder
	b.WriteString("The apps you can work with, as id — name: what it does, and when to use it where the app says. \"help\" marks an app whose documentation read_help reads; \"no operations\" an app that opens but cannot be operated; \"tool\" names a tool of yours that calls the app's operation without a window or request_access first. Name an app by its id; describe_app with the app lists its operations.\n")
	for _, a := range apps {
		b.WriteString("- " + a.App + " — " + a.Display + ": " + a.Summary)
		var marks []string
		if a.Help {
			marks = append(marks, "help")
		}
		if len(a.Operations) == 0 {
			marks = append(marks, "no operations")
		}
		for _, t := range directToolsOf(directs, a.App) {
			if lookupTool(t) == nil {
				marks = append(marks, "tool "+t)
			}
		}
		if len(marks) > 0 {
			b.WriteString(" (" + strings.Join(marks, "; ") + ")")
		}
		if advises(a) {
			b.WriteString(". " + a.WhenToUse)
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

// advises reports whether the apps note gives an app's advice on when to
// use it: the app gives some and has operations to use.
func advises(a agent.AppOperations) bool {
	return a.WhenToUse != "" && len(a.Operations) > 0
}

// boolRank orders false before true.
func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
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
