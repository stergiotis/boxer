package chat

// The artefact_* tools (ADR-0282 §SD2, §SD3). Reads are offered whenever the
// conversation has an artefact; writes only when the settings allow Edit.
// Every write names the revision it was computed against and is refused
// when the artefact moved since; under Ask first it waits, inside the turn,
// for the person's verdict in the panel. The edits themselves are pure
// functions over the text, at the bottom of the file.

import (
	"context"
	"encoding/json/v2"
	"regexp"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdlint"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdspan"
)

// artefactPrompt is the system message's part for a conversation with an
// artefact.
const artefactPrompt = `This conversation has an artefact: one markdown document (Obsidian-flavoured: wikilinks, callouts, tags, frontmatter, footnotes) that you build with the person. They see it in a panel beside the conversation.
- Put the document's content in the artefact, not in your answer; answer with a short account of what you changed and what is open.
- Read before you write: artefact_read gives numbered lines and the revision, artefact_outline the headings with their line spans. Every write names base_revision, the revision you read; a write against an older one is refused — read again.
- Prefer small writes: artefact_edit replaces a text that occurs exactly once, artefact_replace_section a section's body by heading path, artefact_insert adds lines; artefact_write replaces everything and suits a first draft.
- Each write returns the lint findings for the lines it changed; fix what they report. artefact_lint checks the whole document and artefact_inspect lists its links, tags, callouts, code blocks and footnotes.
- The frontmatter's chat_* properties (conversation, run, model, start time and more) are the chat's: it writes them into every revision; leave them as they are, keep the frontmatter valid YAML, and add your own properties beside them.
- The person's settings decide whether you may write and whether each write waits for them; a rejected change is theirs to explain — ask, do not repeat it.`

// maxReadLines bounds one artefact_read; maxFindMatches one artefact_find.
const (
	maxReadLines   = 1500
	maxFindMatches = 60
	maxMatchRunes  = 200
)

var artLinter = mdlint.NewDefaultLinter()

// artefactTools are the tools for the artefact; write adds the writes.
func artefactTools(write bool) (out []openaichat.Tool) {
	out = []openaichat.Tool{
		{Name: "artefact_read", Description: "Read the artefact, or a range of its lines, with each line numbered, and its revision.",
			Parameters: toolSchema(`{"type":"object","properties":{"from_line":{"type":"integer","minimum":1},"to_line":{"type":"integer","minimum":1}},"additionalProperties":false}`)},
		{Name: "artefact_outline", Description: "List the artefact's headings: level, heading path, slug, and the line span of each section.",
			Parameters: toolSchema(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{Name: "artefact_find", Description: "Find a text or a regular expression (RE2) in the artefact; returns line, column and the line of each match.",
			Parameters: toolSchema(`{"type":"object","properties":{"pattern":{"type":"string"},"regex":{"type":"boolean"},"case_sensitive":{"type":"boolean"}},"required":["pattern"],"additionalProperties":false}`)},
		{Name: "artefact_inspect", Description: "The artefact's parsed structure: frontmatter properties, links and wikilinks, embeds, tags, callouts, code blocks and footnotes, each with its line.",
			Parameters: toolSchema(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{Name: "artefact_lint", Description: "Check the whole artefact for markdown that renders differently from what was meant: links to missing headings, skipped heading levels, unclosed fences, invalid frontmatter, footnotes without definitions.",
			Parameters: toolSchema(`{"type":"object","properties":{},"additionalProperties":false}`)},
	}
	if !write {
		return
	}
	base := `"base_revision":{"type":"integer","minimum":0,"description":"the revision you read"}`
	heading := `"heading":{"type":"array","items":{"type":"string"},"minItems":1,"description":"heading path, outermost first; the last element names the heading, the ones before its nearest ancestors"}`
	out = append(out,
		openaichat.Tool{Name: "artefact_write", Description: "Replace the whole artefact with text.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"text":{"type":"string"}},"required":["base_revision","text"],"additionalProperties":false}`)},
		openaichat.Tool{Name: "artefact_edit", Description: "Replace old_text, which must occur exactly once (or every occurrence with replace_all), with new_text.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"old_text":{"type":"string"},"new_text":{"type":"string"},"replace_all":{"type":"boolean"}},"required":["base_revision","old_text","new_text"],"additionalProperties":false}`)},
		openaichat.Tool{Name: "artefact_insert", Description: "Insert lines before a line number (one past the last line appends), or at the start or end of a section named by heading path.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"text":{"type":"string"},"line":{"type":"integer","minimum":1},` + heading + `,"at":{"type":"string","enum":["start","end"],"description":"with heading: right after the heading line, or before the next heading"}},"required":["base_revision","text"],"additionalProperties":false}`)},
		openaichat.Tool{Name: "artefact_replace_section", Description: "Replace the body of a section, named by heading path, keeping its heading; with_heading replaces the heading line too, and body must then start with the new heading.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,` + heading + `,"body":{"type":"string"},"with_heading":{"type":"boolean"}},"required":["base_revision","heading","body"],"additionalProperties":false}`)},
		openaichat.Tool{Name: "artefact_set_frontmatter", Description: "Set or delete top-level frontmatter properties; the others, their order and comments stay.",
			Parameters: toolSchema(`{"type":"object","properties":{` + base + `,"set":{"type":"object"},"delete":{"type":"array","items":{"type":"string"}}},"required":["base_revision"],"additionalProperties":false}`)},
	)
	return
}

// isArtefactTool says whether a tool name is one of the artefact's.
func isArtefactTool(name string) bool { return strings.HasPrefix(name, "artefact_") }

// isArtefactWrite says whether an artefact tool changes the document.
func isArtefactWrite(name string) bool {
	switch name {
	case "artefact_write", "artefact_edit", "artefact_insert", "artefact_replace_section", "artefact_set_frontmatter":
		return true
	}
	return false
}

// toolArgs reads a call's arguments by kind.
type toolArgs map[string]any

func (inst toolArgs) str(k string) (s string) { s, _ = inst[k].(string); return }
func (inst toolArgs) flag(k string) (b bool)  { b, _ = inst[k].(bool); return }

func (inst toolArgs) num(k string) (n int, has bool) {
	f, has := inst[k].(float64)
	return int(f), has
}

func (inst toolArgs) strs(k string) (out []string) {
	switch v := inst[k].(type) {
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case string:
		// A path written as one string, "A > B".
		for _, p := range strings.Split(v, ">") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return
}

// artefactCall runs one artefact tool call.
func (inst *coordinator) artefactCall(ctx context.Context, o toolOrigin, name string, raw map[string]any, art *artefact) (content string, activity string) {
	args := toolArgs(raw)
	n, text := art.head()
	switch name {
	case "artefact_read":
		return artRead(n, text, args), name + " · revision " + strconv.Itoa(n)
	case "artefact_outline":
		return marshal(artOutline(n, text)), name + " · revision " + strconv.Itoa(n)
	case "artefact_find":
		out, reason := artFind(n, text, args)
		if reason != "" {
			return "error: " + reason, name + ": " + reason
		}
		return marshal(out), name + " · " + plural(len(out.Matches), "match")
	case "artefact_inspect":
		return marshal(artInspect(n, text)), name + " · revision " + strconv.Itoa(n)
	case "artefact_lint":
		fs := artLinter.LintSource([]byte(text))
		return marshal(lintView{Revision: n, Findings: findingViews(fs)}), name + " · " + plural(len(fs), "finding")
	}
	if !isArtefactWrite(name) {
		return "error: no tool " + name, "unknown tool " + name
	}
	p := art.policyNow()
	if !p.write {
		reason := "the person's settings let you read the artefact, not change it"
		inst.refuse(reason)
		return "error: " + reason, name + ": refused, read only"
	}
	base, has := args.num("base_revision")
	if !has {
		return "error: base_revision is required: the revision you read", name + ": no base_revision"
	}
	if base != n {
		e := errStale{base: base, head: n}
		return "error: " + e.Error(), name + ": stale, at revision " + strconv.Itoa(n)
	}
	ch, reason := artApply(name, text, args)
	if reason != "" {
		return "error: " + reason, name + ": " + reason
	}
	// The chat's properties go into every write, so the diff the person
	// reviews and the revision both carry them.
	if ch.text, reason = stampChat(ch.text, art.metaNow()); reason != "" {
		return "error: " + reason, name + ": " + reason
	}
	ch.changed = changedLines(text, ch.text)
	if ch.text == text {
		return marshal(writeView{Revision: n, Lines: lineCount(text), Unchanged: true}), name + " · no change"
	}
	title := inst.titleNow()
	if p.ask {
		done := inst.awaitPerson()
		accepted, err := art.propose(ctx, &artProposal{base: base, text: ch.text, tool: name, title: title})
		done()
		if err != nil {
			return "error: the proposed change was withdrawn: " + err.Error(), name + ": proposal withdrawn"
		}
		if !accepted {
			return marshal(writeView{Revision: n, Rejected: true, Then: "the person rejected this change; ask what they want instead, do not repeat it"}), name + " · rejected"
		}
	}
	rev, err := art.commit(base, artRevision{text: ch.text, source: revSourceModel, turn: o.turn, tool: name, title: title,
		changedFirst: ch.changed.First, changedLast: ch.changed.Last})
	if err != nil {
		return "error: " + err.Error(), name + ": " + err.Error()
	}
	fs := mdlint.Within(artLinter.LintSource([]byte(ch.text)), ch.changed)
	content = marshal(writeView{Revision: rev, Lines: lineCount(ch.text), Changed: &lineSpanView{First: ch.changed.First, Last: ch.changed.Last}, Findings: findingViews(fs)})
	activity = name + " · revision " + strconv.Itoa(rev) + " · " + lineRange(ch.changed)
	if len(fs) > 0 {
		activity += " · " + plural(len(fs), "finding")
	}
	return
}

func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "error: " + err.Error()
	}
	return string(b)
}

func lineCount(text string) int { return mdspan.NewLineIndex([]byte(text)).Count() }

func lineRange(ls mdspan.LineSpan) string {
	if ls.First == ls.Last {
		return "line " + strconv.Itoa(ls.First)
	}
	return "lines " + strconv.Itoa(ls.First) + "–" + strconv.Itoa(ls.Last)
}

// --- reads ---------------------------------------------------------------------

// artRead is the text as the model reads it: a header, then each line
// numbered.
func artRead(n int, text string, args toolArgs) string {
	idx := mdspan.NewLineIndex([]byte(text))
	total := idx.Count()
	var b strings.Builder
	b.WriteString("revision " + strconv.Itoa(n) + " · " + plural(total, "line"))
	if total == 0 {
		b.WriteString(" · the artefact is empty")
		return b.String()
	}
	from, hasFrom := args.num("from_line")
	to, hasTo := args.num("to_line")
	if !hasFrom {
		from = 1
	}
	if !hasTo {
		to = total
	}
	from, to = max(1, from), min(total, to)
	if from > to {
		b.WriteString(" · no lines in that range")
		return b.String()
	}
	more := 0
	if to-from+1 > maxReadLines {
		more, to = to-from+1-maxReadLines, from+maxReadLines-1
	}
	if from != 1 || to != total {
		b.WriteString(" · lines " + strconv.Itoa(from) + "–" + strconv.Itoa(to))
	}
	b.WriteByte('\n')
	for l := from; l <= to; l++ {
		line := text[idx.LineStart(l):idx.LineEnd(l)]
		b.WriteString(strconv.Itoa(l))
		b.WriteByte('\t')
		b.WriteString(strings.TrimRight(line, "\n"))
		b.WriteByte('\n')
	}
	if more > 0 {
		b.WriteString("… " + plural(more, "more line") + "; read on with from_line " + strconv.Itoa(to+1) + "\n")
	}
	return b.String()
}

type outlineItem struct {
	Level    uint8    `json:"level"`
	Path     []string `json:"path"`
	Slug     string   `json:"slug"`
	Line     int      `json:"line"`
	LastLine int      `json:"last_line"`
}

type outlineView struct {
	Revision int           `json:"revision"`
	Lines    int           `json:"lines"`
	Headings []outlineItem `json:"headings"`
}

func artOutline(n int, text string) (v outlineView) {
	d := mdspan.Parse([]byte(text))
	v = outlineView{Revision: n, Lines: d.Index.Count(), Headings: make([]outlineItem, 0, len(d.Headings))}
	for i, h := range d.Headings {
		v.Headings = append(v.Headings, outlineItem{Level: h.Level, Path: d.HeadingPath(i), Slug: h.Slug,
			Line: h.Line, LastLine: d.Index.LineSpanOf(h.Section).Last})
	}
	return
}

type findMatch struct {
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}

type findView struct {
	Revision  int         `json:"revision"`
	Matches   []findMatch `json:"matches"`
	Truncated bool        `json:"truncated,omitempty"`
}

func artFind(n int, text string, args toolArgs) (v findView, reason string) {
	pat := args.str("pattern")
	if pat == "" {
		return v, "pattern is empty"
	}
	if !args.flag("regex") {
		pat = regexp.QuoteMeta(pat)
	}
	if !args.flag("case_sensitive") {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return v, "the pattern is not a valid RE2 expression: " + err.Error()
	}
	idx := mdspan.NewLineIndex([]byte(text))
	v = findView{Revision: n, Matches: []findMatch{}}
	for _, m := range re.FindAllStringIndex(text, maxFindMatches+1) {
		if len(v.Matches) == maxFindMatches {
			v.Truncated = true
			break
		}
		l, c := idx.Position(m[0])
		line := strings.TrimRight(text[idx.LineStart(l):idx.LineEnd(l)], "\n")
		if r := []rune(line); len(r) > maxMatchRunes {
			line = string(r[:maxMatchRunes-1]) + "…"
		}
		v.Matches = append(v.Matches, findMatch{Line: l, Col: c, Text: line})
	}
	return
}

type inspectView struct {
	Revision    int             `json:"revision"`
	Lines       int             `json:"lines"`
	Words       uint64          `json:"words"`
	Title       string          `json:"title,omitempty"`
	Frontmatter []propertyView  `json:"frontmatter"`
	FrontErr    string          `json:"frontmatter_error,omitempty"`
	Links       []linkView      `json:"links"`
	Tags        []tagView       `json:"tags"`
	Callouts    []calloutView   `json:"callouts"`
	CodeBlocks  []codeBlockView `json:"code_blocks"`
	Footnotes   []footnoteView  `json:"footnotes"`
	Headings    int             `json:"headings"`
}

type propertyView struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type linkView struct {
	Kind     string `json:"kind"`
	Target   string `json:"target,omitempty"`
	Fragment string `json:"fragment,omitempty"`
	Text     string `json:"text,omitempty"`
	External bool   `json:"external,omitempty"`
	Line     uint64 `json:"line"`
}

type tagView struct {
	Tag    string `json:"tag"`
	Source string `json:"source"`
	Line   uint64 `json:"line,omitempty"`
}

type calloutView struct {
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	Line  int    `json:"line"`
}

type codeBlockView struct {
	Language string `json:"language,omitempty"`
	Line     int    `json:"line"`
	Lines    int    `json:"lines"`
	Closed   bool   `json:"closed"`
}

type footnoteView struct {
	Label      string `json:"label"`
	Line       int    `json:"line"`
	References []int  `json:"referenced_on"`
}

func artInspect(n int, text string) (v inspectView) {
	src := []byte(text)
	x := mdextract.Extract(src)
	d := mdspan.Parse(src)
	v = inspectView{Revision: n, Lines: d.Index.Count(), Words: x.Words, Title: x.Title, Headings: len(d.Headings),
		Frontmatter: []propertyView{}, Links: []linkView{}, Tags: []tagView{}, Callouts: []calloutView{}, CodeBlocks: []codeBlockView{}, Footnotes: []footnoteView{}}
	if fm := x.Frontmatter; fm != nil {
		v.FrontErr = fm.Err
		for _, l := range fm.Leaves {
			v.Frontmatter = append(v.Frontmatter, propertyView{Path: l.Path, Kind: l.Kind.String(), Value: leafValue(l)})
		}
	}
	for _, l := range x.Links {
		v.Links = append(v.Links, linkView{Kind: l.Kind.String(), Target: l.Target, Fragment: l.Fragment, Text: l.Text, External: l.External, Line: l.Line})
	}
	for _, t := range x.Tags {
		v.Tags = append(v.Tags, tagView{Tag: t.Tag, Source: t.Source.String(), Line: t.Line})
	}
	for _, c := range d.Callouts {
		v.Callouts = append(v.Callouts, calloutView{Type: c.Type, Title: c.Title, Line: c.Line})
	}
	for _, cb := range d.CodeBlocks {
		ls := d.Index.LineSpanOf(cb.Span)
		lang, _, _ := strings.Cut(strings.TrimSpace(cb.Info), " ")
		v.CodeBlocks = append(v.CodeBlocks, codeBlockView{Language: lang, Line: cb.Line, Lines: ls.Last - ls.First + 1, Closed: cb.IsClosed})
	}
	fn := mdlint.ScanFootnotes(d)
	for _, def := range fn.Defs {
		fv := footnoteView{Label: def.Label, Line: d.Index.LineOf(def.Span.Start), References: []int{}}
		for _, r := range fn.Refs {
			if r.Label == def.Label {
				fv.References = append(fv.References, d.Index.LineOf(r.Span.Start))
			}
		}
		v.Footnotes = append(v.Footnotes, fv)
	}
	return
}

func leafValue(l mdextract.Leaf) string {
	switch l.Kind {
	case mdextract.LeafKindString:
		return l.S
	case mdextract.LeafKindInt:
		return strconv.FormatInt(l.I, 10)
	case mdextract.LeafKindFloat:
		return strconv.FormatFloat(l.F, 'g', -1, 64)
	case mdextract.LeafKindBool:
		return strconv.FormatBool(l.B)
	case mdextract.LeafKindTime:
		return l.T.Format("2006-01-02T15:04:05Z07:00")
	}
	return l.Kind.String()
}

type findingView struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Line     int32  `json:"line"`
	Col      int32  `json:"col"`
	Message  string `json:"message"`
}

type lintView struct {
	Revision int           `json:"revision"`
	Findings []findingView `json:"findings"`
}

func findingViews(fs []mdlint.Finding) (out []findingView) {
	out = make([]findingView, 0, len(fs))
	for _, f := range fs {
		out = append(out, findingView{Rule: f.Rule, Severity: f.Severity.String(), Line: f.Line, Col: f.Col, Message: f.Message})
	}
	return
}

// --- writes --------------------------------------------------------------------

type lineSpanView struct {
	First int `json:"first"`
	Last  int `json:"last"`
}

type writeView struct {
	Revision  int           `json:"revision"`
	Lines     int           `json:"lines,omitempty"`
	Changed   *lineSpanView `json:"changed,omitempty"`
	Findings  []findingView `json:"findings,omitempty"`
	Unchanged bool          `json:"unchanged,omitempty"`
	Rejected  bool          `json:"rejected,omitempty"`
	Then      string        `json:"then,omitempty"`
}

// artChange is a computed write: the new text and the lines the change
// occupies in it.
type artChange struct {
	text    string
	changed mdspan.LineSpan
}

// artApply computes a write tool's change; reason says what is wrong with
// the call, for the model to fix.
func artApply(name string, text string, args toolArgs) (ch artChange, reason string) {
	switch name {
	case "artefact_write":
		return editWrite(args.str("text"))
	case "artefact_edit":
		return editReplace(text, args.str("old_text"), args.str("new_text"), args.flag("replace_all"))
	case "artefact_insert":
		line, hasLine := args.num("line")
		return editInsert(text, args.str("text"), line, hasLine, args.strs("heading"), args.str("at"))
	case "artefact_replace_section":
		return editSection(text, args.strs("heading"), args.str("body"), args.flag("with_heading"))
	case "artefact_set_frontmatter":
		set, _ := args["set"].(map[string]any)
		return editFrontmatter(text, set, args.strs("delete"))
	}
	return ch, "no such write"
}

func splice(text string, s mdspan.Span, with string) (ch artChange, reason string) {
	out, changed, err := mdspan.Splice([]byte(text), s, []byte(with))
	if err != nil {
		return ch, err.Error()
	}
	return artChange{text: string(out), changed: changed}, ""
}

// asLines ends a block of lines with a newline.
func asLines(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

func editWrite(text string) (ch artChange, reason string) {
	text = asLines(text)
	ch.text = text
	ch.changed = mdspan.LineSpan{First: 1, Last: max(1, lineCount(text))}
	return
}

func editReplace(text string, old string, with string, all bool) (ch artChange, reason string) {
	if old == "" {
		return ch, "old_text is empty; artefact_insert adds text, artefact_write replaces all of it"
	}
	n := strings.Count(text, old)
	switch {
	case n == 0:
		return ch, "old_text does not occur in the artefact; read it again and copy the text exactly, whitespace included"
	case n > 1 && !all:
		return ch, "old_text occurs " + strconv.Itoa(n) + " times; give more context so it occurs once, or set replace_all"
	}
	if !all {
		i := strings.Index(text, old)
		return splice(text, mdspan.Span{Start: i, End: i + len(old)}, with)
	}
	out := strings.ReplaceAll(text, old, with)
	idx := mdspan.NewLineIndex([]byte(out))
	first := strings.Index(text, old)
	lastOld := strings.LastIndex(text, old)
	// The last occurrence moved by what the earlier ones grew or shrank.
	lastNew := lastOld + (n-1)*(len(with)-len(old))
	ch = artChange{text: out, changed: mdspan.LineSpan{First: idx.LineOf(first), Last: idx.LineSpanOf(mdspan.Span{Start: lastNew, End: lastNew + len(with)}).Last}}
	return
}

func editInsert(text string, add string, line int, hasLine bool, heading []string, at string) (ch artChange, reason string) {
	if add == "" {
		return ch, "text is empty"
	}
	add = asLines(add)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	src := []byte(text)
	var off int
	switch {
	case hasLine && len(heading) > 0:
		return ch, "give line or heading, not both"
	case hasLine:
		idx := mdspan.NewLineIndex(src)
		if line < 1 || line > idx.Count()+1 {
			return ch, "line must be 1 to " + strconv.Itoa(idx.Count()+1) + " (one past the last line appends)"
		}
		off = idx.LineStart(line)
	case len(heading) > 0:
		d := mdspan.Parse(src)
		i, err := d.FindHeadingE(heading)
		if err != nil {
			return ch, headingReason(err)
		}
		h := d.Headings[i]
		switch at {
		case "", "end":
			off = h.Section.End
			if off < len(src) && !strings.HasSuffix(add, "\n\n") {
				add += "\n"
			}
		case "start":
			off = h.Body.Start
		default:
			return ch, "at is start or end"
		}
	default:
		off = len(src)
	}
	return splice(text, mdspan.Span{Start: off, End: off}, add)
}

func editSection(text string, heading []string, body string, withHeading bool) (ch artChange, reason string) {
	if len(heading) == 0 {
		return ch, "heading is required: the path of the section"
	}
	d := mdspan.Parse([]byte(text))
	i, err := d.FindHeadingE(heading)
	if err != nil {
		return ch, headingReason(err)
	}
	h := d.Headings[i]
	s := h.Body
	if withHeading {
		s = h.Section
	}
	body = asLines(body)
	if s.End < len(text) && body != "" && !strings.HasSuffix(body, "\n\n") {
		// Keep a blank line before the next heading.
		body += "\n"
	}
	if !withHeading && body != "" && s.Start == len(text) && !strings.HasSuffix(text, "\n") {
		body = "\n" + body
	}
	return splice(text, s, body)
}

func editFrontmatter(text string, set map[string]any, del []string) (ch artChange, reason string) {
	if len(set) == 0 && len(del) == 0 {
		return ch, "nothing to set or delete"
	}
	for k := range set {
		if isChatProperty(k) {
			return ch, "the chat keeps the " + chatPropertyPrefix + "* properties; " + k + " cannot be set"
		}
	}
	for _, k := range del {
		if isChatProperty(k) {
			return ch, "the chat keeps the " + chatPropertyPrefix + "* properties; " + k + " cannot be deleted"
		}
	}
	out, changed, err := mdspan.SetFrontmatter([]byte(text), set, del)
	if err != nil {
		return ch, err.Error()
	}
	return artChange{text: string(out), changed: changed}, ""
}

// headingReason is a failed heading lookup as the model reads it: the
// candidates when the path was ambiguous.
func headingReason(err error) string {
	s := err.Error()
	if strings.Contains(s, "ambiguous") {
		return "the heading path matches several headings; name an ancestor too (artefact_outline lists the paths)"
	}
	return "no heading matches that path; artefact_outline lists the headings"
}

// offerArtefact gives the conversation's artefact to the coordinator; nil
// takes it away.
func (inst *coordinator) offerArtefact(art *artefact) {
	inst.mu.Lock()
	inst.art = art
	inst.mu.Unlock()
}

func (inst *coordinator) artefactOf() (art *artefact) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.art
}

// titleNow is the running call's title.
func (inst *coordinator) titleNow() (t string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.curTitle
}

// artefactNote tells the model where the artefact stands at the start of a
// turn, when it moved since the model was last told: a revert in the panel,
// or a turn whose answer never reached the history.
func (inst *coordinator) artefactNote() (note string) {
	art := inst.artefactOf()
	if art == nil {
		return
	}
	n, text := art.head()
	p := art.policyNow()
	inst.mu.Lock()
	if inst.artToldN && inst.artTold == n {
		inst.mu.Unlock()
		return
	}
	inst.artTold, inst.artToldN = n, true
	inst.mu.Unlock()
	if n == 0 {
		note = "The artefact is empty (revision 0)."
	} else {
		o := artOutline(n, text)
		note = "The artefact is at revision " + strconv.Itoa(n) + ", " + plural(o.Lines, "line") + "."
		if len(o.Headings) > 0 {
			hs := make([]string, 0, min(len(o.Headings), 12))
			for i, h := range o.Headings {
				if i == 12 {
					hs = append(hs, "…")
					break
				}
				hs = append(hs, strings.Repeat("#", int(h.Level))+" "+h.Path[len(h.Path)-1]+" ("+strconv.Itoa(h.Line)+")")
			}
			note += " Headings: " + strings.Join(hs, "; ") + "."
		}
	}
	switch {
	case !p.write:
		note += " The person's settings let you read it, not change it."
	case p.ask:
		note += " Each change you make waits for the person to accept it."
	}
	return
}
