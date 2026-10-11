package chat

import (
	"context"
	"encoding/json/jsontext"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The coordinator's tool registry (ADR-0310 §SD6 and §SD11, ADR-0315
// §SD1): every tool of the coordinator's own — the questions, the
// artefact and its screenshots, the windows — is one toolSpec, registered
// once in coordinatorTools in the order the model sees them. What the model
// is offered and which calls are taken are projections of the registry
// under a toolOffer; dispatch is a lookup. The operation tools of a task's
// windows (chat_coordinator_ops.go) are typed per window and join after
// the registry's.

// toolGroupE says when a tool is offered to the model and when a call to
// it is taken: the two differ, since a write offered only under a write
// policy is still answered — refused by the tool, with the reason — when
// the policy is read-only.
type toolGroupE uint8

const (
	// toolGroupQuestions is ask_user: with Questions on.
	toolGroupQuestions toolGroupE = iota
	// toolGroupArtefactRead reads the artefact: with an artefact.
	toolGroupArtefactRead
	// toolGroupArtefactWrite changes the artefact: offered under a write
	// policy, taken with an artefact.
	toolGroupArtefactWrite
	// toolGroupImageRead lists the screenshots: with an artefact.
	toolGroupImageRead
	// toolGroupImageCapture captures windows: offered under a write policy
	// with Apps on, taken with an artefact.
	toolGroupImageCapture
	// toolGroupImageWrite changes the screenshots: offered under a write
	// policy, taken with an artefact.
	toolGroupImageWrite
	// toolGroupView shows a screenshot's pixels: offered when the Pixels
	// setting lets them reach the model, taken with an artefact.
	toolGroupView
	// toolGroupFixed works in the task's windows: with Apps on.
	toolGroupFixed
	// toolGroupRecall reads an elided result back: once the conversation
	// was compacted (ADR-0316 §SD2).
	toolGroupRecall
)

// toolOffer is what the conversation offers at one moment, read once per
// projection.
type toolOffer struct {
	apps, questions bool
	// artefact says the conversation has one; write that the model may
	// change it; pixels that a screenshot's pixels may reach the model.
	artefact, write, pixels bool
	// compacted says the conversation's history was compacted.
	compacted bool
}

// offered says whether the group's tools are in the model's tool list.
func (inst toolGroupE) offered(s toolOffer) bool {
	switch inst {
	case toolGroupQuestions:
		return s.questions
	case toolGroupArtefactRead, toolGroupImageRead:
		return s.artefact
	case toolGroupArtefactWrite, toolGroupImageWrite:
		return s.artefact && s.write
	case toolGroupImageCapture:
		return s.artefact && s.write && s.apps
	case toolGroupView:
		return s.artefact && s.pixels
	case toolGroupFixed:
		return s.apps
	case toolGroupRecall:
		return s.compacted
	}
	return false
}

// accepted says whether a call to the group's tools is taken.
func (inst toolGroupE) accepted(s toolOffer) bool {
	switch inst {
	case toolGroupQuestions:
		return s.questions
	case toolGroupFixed:
		return s.apps
	case toolGroupRecall:
		return true
	}
	return s.artefact
}

// toolSpec is one coordinator tool: its declaration for the model, when it
// is offered, what runs it, and what bounds its result.
type toolSpec struct {
	name        string
	description string
	// schema is the parameter schema without the title every tool takes;
	// params is the schema the model sees, built once.
	schema string
	params jsontext.Value
	group  toolGroupE
	// budget bounds the bytes of the result the model reads, applied by
	// the tool itself where its cut keeps a shape (a tree outline cuts at
	// a line); zero leaves the result uncapped. call_operation declares
	// none: capping it is a visible change for the profile record.
	budget int
	// parallelSafe says the call changes nothing, so it could run beside
	// another; declared, and not read while tools run one at a time.
	parallelSafe bool
	exec         func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string)
}

// tool is the declaration the model sees.
func (inst *toolSpec) tool() openaichat.Tool {
	return openaichat.Tool{Name: inst.name, Description: inst.description, Parameters: inst.params}
}

// The pieces the artefact's schemas share.
const (
	baseRevisionProperty = `"base_revision":{"type":"integer","minimum":0,"description":"the revision you read"}`
	headingProperty      = `"heading":{"type":"array","items":{"type":"string"},"minItems":1,"description":"heading path, outermost first; the last element names the heading, the ones before its nearest ancestors"}`
	imageNameProperty    = `"name":{"type":"string","description":"lower-case letters, digits, '.', '-' and '_', ending in .png; omitted, the chat picks screenshot-<n>.png"}`
)

// maxTreeBytes bounds the outline read_window_tree hands the model; a busy
// window's tree is tens of kilobytes. maxWindowRows bounds what
// query_windows hands it, in bytes of JSON lines; a desktop has tens of
// windows, so only a statement that ignores the table's shape reaches it.
const (
	maxTreeBytes  = 24 << 10
	maxWindowRows = 16 << 10
)

func askExec(ctx context.Context, inst *coordinator, _ *toolSpec, _ toolOrigin, _ openaichat.ToolCall, args map[string]any) (content string, activity string) {
	return inst.askUser(ctx, args)
}

func artefactExec(ctx context.Context, inst *coordinator, _ *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
	return inst.artefactCall(ctx, o, call.Name, args, inst.artefactOf())
}

func imageExec(ctx context.Context, inst *coordinator, _ *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
	return inst.imageCall(ctx, o, call.Name, args, inst.artefactOf())
}

// coordinatorTools is the registry, in the order the model sees the tools.
var coordinatorTools = []toolSpec{
	{name: "ask_user", group: toolGroupQuestions,
		description: "Ask the person one to " + strconv.Itoa(maxAskQuestions) + " questions as a form of options, shown inline in the conversation. Waits for the person's answer.",
		schema: `{"type":"object","properties":{"questions":{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxAskQuestions) + `,"items":{"type":"object","properties":{` +
			`"question":{"type":"string","description":"the question, a full sentence ending in a question mark"},` +
			`"header":{"type":"string","description":"a label of one or two words shown as a chip, e.g. \"Scope\""},` +
			`"multi_select":{"type":"boolean","description":"true when several options may be chosen"},` +
			`"options":{"type":"array","minItems":` + strconv.Itoa(minAskOptions) + `,"maxItems":` + strconv.Itoa(maxAskOptions) + `,"items":{"type":"object","properties":{` +
			`"label":{"type":"string","description":"a few words, the choice itself"},` +
			`"description":{"type":"string","description":"one line: what choosing it means"}},"required":["label"],"additionalProperties":false}}},` +
			`"required":["question","options"],"additionalProperties":false}}},"required":["questions"],"additionalProperties":false}`,
		exec: askExec},
	{name: "artefact_read", group: toolGroupArtefactRead, parallelSafe: true,
		description: "Read the artefact, or a range of its lines, with each line numbered, and its revision.",
		schema:      `{"type":"object","properties":{"from_line":{"type":"integer","minimum":1},"to_line":{"type":"integer","minimum":1}},"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_outline", group: toolGroupArtefactRead, parallelSafe: true,
		description: "List the artefact's headings: level, heading path, slug, and the line span of each section.",
		schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_find", group: toolGroupArtefactRead, parallelSafe: true,
		description: "Find a text or a regular expression (RE2) in the artefact; returns line, column and the line of each match.",
		schema:      `{"type":"object","properties":{"pattern":{"type":"string"},"regex":{"type":"boolean"},"case_sensitive":{"type":"boolean"}},"required":["pattern"],"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_inspect", group: toolGroupArtefactRead, parallelSafe: true,
		description: "The artefact's parsed structure: frontmatter properties, links and wikilinks, embeds, tags, callouts, code blocks and footnotes, each with its line.",
		schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_lint", group: toolGroupArtefactRead, parallelSafe: true,
		description: "Check the whole artefact for markdown that renders differently from what was meant: links to missing headings, skipped heading levels, unclosed fences, invalid frontmatter, footnotes without definitions.",
		schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_write", group: toolGroupArtefactWrite,
		description: "Replace the whole artefact with text.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"text":{"type":"string"}},"required":["base_revision","text"],"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_edit", group: toolGroupArtefactWrite,
		description: "Replace old_text, which must occur exactly once (or every occurrence with replace_all), with new_text.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"old_text":{"type":"string"},"new_text":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["base_revision","old_text","new_text"],"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_insert", group: toolGroupArtefactWrite,
		description: "Insert lines before a line number (one past the last line appends), or at the start or end of a section named by heading path.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"text":{"type":"string"},"line":{"type":"integer","minimum":1},` + headingProperty + `,"at":{"type":"string","enum":["start","end"],"description":"with heading: right after the heading line, or before the next heading"}},"required":["base_revision","text"],"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_replace_section", group: toolGroupArtefactWrite,
		description: "Replace the body of a section, named by heading path, keeping its heading; with_heading replaces the heading line too, and body must then start with the new heading.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,` + headingProperty + `,"body":{"type":"string"},"with_heading":{"type":"boolean"}},"required":["base_revision","heading","body"],"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_set_frontmatter", group: toolGroupArtefactWrite,
		description: "Set or delete top-level frontmatter properties; the others, their order and comments stay.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"set":{"type":"object"},"delete":{"type":"array","items":{"type":"string"}}},"required":["base_revision"],"additionalProperties":false}`,
		exec:        artefactExec},
	{name: "artefact_images", group: toolGroupImageRead, parallelSafe: true,
		description: "List the artefact's screenshots: name, size, where each came from, whether its bytes were purged, and the budget's use.",
		schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
		exec:        imageExec},
	{name: "artefact_capture", group: toolGroupImageCapture,
		description: "Capture windows of your task as a PNG and add it to the artefact's screenshots. The windows are drawn together, cut to their bounds; crop keeps a rect of the frame instead, in logical points.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"windows":{"type":"array","items":{"type":"integer"},"minItems":1},"crop":{"type":"object","properties":{"x":{"type":"number"},"y":{"type":"number"},"w":{"type":"number","exclusiveMinimum":0},"h":{"type":"number","exclusiveMinimum":0}},"required":["x","y","w","h"],"additionalProperties":false},` + imageNameProperty + `},"required":["base_revision","windows"],"additionalProperties":false}`,
		exec:        imageExec},
	{name: "artefact_copy_image", group: toolGroupImageWrite,
		description: "Add a second name on an existing screenshot's bytes, e.g. to keep it before cropping.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"from":{"type":"string"},` + imageNameProperty + `},"required":["base_revision","from"],"additionalProperties":false}`,
		exec:        imageExec},
	{name: "artefact_crop_image", group: toolGroupImageWrite,
		description: "Add a new screenshot cut out of an existing one, by a rect in its pixels (artefact_images gives each one's size).",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"from":{"type":"string"},"x":{"type":"integer","minimum":0},"y":{"type":"integer","minimum":0},"w":{"type":"integer","minimum":1},"h":{"type":"integer","minimum":1},` + imageNameProperty + `},"required":["base_revision","from","x","y","w","h"],"additionalProperties":false}`,
		exec:        imageExec},
	{name: "artefact_remove_image", group: toolGroupImageWrite,
		description: "Remove a screenshot's name. Its bytes stay while an earlier revision names them; purge frees them for good, and a revert cannot bring them back.",
		schema:      `{"type":"object","properties":{` + baseRevisionProperty + `,"name":{"type":"string"},"purge":{"type":"boolean"}},"required":["base_revision","name"],"additionalProperties":false}`,
		exec:        imageExec},
	{name: "artefact_view_image", group: toolGroupView,
		description: "Look at one of the artefact's screenshots: the chat attaches its pixels in a message after this round's tool results, for the rest of this turn. Depending on the person's settings it waits for them to allow it.",
		schema:      `{"type":"object","properties":{"name":{"type":"string","description":"the screenshot's name, as artefact_images lists it"}},"required":["name"],"additionalProperties":false}`,
		exec:        imageExec},
	{name: "recall", group: toolGroupRecall, parallelSafe: true, budget: recallBudget,
		description: "Read back a tool result that was elided from this conversation's compacted history, by the ordinal its stub names.",
		schema:      `{"type":"object","properties":{"ordinal":{"type":"integer","minimum":1,"description":"the ordinal the stub names"}},"required":["ordinal"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.recall(spec, args)
		}},
	{name: "request_access", group: toolGroupFixed,
		description: "Ask the person to share windows with you for a task, and optionally to let you open windows of apps. Waits for the person's decision.",
		schema:      `{"type":"object","properties":{"plan":{"type":"string","description":"one line: what you intend to do"},"open":{"type":"array","description":"apps you want to open windows of","items":{"type":"object","properties":{"app":{"type":"string"},"count":{"type":"integer","minimum":1,"maximum":4}},"required":["app"],"additionalProperties":false}},"destinations":{"type":"array","description":"what your runs may reach, e.g. keelson:apps, keelson-bundle:<bundle> or clickhouse:host:port; publish:<prefix> lets publish_result and publish_projection publish under names with that prefix without asking each time","items":{"type":"string"}},"desktop":{"type":"boolean","description":"ask to arrange every window on the desktop, the person's own included"}},"required":["plan"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			content, activity = inst.requestAccess(ctx, inst.asked(o, call.Id), argString(args, "plan"), args)
			if strings.HasPrefix(content, "access granted") {
				// The grant is journaled as the answer it was: the person's,
				// the consent the task's consequential calls proceed under
				// (ADR-0311 §SD3, law L15); or the host's in their place
				// (ADR-0298), which consents to nothing.
				if err := inst.journalGrant(ctx, o.turn); err != nil {
					log.Warn().Err(err).Str("conversation", inst.conversation).Msg("chat: the grant was not journaled")
				}
			}
			return
		}},
	{name: "list_windows", group: toolGroupFixed,
		description:  "List the windows of your task, with their app, mode and load (opening, ready or failed).",
		schema:       `{"type":"object","properties":{},"additionalProperties":false}`,
		parallelSafe: true,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.listWindows(ctx, inst.asked(o, call.Id))
		}},
	{name: "describe_app", group: toolGroupFixed,
		description:  "With no arguments, list every app and the operations it offers you; with app, that app's; with app and operation, the operation's argument schema.",
		schema:       `{"type":"object","properties":{"app":{"type":"string","description":"an app id as describe_app lists it"},"search":{"type":"string","description":"filter apps and operations by a word"},"operation":{"type":"string"}},"additionalProperties":false}`,
		parallelSafe: true,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.describeApp(ctx, inst.asked(o, call.Id), args)
		}},
	{name: "read_help", group: toolGroupFixed,
		description:  "Read the documentation apps ship: with search, sections across the apps matching it; with app, that app's documents and their sections; with app and doc, the document, or with section as well, that section.",
		schema:       `{"type":"object","properties":{"search":{"type":"string","description":"words to find in the apps' help"},"app":{"type":"string","description":"an app id as describe_app lists it"},"doc":{"type":"string","description":"a document as read_help lists it"},"section":{"type":"string","description":"a section slug as read_help lists it"}},"additionalProperties":false}`,
		parallelSafe: true,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.readHelp(ctx, agent.HelpRequest{App: argString(args, "app"), Doc: argString(args, "doc"), Section: argString(args, "section"), Search: argString(args, "search"),
				Asked: inst.asked(o, call.Id)})
		}},
	{name: "call_operation", group: toolGroupFixed,
		description: "Call one operation in one window of your task; the operation's own arguments go under args.",
		schema:      `{"type":"object","properties":{"window":{"type":"integer"},"operation":{"type":"string"},"args":{"type":"object"},"reason":{"type":"string","description":"one line, shown to the person"}},"required":["window","operation"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.call(ctx, o, call.Id, args)
		}},
	{name: "open_window", group: toolGroupFixed,
		description: "Open a window of an app your task may open; it joins your task.",
		schema:      `{"type":"object","properties":{"app":{"type":"string"}},"required":["app"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.openWindow(ctx, o, call.Id, argString(args, "app"))
		}},
	{name: "query_windows", group: toolGroupFixed,
		description:  "Run a SELECT over keelson('windows') — every open window: key, app_id, display, title, x, y, w, h, need_w, need_h, stack, active, maximized, collapsed, shown, agent_tasks — or over keelson('desktop'), the work area (work_x, work_y, work_w, work_h), the active window's key and the arrangement in progress. Rows come back as JSON lines.",
		schema:       `{"type":"object","properties":{"table":{"type":"string","enum":["windows","desktop"]},"sql":{"type":"string","description":"a SELECT naming keelson('<table>'), without FORMAT"}},"required":["table","sql"],"additionalProperties":false}`,
		budget:       maxWindowRows,
		parallelSafe: true,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.queryWindows(ctx, argString(args, "table"), argString(args, "sql"), spec.budget)
		}},
	{name: "arrange_windows", group: toolGroupFixed,
		description: "Lay windows out on the desktop: cascade, tile, columns (side by side), rows (stacked), or gather (bring windows back into view). Without windows it arranges every window; with them, only those, leaving the rest where they are. Needs the desktop granted (request_access with desktop true).",
		schema:      `{"type":"object","properties":{"command":{"type":"string","enum":["cascade","tile","columns","rows","gather"]},"windows":{"type":"array","items":{"type":"integer"},"description":"window keys from query_windows; empty is every window"}},"required":["command"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.windowVerb(ctx, inst.asked(o, call.Id), call.Name, args)
		}},
	{name: "raise_window", group: toolGroupFixed,
		description: "Bring a window of your task, shared in act mode, to the front.",
		schema:      `{"type":"object","properties":{"window":{"type":"integer"}},"required":["window"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.windowVerb(ctx, inst.asked(o, call.Id), call.Name, args)
		}},
	{name: "place_window", group: toolGroupFixed,
		description: "Move and size a window of your task, shared in act mode: x, y is the top-left corner and w, h the outer size, in the coordinates query_windows reports. A window whose content does not fill the height or width keeps its content's size there.",
		schema:      `{"type":"object","properties":{"window":{"type":"integer"},"x":{"type":"number"},"y":{"type":"number"},"w":{"type":"number"},"h":{"type":"number"}},"required":["window","x","y","w","h"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.windowVerb(ctx, inst.asked(o, call.Id), call.Name, args)
		}},
	{name: "read_window_tree", group: toolGroupFixed,
		description:  "Read what windows of your task show on screen, as an outline taken from one frame when you call it: each line is a widget — role, name, value — with its rect [x,y wxh] in the coordinates query_windows reports, and last the kind of part that drew it. A table's visible cells print as rows of data. Counts say what is out of view or not drawn; their contents are not in the outline. It shows the pixels, not the app's state: revisions, phase, signals and results come from the app's operations. Observe mode suffices.",
		schema:       `{"type":"object","properties":{"windows":{"type":"array","items":{"type":"integer"},"minItems":1,"description":"window numbers, as list_windows gives them"}},"required":["windows"],"additionalProperties":false}`,
		budget:       maxTreeBytes,
		parallelSafe: true,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.readWindowTree(ctx, o, windowKeys(args["windows"]), spec.budget)
		}},
	{name: "point_out", group: toolGroupFixed,
		description: "Show the person something on screen: highlight parts, attach a note (callout), draw an arrow from one part to another, mark a numbered step, or dim everything else (spotlight). Name parts by a window tree you read — tree and node, e.g. {\"tree\":\"t1\",\"node\":\"#12.3\"}, or a whole table row by its row line's #60r — or a whole window. A callout already outlines its target: do not add a highlight of the same part. The mark follows its window and stays until the person clears it; marking the same id replaces it. Needs suggest or act mode on the windows. The style is the host's: you choose no colour or position.",
		schema:      `{"type":"object","properties":{"id":{"type":"string","description":"names the mark within your task"},"op":{"type":"string","enum":["highlight","callout","arrow","step","spotlight"]},"targets":{"type":"array","minItems":1,"maxItems":8,"description":"callout and step take one, arrow two (from, to)","items":{"type":"object","properties":{"tree":{"type":"string","description":"a tree reference, as read_window_tree gave it"},"node":{"type":"string","description":"a part of that tree: #12 or #12.3"},"window":{"type":"integer","description":"a whole window, instead of a tree part"}},"additionalProperties":false}},"text":{"type":"string","description":"the note or label, one short line; callout and step need it"}},"required":["id","op","targets"],"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.pointOut(ctx, inst.asked(o, call.Id), args)
		}},
	{name: "clear_marks", group: toolGroupFixed,
		description: "Remove one of your marks by id, or all of yours without an id.",
		schema:      `{"type":"object","properties":{"id":{"type":"string"}},"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.clearMarks(ctx, inst.asked(o, call.Id), argString(args, "id"))
		}},
	{name: "stop_task", group: toolGroupFixed,
		description: "End your task; the windows you opened pass to the person.",
		schema:      `{"type":"object","properties":{},"additionalProperties":false}`,
		exec: func(ctx context.Context, inst *coordinator, spec *toolSpec, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
			return inst.stopTask(ctx, inst.asked(o, call.Id))
		}},
}

// toolsByName indexes the registry; a duplicate name is a programming
// error caught at start-up.
var toolsByName = func() (m map[string]*toolSpec) {
	m = make(map[string]*toolSpec, len(coordinatorTools))
	for i := range coordinatorTools {
		spec := &coordinatorTools[i]
		if _, dup := m[spec.name]; dup {
			panic("chat: two coordinator tools are named " + spec.name)
		}
		spec.params = toolSchema(spec.schema)
		m[spec.name] = spec
	}
	return
}()

// lookupTool is the registry's entry for a tool name, nil for none.
func lookupTool(name string) (spec *toolSpec) { return toolsByName[name] }

// toolsOfGroups projects the registry: the tools of the groups named, in
// the registry's order.
func toolsOfGroups(groups ...toolGroupE) (out []openaichat.Tool) {
	for i := range coordinatorTools {
		spec := &coordinatorTools[i]
		for _, g := range groups {
			if spec.group == g {
				out = append(out, spec.tool())
				break
			}
		}
	}
	return
}

// argString is a call's string argument, empty when absent or not a string.
func argString(args map[string]any, k string) (s string) { s, _ = args[k].(string); return }

// windowKeys reads a list of window numbers out of a call's argument.
func windowKeys(v any) (keys []uint64) {
	if ws, ok := v.([]any); ok {
		for _, w := range ws {
			if f, isNum := w.(float64); isNum {
				keys = append(keys, uint64(f))
			}
		}
	}
	return
}
