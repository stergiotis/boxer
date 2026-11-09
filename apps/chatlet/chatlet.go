// Package chatlet mints the chat specialised for one scenario from a
// committed markdown document (ADR-0364, proposed): one document, one app.
//
// A document's frontmatter is its manifest and its settings; its body is the
// system prompt, as in a promptbook document. What a window is made of is
// chat.Preset. Only committed books are read: a reviewed corpus is the gate
// on what a system prompt may say, and there is no store to mint from.
package chatlet

import (
	"maps"
	"slices"
	"strings"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"github.com/stergiotis/boxer/apps/chat"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
)

// idPrefix makes a slug into an app id; a slug is public once it ships.
const idPrefix = "github.com/stergiotis/boxer/apps/chatlet/"

// knownKeys are the frontmatter a document may carry. Anything else is
// refused: a misspelt limit must not read as no limit.
var knownKeys = map[string]struct{}{
	"title": {}, "summary": {}, "icon": {}, "topics": {}, "keywords": {},
	"apps": {}, "questions": {}, "artefact": {}, "rounds": {},
	"allow": {}, "changes": {}, "reach": {}, "launch": {}, "desktop": {},
	"greeting": {}, "starters": {}, "followups": {}, "purpose": {}, "watch": {}, "view": {}, "thinking": {},
}

// ParseDocSource reads one chatlet document into the preset it mints. topics
// is the book's default, used when the document names none.
func ParseDocSource(bookID string, path string, src []byte, topics []app.TopicT) (p chat.Preset, err error) {
	def, err := promptbook.ParseDocSource(bookID, path, src)
	if err != nil {
		return
	}
	gm := obsidian.New(obsidian.Options{Features: obsidian.FeatureFrontmatter})
	pc := obsidian.NewParserContext()
	gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	meta, err := obsidian.TryGetFrontmatter(pc)
	if err != nil {
		return p, eb.Build().Str("bookID", bookID).Str("path", path).Errorf("chatlet: frontmatter: %w", err)
	}
	for _, k := range slices.Sorted(maps.Keys(meta)) {
		if _, known := knownKeys[k]; !known {
			return p, eb.Build().Str("bookID", bookID).Str("path", path).Str("key", k).Errorf("chatlet: unknown frontmatter key")
		}
	}
	r := reader{bookID: bookID, path: path, meta: meta}
	m := chat.BaseManifest()
	m.Id, m.Display, m.Title, m.Summary = app.AppIdT(idPrefix+def.Slug), def.Title, def.Title, def.Summary
	m.Kind = app.KindChatlet
	if def.Icon != "" {
		m.Icon = def.Icon
	}
	m.Topics, m.Keywords = r.topics(topics), r.strs("keywords")
	p = chat.Preset{Manifest: m, Book: bookID, System: def.System,
		Apps: r.flag("apps"), Questions: r.flag("questions"), Artefact: r.flag("artefact"), Launch: r.flag("launch"), Desktop: r.flag("desktop"),
		Rounds: r.number("rounds"), Allow: r.required("allow"), Changes: r.required("changes"), Reach: r.required("reach"),
		Greeting: r.text("greeting"), Starters: r.starters("starters"), Followups: r.starters("followups"), Purpose: r.text("purpose"), Watch: r.flag("watch"),
		Simple: r.oneOf("view", "full", "simple"), HideThinking: r.oneOf("thinking", "shown", "hidden"), Source: string(src)}
	if err = r.err; err != nil {
		return
	}
	err = p.Validate()
	return
}

// reader reads typed values out of a document's frontmatter, keeping the
// first error.
type reader struct {
	bookID string
	path   string
	meta   map[string]any
	err    error
}

func (inst *reader) fail(key string, what string) {
	if inst.err == nil {
		inst.err = eb.Build().Str("bookID", inst.bookID).Str("path", inst.path).Str("key", key).Errorf("chatlet: frontmatter key %s", what)
	}
}

func (inst *reader) flag(key string) (v bool) {
	raw, has := inst.meta[key]
	if !has {
		return
	}
	v, ok := raw.(bool)
	if !ok {
		inst.fail(key, "must be true or false")
	}
	return
}

func (inst *reader) number(key string) (v int) {
	switch n := inst.meta[key].(type) {
	case nil:
	case int:
		v = n
	case uint64:
		v = int(n)
	case int64:
		v = int(n)
	default:
		inst.fail(key, "must be a whole number")
	}
	return
}

// required reads a setting that bounds the model. It has no default: the
// author states it.
func (inst *reader) required(key string) (v string) {
	v, ok := inst.meta[key].(string)
	if !ok || strings.TrimSpace(v) == "" {
		inst.fail(key, "is required")
	}
	return
}

func (inst *reader) text(key string) (v string) {
	raw, has := inst.meta[key]
	if !has {
		return
	}
	v, ok := raw.(string)
	if !ok {
		inst.fail(key, "must be a string")
	}
	return
}

// oneOf reads a key that takes one of two words, and says whether it was the
// second; absent is the first, the plain chat's. Words rather than a flag:
// "thinking: false" reads as a model that does not reason, which no key here
// can arrange.
func (inst *reader) oneOf(key string, first string, second string) (isSecond bool) {
	raw, has := inst.meta[key]
	if !has {
		return
	}
	switch raw {
	case first:
	case second:
		isSecond = true
	default:
		inst.fail(key, "must be "+first+" or "+second)
	}
	return
}

// starters reads a list of {label, text}. Unknown fields are refused, as at
// the top level.
func (inst *reader) starters(key string) (out []chat.Starter) {
	raw, has := inst.meta[key]
	if !has {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		inst.fail(key, "must be a list")
		return
	}
	for _, e := range list {
		fields, isMap := stringKeyed(e)
		if !isMap {
			inst.fail(key, "entries must have a label and a text")
			return
		}
		var s chat.Starter
		for k, v := range fields {
			str, isStr := v.(string)
			switch {
			case !isStr:
				inst.fail(key, "fields must be strings")
				return
			case k == "label":
				s.Label = str
			case k == "text":
				s.Text = str
			default:
				inst.fail(key, "entries may carry only label and text")
				return
			}
		}
		out = append(out, s)
	}
	return
}

// stringKeyed reads a YAML mapping, whichever key type the decoder chose.
func stringKeyed(v any) (m map[string]any, ok bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[any]any:
		m = make(map[string]any, len(t))
		for k, e := range t {
			name, isStr := k.(string)
			if !isStr {
				return nil, false
			}
			m[name] = e
		}
		return m, true
	}
	return nil, false
}

func (inst *reader) strs(key string) (out []string) {
	raw, has := inst.meta[key]
	if !has {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		inst.fail(key, "must be a list")
		return
	}
	for _, e := range list {
		s, isStr := e.(string)
		if !isStr {
			inst.fail(key, "entries must be strings")
			return
		}
		out = append(out, s)
	}
	return
}

func (inst *reader) topics(def []app.TopicT) (out []app.TopicT) {
	names := inst.strs("topics")
	if len(names) == 0 {
		return def
	}
	for _, name := range names {
		t, known := app.ParseTopic(name)
		if !known {
			inst.fail("topics", "names an unregistered topic: "+name)
			return
		}
		out = append(out, t)
	}
	return
}
