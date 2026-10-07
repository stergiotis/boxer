package chat

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"regexp"
	"strconv"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// OperationToolsSeed offers each operation of a window in the task as a
// tool of its own, typed by the operation's argument schema, beside the
// fixed tools.
var OperationToolsSeed = env.NewBool(env.Spec{
	Name:        "BOXER_CHAT_OPERATION_TOOLS",
	Default:     "false",
	Description: "offer each operation of a window in the chat's task as its own typed tool, w<window>_<operation>, beside call_operation; for trials",
	Category:    env.CategoryE("boxer-chat"),
})

// nextStep is what the model can do about a refusal: a tool to call, with
// the arguments that would let the refused call through, or the schema its
// arguments have to fit.
type nextStep struct {
	Tool       string         `json:"tool"`
	Args       map[string]any `json:"args,omitempty"`
	ArgsSchema jsontext.Value `json:"args_schema,omitempty"`
	Then       string         `json:"then,omitempty"`
}

// nextFor turns a refusal's remedy into the step the model takes; nil when
// the refusal carries none.
func (inst *coordinator) nextFor(window uint64, op string, r *agent.Remedy) (n *nextStep) {
	if r == nil {
		return
	}
	switch {
	case len(r.Destinations) > 0:
		n = &nextStep{Tool: "request_access", Args: map[string]any{"plan": "let " + op + " reach what it needs", "destinations": r.Destinations},
			Then: "call " + op + " in window " + strconv.FormatUint(window, 10) + " again"}
	case r.ArgsSchema != "":
		n = &nextStep{Tool: inst.toolFor(window, op), ArgsSchema: schemaValue(r.ArgsSchema)}
		if n.Tool == "call_operation" {
			n.Then = "put arguments that fit args_schema under args"
		}
	}
	return
}

// schemaValue is a schema as a JSON value for the model, or nothing when
// the text is not JSON.
func schemaValue(s string) (v jsontext.Value) {
	if v = jsontext.Value(s); !v.IsValid() {
		v = nil
	}
	return
}

type describedOperation struct {
	Name         string         `json:"name"`
	Summary      string         `json:"summary"`
	Class        string         `json:"class"`
	Effect       string         `json:"effect"`
	Reads        []string       `json:"reads,omitempty"`
	Writes       []string       `json:"writes,omitempty"`
	Refs         []string       `json:"refs,omitempty"`
	Follows      []string       `json:"follows,omitempty"`
	Untrusted    bool           `json:"untrusted,omitempty"`
	Gesture      string         `json:"gesture,omitempty"`
	Consent      string         `json:"consent,omitempty"`
	ArgsSchema   jsontext.Value `json:"args_schema,omitempty"`
	ResultSchema jsontext.Value `json:"result_schema,omitempty"`
}

type describedResource struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
}

type describedApp struct {
	App     string `json:"app"`
	Display string `json:"display"`
	Summary string `json:"summary,omitempty"`
	// Help says read_help has documentation for the app.
	Help       bool                 `json:"help,omitempty"`
	Resources  []describedResource  `json:"resources,omitempty"`
	Operations []describedOperation `json:"operations"`
}

// describeView is what the model reads of describe_app: the schemas as JSON
// objects rather than text holding JSON.
func describeView(apps []agent.AppOperations) (out []describedApp) {
	out = make([]describedApp, 0, len(apps))
	for _, a := range apps {
		d := describedApp{App: a.App, Display: a.Display, Summary: a.Summary, Help: a.Help, Operations: make([]describedOperation, 0, len(a.Operations))}
		for _, r := range a.Resources {
			d.Resources = append(d.Resources, describedResource{Name: r.Name, Summary: r.Summary})
		}
		for _, o := range a.Operations {
			d.Operations = append(d.Operations, describedOperation{Name: o.Name, Summary: o.Summary, Class: o.Class, Effect: o.Effect,
				Reads: o.Reads, Writes: o.Writes, Refs: o.Refs, Follows: o.Follows, Untrusted: o.Untrusted, Gesture: o.Gesture, Consent: o.Consent,
				ArgsSchema: schemaValue(o.ArgsSchema), ResultSchema: schemaValue(o.ResultSchema)})
		}
		out = append(out, d)
	}
	return
}

// typedOp is the operation an operation tool stands for.
type typedOp struct {
	window uint64
	op     string
	// titled says the tool takes a title: false when the operation has an
	// argument of that name, which it keeps.
	titled bool
}

var toolNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// operationToolName names the tool for one operation in one window.
func operationToolName(window uint64, op string) (name string) {
	name = "w" + strconv.FormatUint(window, 10) + "_" + toolNameUnsafe.ReplaceAllString(op, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return
}

// toolFor is the tool the model calls an operation through: its own when
// operation tools are on and the window is in the task, call_operation
// otherwise.
func (inst *coordinator) toolFor(window uint64, op string) (tool string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	name := operationToolName(window, op)
	if t, ok := inst.typed[name]; ok && t.window == window && t.op == op {
		return name
	}
	return "call_operation"
}

// operationTools are the typed tools of the task's windows, rebuilt from
// list_windows on each model call; the operations' schemas are read once per
// app and operation.
func (inst *coordinator) operationTools(ctx context.Context) (out []openaichat.Tool) {
	h := inst.handle()
	if h == "" {
		return
	}
	windows, err := inst.cli.List(ctx, h)
	if err != nil {
		return
	}
	typed := make(map[string]typedOp)
	for _, w := range windows {
		if !w.Ops || w.Load == "opening" || w.Load == "failed" {
			continue
		}
		for _, o := range inst.operationsOf(ctx, w.App) {
			name := operationToolName(w.Instance, o.Name)
			params, titled := withReason(o.ArgsSchema)
			typed[name] = typedOp{window: w.Instance, op: o.Name, titled: titled}
			out = append(out, openaichat.Tool{Name: name, Description: "Window " + strconv.FormatUint(w.Instance, 10) + ": " + o.Summary + ".",
				Parameters: params})
		}
	}
	inst.mu.Lock()
	inst.typed = typed
	inst.mu.Unlock()
	return
}

// operationsOf is an app's operations with their schemas, from the cache or
// from describe_app.
func (inst *coordinator) operationsOf(ctx context.Context, appId string) (ops []agent.Operation) {
	inst.mu.Lock()
	ops, cached := inst.opCache[appId]
	inst.mu.Unlock()
	if cached {
		return
	}
	apps, err := inst.cli.Describe(ctx, agent.DescribeRequest{App: appId})
	if err != nil || len(apps) == 0 {
		return
	}
	for _, o := range apps[0].Operations {
		if full, ferr := inst.cli.Describe(ctx, agent.DescribeRequest{App: appId, Operation: o.Name}); ferr == nil && len(full) > 0 && len(full[0].Operations) > 0 {
			o = full[0].Operations[0]
		}
		ops = append(ops, o)
	}
	inst.mu.Lock()
	inst.opCache[appId] = ops
	inst.mu.Unlock()
	return
}

// withReason is an operation's argument schema with the one-line reason
// and the title every call may carry; titled is false when the operation
// has a title argument of its own, which then stays the operation's.
func withReason(argsSchema string) (v jsontext.Value, titled bool) {
	return withProperties(argsSchema, map[string]any{"reason": map[string]any{"type": "string", "description": "one line, shown to the person"},
		"title": titleProperty})
}

// withProperties adds properties to an object schema, never over one the
// schema has; added says whether title was among those added.
func withProperties(argsSchema string, add map[string]any) (v jsontext.Value, titled bool) {
	s := map[string]any{}
	if argsSchema != "" {
		_ = json.Unmarshal([]byte(argsSchema), &s)
	}
	if s["type"] == nil {
		s["type"] = "object"
	}
	props, _ := s["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		if argsSchema == "" {
			s["additionalProperties"] = false
		}
	}
	for k, p := range add {
		if _, has := props[k]; has {
			continue
		}
		props[k] = p
		if k == "title" {
			titled = true
		}
	}
	s["properties"] = props
	b, err := json.Marshal(s, json.Deterministic(true))
	if err != nil {
		return schema(`{"type":"object"}`), false
	}
	return jsontext.Value(b), titled
}

type helpSection struct {
	Slug    string `json:"slug"`
	Heading string `json:"heading"`
	Level   uint8  `json:"level"`
}

type helpDoc struct {
	App      string        `json:"app"`
	Doc      string        `json:"doc"`
	Title    string        `json:"title"`
	Sections []helpSection `json:"sections,omitempty"`
}

type helpHit struct {
	App     string `json:"app"`
	Doc     string `json:"doc"`
	Section string `json:"section,omitempty"`
	Title   string `json:"title"`
	Heading string `json:"heading,omitempty"`
	Context string `json:"context,omitempty"`
}

type helpRead struct {
	Docs      []helpDoc     `json:"docs,omitempty"`
	Hits      []helpHit     `json:"hits,omitempty"`
	Text      string        `json:"text,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
	Sections  []helpSection `json:"sections,omitempty"`
	Then      string        `json:"then,omitempty"`
}

func helpSectionsOf(in []agent.HelpSection) (out []helpSection) {
	for _, s := range in {
		out = append(out, helpSection{Slug: s.Slug, Heading: s.Heading, Level: s.Level})
	}
	return
}

// readHelp is what the model reads of read_help. Help is the apps' own
// documentation, so it is not delimited as untrusted.
func (inst *coordinator) readHelp(ctx context.Context, r agent.HelpRequest) (content string, activity string) {
	got, err := inst.cli.Help(ctx, r)
	if err != nil {
		inst.refuse(err.Error())
		return "error: " + err.Error(), "help: " + err.Error()
	}
	out := helpRead{Text: got.Text, Truncated: got.Truncated, Sections: helpSectionsOf(got.Sections)}
	for _, d := range got.Docs {
		out.Docs = append(out.Docs, helpDoc{App: d.App, Doc: d.Doc, Title: d.Title, Sections: helpSectionsOf(d.Sections)})
	}
	for _, h := range got.Hits {
		out.Hits = append(out.Hits, helpHit(h))
	}
	switch {
	case got.Truncated:
		out.Then = "the text was cut; read one of sections for the rest"
	case len(out.Hits) > 0:
		out.Then = "read a hit with read_help and its app, doc and section"
	}
	b, _ := json.Marshal(out)
	content = string(b)
	switch {
	case len(got.Hits) > 0:
		activity = "searched help: " + strconv.Itoa(len(got.Hits)) + " section(s)"
	case len(got.Docs) > 0:
		activity = "listed help: " + strconv.Itoa(len(got.Docs)) + " document(s)"
	default:
		activity = "read help " + r.Doc
		if r.Section != "" {
			activity += "#" + r.Section
		}
	}
	return
}
