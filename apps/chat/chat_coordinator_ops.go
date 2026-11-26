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
			Then: "add anything else your plan still needs to the same request, then call " + op + " in window " + strconv.FormatUint(window, 10) + " again"}
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
	Name      string   `json:"name"`
	Summary   string   `json:"summary"`
	Class     string   `json:"class"`
	Effect    string   `json:"effect"`
	Reads     []string `json:"reads,omitempty"`
	Writes    []string `json:"writes,omitempty"`
	Refs      []string `json:"refs,omitempty"`
	Follows   []string `json:"follows,omitempty"`
	Untrusted bool     `json:"untrusted,omitempty"`
	Gesture   string   `json:"gesture,omitempty"`
	Consent   string   `json:"consent,omitempty"`
	// Tool is the operation's direct tool, which calls it without a window.
	Tool         string         `json:"tool,omitempty"`
	ArgsSchema   jsontext.Value `json:"args_schema,omitempty"`
	ResultSchema jsontext.Value `json:"result_schema,omitempty"`
	// LinkExamples spell the operation as links a person can click
	// (ADR-0409, proposed); make_link mints one that runs under the grant.
	LinkExamples []string `json:"link_examples,omitempty"`
}

type describedResource struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
}

type describedApp struct {
	App     string `json:"app"`
	Display string `json:"display"`
	Summary string `json:"summary,omitempty"`
	// WhenToUse is the app's advice on when to use it.
	WhenToUse string `json:"when_to_use,omitempty"`
	// Help says read_help has documentation for the app.
	Help       bool                 `json:"help,omitempty"`
	Resources  []describedResource  `json:"resources,omitempty"`
	Operations []describedOperation `json:"operations"`
}

// The detail describe_app reads at: the apps and the names of their
// operations, or every operation in full.
const (
	describeDetailNames = "names"
	describeDetailFull  = "full"
)

// describedAppNames is an app as describe_app lists it by name: what the
// app is, and its operations by name.
type describedAppNames struct {
	App       string `json:"app"`
	Display   string `json:"display"`
	Summary   string `json:"summary,omitempty"`
	WhenToUse string `json:"when_to_use,omitempty"`
	Help      bool   `json:"help,omitempty"`
	// Tools are the app's direct tools, which call an operation without a
	// window.
	Tools      []string `json:"tools,omitempty"`
	Operations []string `json:"operations"`
}

// describeNames is what the model reads of describe_app by name: each app
// with its operations' names and its direct tools among directs.
func describeNames(apps []agent.AppOperations, directs []directOp) (out []describedAppNames) {
	out = make([]describedAppNames, 0, len(apps))
	for _, a := range apps {
		d := describedAppNames{App: a.App, Display: a.Display, Summary: a.Summary, WhenToUse: a.WhenToUse, Help: a.Help, Operations: make([]string, 0, len(a.Operations))}
		for _, o := range a.Operations {
			d.Operations = append(d.Operations, o.Name)
			if t := directToolFor(directs, a.App, o.Name); t != "" {
				d.Tools = append(d.Tools, t)
			}
		}
		out = append(out, d)
	}
	return
}

// describeView is what the model reads of describe_app: the schemas as JSON
// objects rather than text holding JSON, and each direct operation's tool
// among directs.
func describeView(apps []agent.AppOperations, directs []directOp) (out []describedApp) {
	out = make([]describedApp, 0, len(apps))
	for _, a := range apps {
		d := describedApp{App: a.App, Display: a.Display, Summary: a.Summary, WhenToUse: a.WhenToUse, Help: a.Help, Operations: make([]describedOperation, 0, len(a.Operations))}
		for _, r := range a.Resources {
			d.Resources = append(d.Resources, describedResource{Name: r.Name, Summary: r.Summary})
		}
		for _, o := range a.Operations {
			d.Operations = append(d.Operations, describedOperation{Name: o.Name, Summary: o.Summary, Class: o.Class, Effect: o.Effect,
				Reads: o.Reads, Writes: o.Writes, Refs: o.Refs, Follows: o.Follows, Untrusted: o.Untrusted, Gesture: o.Gesture, Consent: o.Consent,
				Tool: directToolFor(directs, a.App, o.Name), ArgsSchema: schemaValue(o.ArgsSchema), ResultSchema: schemaValue(o.ResultSchema),
				LinkExamples: o.LinkExamples})
		}
		out = append(out, d)
	}
	return
}

// directToolFor is the direct tool of op in appId among directs, empty for
// none.
func directToolFor(directs []directOp, appId string, op string) (name string) {
	for _, d := range directs {
		if d.app == appId && d.op == op {
			return d.name
		}
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
	inst.mu.Lock()
	for _, w := range windows {
		inst.windowApps[w.Instance] = w.App
	}
	inst.mu.Unlock()
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
	inst.noteEffects([]agent.AppOperations{{App: appId, Operations: ops}})
	return
}

// noteEffects keeps the declared effect class of every operation a
// describe_app answer names, by app, for effectOf.
func (inst *coordinator) noteEffects(apps []agent.AppOperations) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, a := range apps {
		m := inst.opEffects[a.App]
		if m == nil {
			m = make(map[string]string)
			inst.opEffects[a.App] = m
		}
		for _, o := range a.Operations {
			if o.Effect != "" {
				m[o.Name] = o.Effect
			}
		}
	}
}

// takesArgs reports whether op in window takes arguments: a refusal that
// names the schema they have to fit says so, otherwise the operation's
// schema does. An operation it cannot look up takes none.
func (inst *coordinator) takesArgs(ctx context.Context, window uint64, op string, remedy *agent.Remedy) (takes bool) {
	if remedy != nil && remedy.ArgsSchema != "" {
		return true
	}
	inst.mu.Lock()
	appId := inst.windowApps[window]
	inst.mu.Unlock()
	if appId == "" {
		return
	}
	for _, o := range inst.operationsOf(ctx, appId) {
		if o.Name == op {
			return schemaTakesArgs(o.ArgsSchema)
		}
	}
	return
}

// schemaTakesArgs reports whether an object schema declares any property.
func schemaTakesArgs(argsSchema string) (takes bool) {
	var s struct {
		Properties map[string]jsontext.Value `json:"properties"`
	}
	if json.Unmarshal([]byte(argsSchema), &s) != nil {
		return
	}
	return len(s.Properties) > 0
}

// withReason is an operation's argument schema with the one-line reason
// and the title every call may carry; titled is false when the operation
// has a title argument of its own, which then stays the operation's.
func withReason(argsSchema string) (v jsontext.Value, titled bool) {
	return withProperties(argsSchema, map[string]any{"reason": map[string]any{"type": "string", "description": "one line, shown to the person"},
		"title": titleProperty})
}

// withProperties adds properties to an object schema, never over one the
// schema has; titled says whether title was among those added.
func withProperties(argsSchema string, add map[string]any) (v jsontext.Value, titled bool) {
	v, added := withPropertiesAdded(argsSchema, add)
	return v, added["title"]
}

// withPropertiesAdded is withProperties saying which of the properties it
// added.
func withPropertiesAdded(argsSchema string, add map[string]any) (v jsontext.Value, added map[string]bool) {
	added = make(map[string]bool, len(add))
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
		added[k] = true
	}
	s["properties"] = props
	b, err := json.Marshal(s, json.Deterministic(true))
	if err != nil {
		return schema(`{"type":"object"}`), map[string]bool{}
	}
	return jsontext.Value(b), added
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
