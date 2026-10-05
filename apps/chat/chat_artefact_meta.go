package chat

// The artefact's chat properties (ADR-0282 §SD1): the frontmatter keys the
// chat keeps, so the document says which session made it wherever it is
// taken — the conversation id that keys the host's records, the run and
// window, the model, when the conversation started. They are stamped into
// every write before it is shown or committed; the model cannot set,
// change or remove them.

import (
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdspan"
)

// chatPropertyPrefix marks the properties the chat keeps.
const chatPropertyPrefix = "chat_"

// artMeta is what the properties say. The render goroutine sets it each
// frame; an empty field removes its property.
type artMeta struct {
	conversation string
	title        string
	started      time.Time
	model        string
	endpoint     string
	run          string
	app          string
	window       uint64
	task         string
	keep         bool
}

// chatProperties lists every property the chat keeps, so one that became
// empty is removed.
var chatProperties = []string{
	"chat_conversation", "chat_title", "chat_started", "chat_model", "chat_endpoint",
	"chat_run", "chat_app", "chat_window", "chat_task", "chat_keep",
}

// properties are the meta as frontmatter: what to set, and what to delete.
func (inst artMeta) properties() (set map[string]any, del []string) {
	set = make(map[string]any, len(chatProperties))
	str := func(k string, v string) {
		if v == "" {
			del = append(del, k)
			return
		}
		set[k] = v
	}
	str("chat_conversation", inst.conversation)
	str("chat_title", inst.title)
	if inst.started.IsZero() {
		del = append(del, "chat_started")
	} else {
		set["chat_started"] = inst.started.Truncate(time.Second)
	}
	str("chat_model", inst.model)
	str("chat_endpoint", inst.endpoint)
	str("chat_run", inst.run)
	str("chat_app", inst.app)
	if inst.window == 0 {
		del = append(del, "chat_window")
	} else {
		set["chat_window"] = inst.window
	}
	str("chat_task", inst.task)
	set["chat_keep"] = inst.keep
	return
}

// isChatProperty says whether a frontmatter key is one the chat keeps.
func isChatProperty(k string) bool { return strings.HasPrefix(k, chatPropertyPrefix) }

// stampChat writes the chat's properties into text; reason is set when the
// frontmatter is not valid YAML, which leaves nowhere to write them.
func stampChat(text string, m artMeta) (out string, reason string) {
	if m.conversation == "" {
		// No session to describe: a test, or a frame not yet drawn.
		return text, ""
	}
	set, del := m.properties()
	b, _, err := mdspan.SetFrontmatter([]byte(text), set, del)
	if err != nil {
		return text, "the frontmatter is not valid YAML, and the chat keeps its chat_* properties there: write the frontmatter as valid YAML"
	}
	return string(b), ""
}

// changedLines is the lines of new that differ from old: from the first
// differing line to the last, by common head and tail.
func changedLines(old string, new string) (ls mdspan.LineSpan) {
	a, b := splitLines(old), splitLines(new)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	first, last := p+1, len(b)-s
	if last < first {
		// A pure deletion: the line the cut closed on.
		last = first
	}
	return mdspan.LineSpan{First: min(first, max(1, len(b))), Last: min(last, max(1, len(b)))}
}

// artMeta is the session as the artefact's properties say it.
func (inst *App) artMeta() (m artMeta) {
	conv := inst.conv
	m = artMeta{conversation: conv.id, title: conv.title, run: inst.runId, window: inst.window, keep: conv.keep,
		app: string(ManifestId)}
	if !conv.started {
		m.keep = inst.keep
	}
	if conv.startedAt > 0 {
		m.started = time.UnixMilli(conv.startedAt).In(time.Local)
	}
	if inst.model.Configured {
		m.model, m.endpoint = inst.model.Model, inst.model.EndpointHost
	}
	if task, _, _ := inst.coord.stateOrNone(); task != "" {
		m.task = task
	}
	return
}
