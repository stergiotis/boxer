package chat

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
)

// The coordinator's annotation tools (ADR-0297): annotate puts a mark on the
// overlay the host draws above every window, clear_annotations removes the
// task's. A target is a part of a window tree the model read — resolved
// here, by code, to its window and a rect relative to it (§SD4) — or a whole
// window. The model never sends a coordinate.

func (inst *coordinator) annotate(ctx context.Context, asked agent.Asked, args map[string]any) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "error: no task yet; call request_access first", "annotate: no task"
	}
	str := func(k string) (s string) { s, _ = args[k].(string); return }
	raw, _ := args["targets"].([]any)
	targets := make([]agent.Target, 0, len(raw))
	for i, r := range raw {
		m, _ := r.(map[string]any)
		tree, _ := m["tree"].(string)
		node, _ := m["node"].(string)
		window, _ := m["window"].(float64)
		switch {
		case tree != "" || node != "":
			w, local, err := inst.treeAnchor(tree, node)
			if err != nil {
				return "error: target " + strconv.Itoa(i+1) + ": " + err.Error(), "annotate: " + err.Error()
			}
			targets = append(targets, agent.Target{Window: w, Rect: &local})
		case window > 0:
			targets = append(targets, agent.Target{Window: uint64(window)})
		default:
			return "error: target " + strconv.Itoa(i+1) + " names a tree part (tree and node) or a window",
				"annotate: a target names nothing"
		}
	}
	op := str("op")
	out, err := inst.cli.Annotate(ctx, agent.AnnotateRequest{Handle: h, Asked: asked, Id: str("id"), Op: op,
		Targets: targets, Text: str("text")})
	activity = op + " " + strconv.Quote(str("id"))
	if err != nil {
		inst.refuse(err.Error())
		return "error: " + err.Error(), activity + ": " + err.Error()
	}
	b, _ := json.Marshal(callOutcome{Phase: out.Phase, Reason: out.Reason})
	content = string(b)
	if out.Phase != "completed" {
		inst.refuse(out.Reason)
		return content, activity + ": " + out.Phase
	}
	return content, activity
}

func (inst *coordinator) clearAnnotations(ctx context.Context, asked agent.Asked, id string) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "error: no task yet", "clear annotations: no task"
	}
	out, err := inst.cli.ClearAnnotations(ctx, h, asked, id)
	if err != nil {
		return "error: " + err.Error(), "clear annotations: " + err.Error()
	}
	b, _ := json.Marshal(callOutcome{Phase: out.Phase, Reason: out.Reason})
	return string(b), "clear annotations: " + out.Reason
}
