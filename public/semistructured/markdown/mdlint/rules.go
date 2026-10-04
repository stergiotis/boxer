package mdlint

import (
	"bytes"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"gopkg.in/yaml.v3"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdspan"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/embed"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/wikilink"
)

// rule is a RuleI from a function.
type rule struct {
	id, name, summary string
	check             func(d *mdspan.Doc, r *Reporter)
}

func (inst rule) Id() string                       { return inst.id }
func (inst rule) Name() string                     { return inst.name }
func (inst rule) Summary() string                  { return inst.summary }
func (inst rule) Check(d *mdspan.Doc, r *Reporter) { inst.check(d, r) }

var _ RuleI = rule{}

// AllRules lists every rule of this package, in id order.
func AllRules() []RuleI {
	return []RuleI{
		rule{"ML001", "link-target", "a link into this document names a heading or block that is not there", checkLinkTarget},
		rule{"ML002", "heading-increment", "a heading is more than one level below the heading before it", checkHeadingIncrement},
		rule{"ML003", "duplicate-anchor", "two headings share one anchor, so a link reaches only the first", checkDuplicateAnchor},
		rule{"ML004", "frontmatter", "the frontmatter block is not closed or is not valid YAML", checkFrontmatter},
		rule{"ML005", "unclosed-fence", "a code fence is never closed and swallows the rest of its container", checkUnclosedFence},
		rule{"ML006", "callout-type", "a callout names a type Obsidian does not know; it renders as a note", checkCalloutType},
		rule{"ML007", "footnote-undefined", "a footnote reference has no definition and renders as literal text", checkFootnoteUndefined},
		rule{"ML008", "footnote-unused", "a footnote definition is never referenced and is dropped", checkFootnoteUnused},
		rule{"ML009", "tag", "a tag in the frontmatter is not a valid tag name", checkTag},
		rule{"ML010", "empty-link", "a link has no target", checkEmptyLink},
	}
}

// --- ML001 -------------------------------------------------------------------

var blockIdRe = regexp.MustCompile(`(?m)(?:^|[ \t])\^([A-Za-z0-9-]+)[ \t]*$`)

// blockIds lists the `^id` markers that end a line outside literal text.
func blockIds(d *mdspan.Doc) map[string]bool {
	ids := make(map[string]bool, 4)
	for _, m := range blockIdRe.FindAllSubmatchIndex(d.Src, -1) {
		if d.IsLiteral(m[2]) {
			continue
		}
		ids[string(d.Src[m[2]:m[3]])] = true
	}
	return ids
}

// inlineSpan is an inline node's source range: from its position to the
// closing bracket pair, or the end of the line.
func inlineSpan(d *mdspan.Doc, n ast.Node, closer string) mdspan.Span {
	start := n.Pos()
	if start < 0 {
		start = 0
	}
	lineEnd := d.Index.LineEnd(d.Index.LineOf(start))
	end := lineEnd
	if i := bytes.Index(d.Src[start:lineEnd], []byte(closer)); i >= 0 {
		end = start + i + len(closer)
	}
	return mdspan.Span{Start: start, End: end}
}

func checkLinkTarget(d *mdspan.Doc, r *Reporter) {
	var ids map[string]bool
	resolve := func(fragment string) (ok bool, what string) {
		if strings.HasPrefix(fragment, "^") {
			if ids == nil {
				ids = blockIds(d)
			}
			return ids[fragment[1:]], "block"
		}
		path := strings.Split(fragment, "#")
		_, found, cands := d.FindHeading(path)
		return found || len(cands) > 0, "heading"
	}
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var page, fragment []byte
		closer := "]]"
		switch t := n.(type) {
		case *wikilink.Node:
			page, fragment = t.Page, t.Heading
		case *embed.Node:
			page, fragment = t.Target, t.Heading
		case *ast.Link:
			dest := string(t.Destination)
			if !strings.HasPrefix(dest, "#") || len(dest) == 1 {
				return ast.WalkContinue, nil
			}
			frag, err := url.PathUnescape(dest[1:])
			if err != nil {
				frag = dest[1:]
			}
			if ok, _ := resolve(frag); !ok && !slugExists(d, frag) {
				r.Report(inlineSpan(d, n, ")"), SeverityWarn, "no heading has the anchor #"+frag)
			}
			return ast.WalkContinue, nil
		default:
			return ast.WalkContinue, nil
		}
		if len(page) != 0 || len(fragment) == 0 {
			return ast.WalkContinue, nil
		}
		if ok, what := resolve(string(fragment)); !ok {
			r.Report(inlineSpan(d, n, closer), SeverityWarn, "no "+what+" in this document matches #"+string(fragment))
		}
		return ast.WalkContinue, nil
	})
}

func slugExists(d *mdspan.Doc, frag string) bool {
	s := mdspan.Slug(frag)
	for _, h := range d.Headings {
		if h.Slug == s {
			return true
		}
	}
	return false
}

// --- ML002 / ML003 -----------------------------------------------------------

func checkHeadingIncrement(d *mdspan.Doc, r *Reporter) {
	for i := 1; i < len(d.Headings); i++ {
		prev, h := d.Headings[i-1], d.Headings[i]
		if h.Level > prev.Level+1 {
			r.Report(h.Lines, SeverityWarn, "heading level "+strconv.Itoa(int(h.Level))+" follows level "+strconv.Itoa(int(prev.Level))+"; a level is skipped")
		}
	}
}

func checkDuplicateAnchor(d *mdspan.Doc, r *Reporter) {
	first := make(map[string]int, len(d.Headings))
	for i, h := range d.Headings {
		if j, dup := first[h.Slug]; dup {
			r.Report(h.Lines, SeverityWarn, "the anchor #"+h.Slug+" is already taken by the heading on line "+strconv.Itoa(d.Headings[j].Line))
			continue
		}
		first[h.Slug] = i
	}
}

// --- ML004 -------------------------------------------------------------------

var yamlLineRe = regexp.MustCompile(`line (\d+)`)

func checkFrontmatter(d *mdspan.Doc, r *Reporter) {
	if !d.HasFrontmatter() {
		if d.Index.Count() > 0 && string(bytes.TrimRight(d.Src[:d.Index.LineEnd(1)], " \t\r\n")) == "---" {
			r.Report(d.Index.Lines(mdspan.LineSpan{First: 1, Last: 1}), SeverityError, "the frontmatter opened on line 1 is never closed with ---")
		}
		return
	}
	body := d.Src[d.FrontmatterBody.Start:d.FrontmatterBody.End]
	var v any
	err := yaml.Unmarshal(body, &v)
	if err == nil {
		if _, isMap := v.(map[string]any); !isMap && v != nil {
			r.Report(d.FrontmatterBody, SeverityError, "frontmatter is YAML but not a mapping of properties")
		}
		return
	}
	s := d.FrontmatterBody
	if m := yamlLineRe.FindStringSubmatch(err.Error()); m != nil {
		if n, perr := strconv.Atoi(m[1]); perr == nil {
			line := d.Index.LineOf(d.FrontmatterBody.Start) + n - 1
			s = d.Index.Lines(mdspan.LineSpan{First: line, Last: line})
		}
	}
	r.Report(s, SeverityError, "frontmatter is not valid YAML: "+strings.TrimPrefix(err.Error(), "yaml: "))
}

// --- ML005 / ML006 -----------------------------------------------------------

func checkUnclosedFence(d *mdspan.Doc, r *Reporter) {
	for _, cb := range d.CodeBlocks {
		if cb.Fenced && !cb.IsClosed {
			open := d.Index.Lines(mdspan.LineSpan{First: cb.Line, Last: cb.Line})
			r.Report(open, SeverityError, "the code fence opened here is never closed")
		}
	}
}

// calloutTypes are the types Obsidian styles, aliases included.
var calloutTypes = map[string]bool{
	"note": true, "abstract": true, "summary": true, "tldr": true, "info": true, "todo": true,
	"tip": true, "hint": true, "important": true, "success": true, "check": true, "done": true,
	"question": true, "help": true, "faq": true, "warning": true, "caution": true, "attention": true,
	"failure": true, "fail": true, "missing": true, "danger": true, "error": true, "bug": true,
	"example": true, "quote": true, "cite": true,
}

func checkCalloutType(d *mdspan.Doc, r *Reporter) {
	for _, c := range d.Callouts {
		if !calloutTypes[strings.ToLower(c.Type)] {
			r.Report(d.Index.Lines(mdspan.LineSpan{First: c.Line, Last: c.Line}), SeverityInfo, "callout type "+strconv.Quote(c.Type)+" is not one Obsidian knows; it renders as a note")
		}
	}
}

// --- ML007 / ML008 -----------------------------------------------------------

var (
	footnoteDefRe = regexp.MustCompile(`(?m)^[ ]{0,3}\[\^([^\]\s]+)\]:`)
	footnoteRefRe = regexp.MustCompile(`\[\^([^\]\s]+)\]`)
)

// Footnote is one footnote reference or definition.
type Footnote struct {
	Label string
	Span  mdspan.Span
}

// Footnotes are a document's footnote definitions and references, in source
// order, outside literal text.
type Footnotes struct {
	Defs []Footnote
	Refs []Footnote
}

// ScanFootnotes reads footnotes from the source: goldmark drops a reference
// without a definition and a definition without a reference before the tree
// is built, which are exactly the two cases ML007 and ML008 report.
func ScanFootnotes(d *mdspan.Doc) (fn Footnotes) {
	defAt := make(map[int]bool, 4)
	for _, m := range footnoteDefRe.FindAllSubmatchIndex(d.Src, -1) {
		if d.IsLiteral(m[2]) {
			continue
		}
		fn.Defs = append(fn.Defs, Footnote{Label: string(d.Src[m[2]:m[3]]), Span: mdspan.Span{Start: m[2] - 2, End: m[1]}})
		defAt[m[2]-2] = true
	}
	for _, m := range footnoteRefRe.FindAllSubmatchIndex(d.Src, -1) {
		if defAt[m[0]] || d.IsLiteral(m[0]) {
			continue
		}
		fn.Refs = append(fn.Refs, Footnote{Label: string(d.Src[m[2]:m[3]]), Span: mdspan.Span{Start: m[0], End: m[1]}})
	}
	return
}

func labels(fs []Footnote) map[string]bool {
	m := make(map[string]bool, len(fs))
	for _, f := range fs {
		m[f.Label] = true
	}
	return m
}

func checkFootnoteUndefined(d *mdspan.Doc, r *Reporter) {
	fn := ScanFootnotes(d)
	defined := labels(fn.Defs)
	for _, ref := range fn.Refs {
		if !defined[ref.Label] {
			r.Report(ref.Span, SeverityWarn, "footnote [^"+ref.Label+"] has no definition")
		}
	}
}

func checkFootnoteUnused(d *mdspan.Doc, r *Reporter) {
	fn := ScanFootnotes(d)
	used := labels(fn.Refs)
	seen := make(map[string]bool, len(fn.Defs))
	for _, def := range fn.Defs {
		if !used[def.Label] && !seen[def.Label] {
			r.Report(def.Span, SeverityInfo, "footnote [^"+def.Label+"] is defined but never referenced")
		}
		seen[def.Label] = true
	}
}

// --- ML009 / ML010 -----------------------------------------------------------

func checkTag(d *mdspan.Doc, r *Reporter) {
	if !d.HasFrontmatter() {
		return
	}
	var fm struct {
		Tags any `yaml:"tags"`
	}
	if yaml.Unmarshal(d.Src[d.FrontmatterBody.Start:d.FrontmatterBody.End], &fm) != nil {
		return
	}
	var tags []string
	switch t := fm.Tags.(type) {
	case string:
		tags = strings.FieldsFunc(t, func(c rune) bool { return c == ',' || c == ' ' })
	case []any:
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				r.Report(d.FrontmatterBody, SeverityWarn, "a tags entry is not a string")
				continue
			}
			tags = append(tags, s)
		}
	}
	for _, tg := range tags {
		if _, ok := mdextract.NormalizeTag(tg); !ok {
			r.Report(tagSpan(d, tg), SeverityWarn, "tag "+strconv.Quote(tg)+" is not a valid tag name: letters, digits, _ - and /, not all digits")
		}
	}
}

// tagSpan locates a frontmatter value, falling back to the whole block.
func tagSpan(d *mdspan.Doc, tg string) mdspan.Span {
	body := d.Src[d.FrontmatterBody.Start:d.FrontmatterBody.End]
	if i := bytes.Index(body, []byte(tg)); i >= 0 && tg != "" {
		return mdspan.Span{Start: d.FrontmatterBody.Start + i, End: d.FrontmatterBody.Start + i + len(tg)}
	}
	return d.FrontmatterBody
}

func checkEmptyLink(d *mdspan.Doc, r *Reporter) {
	_ = ast.Walk(d.Root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.Link:
			if len(bytes.TrimSpace(t.Destination)) == 0 {
				r.Report(inlineSpan(d, n, ")"), SeverityWarn, "link has no target")
			}
		case *ast.Image:
			if len(bytes.TrimSpace(t.Destination)) == 0 {
				r.Report(inlineSpan(d, n, ")"), SeverityWarn, "image has no source")
			}
		case *wikilink.Node:
			if len(t.Page) == 0 && len(t.Heading) == 0 {
				r.Report(inlineSpan(d, n, "]]"), SeverityWarn, "wikilink has no target")
			}
		}
		return ast.WalkContinue, nil
	})
}
