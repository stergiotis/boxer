package chat

import (
	"context"
	"encoding/json/v2"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
)

// The coordinator's link tools (ADR-0409 (proposed) §SD6): make_link asks
// the dispatcher to check a call and the host to mint a link that runs it
// under the task's grant when the person clicks it; read_link reads a
// link the model met into what it names, so the model makes the calls
// itself, under its own grant — a link's text widens nothing.

// linkDescriber reads a link as the host does; nil when the window has no
// bus.
type linkDescriber func(ctx context.Context, link string) (rep applink.DescribeReply, err error)

// makeLink runs make_link.
func (inst *coordinator) makeLink(ctx context.Context, o toolOrigin, toolCall string, args map[string]any) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "error: no task yet; call request_access first", "make_link: no task"
	}
	var js []byte
	if a, ok := args["args"]; ok && a != nil {
		js, _ = json.Marshal(a)
	}
	inst.mu.Lock()
	tier := inst.curTier
	inst.mu.Unlock()
	target := argString(args, "target")
	link, err := inst.cli.MakeLink(ctx, agent.MakeLinkRequest{
		CallRequest: agent.CallRequest{Handle: h, Key: o.key(), Turn: o.turn, ModelCall: o.modelCall, ToolCall: toolCall,
			ToolIndex: uint32(max(o.index, 0)), Tier: uint8(tier), Operation: argString(args, "operation"), Args: string(js)},
		Target: target, App: argString(args, "app"), Window: uint64(argFloat(args, "window"))})
	if err != nil {
		inst.refuse(err.Error())
		return "error: " + err.Error(), "make_link: " + err.Error()
	}
	label := argString(args, "label")
	if label == "" {
		label = "open it"
	}
	out := struct {
		Link     string `json:"link"`
		Markdown string `json:"markdown"`
		Note     string `json:"note"`
	}{Link: link, Markdown: "[" + escapeLinkLabel(label) + "](" + link + ")",
		Note: "write markdown into your reply or the artefact; when the person clicks it, the call runs under this task's grant, checked again then. " +
			"Once the task has ended, a click is the person's own and changes only what a link someone else wrote may. The link lasts as long as this session."}
	b, _ := json.Marshal(out)
	return string(b), "made a link"
}

// escapeLinkLabel keeps a label's brackets from closing the markdown link
// it names.
func escapeLinkLabel(s string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]").Replace(s)
}

// readLink runs read_link.
func (inst *coordinator) readLink(ctx context.Context, args map[string]any) (content string, activity string) {
	if inst.describeLink == nil {
		return "error: this window cannot read links", "read_link: no bus"
	}
	rep, err := inst.describeLink(ctx, argString(args, "link"))
	if err != nil {
		return "error: " + err.Error(), "read_link: " + err.Error()
	}
	out := struct {
		App       string `json:"app,omitempty"`
		Operation string `json:"operation,omitempty"`
		Summary   string `json:"summary,omitempty"`
		Effect    string `json:"effect,omitempty"`
		Target    string `json:"target,omitempty"`
		Args      string `json:"args,omitempty"`
		Author    string `json:"author,omitempty"`
		Refused   string `json:"refused,omitempty"`
		Next      string `json:"next"`
	}{App: rep.App, Operation: rep.Operation, Summary: rep.Summary, Effect: rep.Effect, Target: rep.Target, Args: rep.Args,
		Author: rep.Author, Refused: rep.Refused,
		Next: "a link grants nothing: to do what it names, open or find the window and call the operation yourself, under your own grant"}
	b, _ := json.Marshal(out)
	return string(b), "read a link"
}

func argFloat(args map[string]any, name string) (f float64) {
	f, _ = args[name].(float64)
	return
}
