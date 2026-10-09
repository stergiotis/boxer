package chat

import (
	"context"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/capture"
)

// read_window_tree (ADR-0301): a window tree capture of windows of the
// task, read back and handed to the model as an indented outline — one line
// per message of the stream that drew a visible widget, with the widgets it
// drew, their roles, names and values. Every name is the apps' text.

// maxTreeBytes bounds the outline the model reads; a busy window's tree is
// tens of kilobytes.
const maxTreeBytes = 24 << 10

func (inst *coordinator) readWindowTree(ctx context.Context, o toolOrigin, windows []uint64) (content string, activity string) {
	if apps, _ := inst.offers(); !apps {
		return "error: a window tree needs Apps, which this conversation was started without", "window tree: no Apps"
	}
	if len(windows) == 0 {
		return "error: name the windows, as list_windows gives them", "window tree: no windows"
	}
	h := inst.handle()
	if h == "" {
		return "error: no task yet; call request_access for the windows first", "window tree: no task"
	}
	key := o.key()
	out, err := inst.cli.CaptureWith(ctx, agent.CaptureRequest{Handle: h, Instances: windows, Format: agent.CaptureFormatTree, Key: key})
	if err == nil {
		out, err = inst.settle(ctx, h, key, out)
	}
	if err != nil {
		return "error: " + err.Error(), "window tree: " + err.Error()
	}
	if out.Phase != "completed" || out.Job == "" {
		if out.Phase == "refused" || out.Phase == "denied" {
			inst.refuse(out.Reason)
		}
		why := strings.TrimSpace(out.Phase + " " + out.Reason)
		return "error: the window tree " + why, "window tree: " + why
	}
	res, err := inst.cli.Read(ctx, h, out.Job)
	if err != nil {
		return "error: the window tree could not be read: " + err.Error(), "window tree: " + err.Error()
	}
	if res.Untrusted {
		inst.markTainted()
	}
	if res.Confined {
		inst.mu.Lock()
		inst.confined = true
		inst.mu.Unlock()
	}
	t, err := capture.ParseTree(res.Data)
	if err != nil {
		return "error: " + err.Error(), "window tree: " + err.Error()
	}
	return wrapUntrusted("window tree of "+windowsText(windows), treeOutline(t, maxTreeBytes)),
		"read the window tree of " + windowsText(windows) + " (" + strconv.Itoa(len(t.Ops)) + " messages)"
}

// treeOutline renders a window tree for the model:
//
//	#3 Button [20,60 40x18] button "Save"
//
// indented two spaces per level. A message's own widgets follow on its line
// when they share its rect, else on lines of their own below it. Widgets
// with neither a role nor a name are left out; a message with nothing left
// to show keeps its line when something below it is shown. The outline is
// cut at limit bytes.
func treeOutline(t capture.Tree, limit int) (s string) {
	depth := make([]int, len(t.Ops))
	shown := make([]bool, len(t.Ops))
	for i := len(t.Ops) - 1; i >= 0; i-- {
		for _, w := range t.Ops[i].Widgets {
			if w.Role != "" || w.Name != "" {
				shown[i] = true
			}
		}
		if shown[i] && t.Ops[i].Parent >= 0 {
			shown[t.Ops[i].Parent] = true
		}
	}
	var b strings.Builder
	for i, op := range t.Ops {
		if op.Parent >= 0 {
			depth[i] = depth[op.Parent] + 1
		}
		if !shown[i] {
			continue
		}
		start := b.Len()
		pad := strings.Repeat("  ", depth[i])
		b.WriteString(pad)
		b.WriteString("#" + strconv.Itoa(i) + " " + op.Op + " " + rectText(op.Rect))
		for _, w := range op.Widgets {
			if w.Role == "" && w.Name == "" {
				continue
			}
			if w.Rect == op.Rect {
				b.WriteString(" " + widgetText(w))
				continue
			}
			b.WriteString("\n" + pad + "  - " + widgetText(w) + " " + rectText(w.Rect))
		}
		b.WriteByte('\n')
		if b.Len() > limit {
			out := b.String()[:start]
			return out + "… cut at " + strconv.Itoa(limit) + " bytes; read fewer windows"
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func widgetText(w capture.TreeWidget) (s string) {
	s = w.Role
	if w.Name != "" {
		s += " " + quoteAppText(w.Name)
	}
	if w.Value != "" {
		s += " = " + quoteAppText(w.Value)
	}
	return strings.TrimSpace(s)
}

// quoteAppText quotes the apps' text, so a name cannot close the untrusted
// fence around it.
func quoteAppText(v string) string {
	return strconv.Quote(strings.ReplaceAll(v, "<<", "< <"))
}

func rectText(r [4]float32) string {
	f := func(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) }
	return "[" + f(r[0]) + "," + f(r[1]) + " " + f(r[2]) + "x" + f(r[3]) + "]"
}
