package mdnotebook

import (
	"bytes"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
)

// MediaTypeWAV is the one media type a media cell has: what the markdown
// widget draws itself, an image, stays in its prose.
const MediaTypeWAV = "audio/wav"

// RendererTable is the renderer every host draws: the result as a grid.
const RendererTable = "table"

// ValScheme is the scheme of a link to a stored value (ADR-0371 §SD5).
const ValScheme = "val:"

var (
	// A CommonMark image alone on its line: alt, path, optional title.
	reMediaCommon = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)\s"]+)(?:\s+"([^"]*)")?\)[ \t]*$`)
	// An Obsidian embed alone on its line: target, optional alias.
	reMediaEmbed = regexp.MustCompile(`^!\[\[([^\]|]+)(?:\|([^\]]*))?\]\][ \t]*$`)
	// A block id on a line of its own, as the obsidian blockid parser reads it.
	reBlockId = regexp.MustCompile(`^\^([A-Za-z0-9-]+)[ \t]*$`)
	// A link to a stored value: label, record, column, and optionally a
	// gloss token as the link's title.
	reValLink = regexp.MustCompile(`\[([^\]]+)\]\(val:([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)(?:[ \t]+"([^"]*)")?\)`)
	reIdent   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// An ATX heading: its text, without the closing run of `#`.
	reHeading = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+(.*?)[ \t#]*$`)
	// An image's target in prose, in either spelling.
	reImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s"]+)|!\[\[([^\]|]+)`)
)

// imageExts are the extensions of the files a prose cell draws as images:
// the formats the image decoder reads. An embed of anything else — a note,
// a document — is the markdown widget's to draw or not.
var imageExts = map[string]struct{}{".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}}

// line is one source line, its newline excluded.
type line struct {
	start, stop int
}

// Read takes a notebook's text apart. It fails only on frontmatter that is
// not YAML; everything else it can say about the text is a [Problem].
func Read(src []byte) (nb *Notebook, err error) {
	nb = &Notebook{Src: src}
	meta, found, err := obsidian.ParseFrontmatter(src)
	if err != nil {
		return nil, eh.Errorf("read notebook frontmatter: %w", err)
	}
	if found {
		nb.BodyStart = frontmatterEnd(src)
		nb.readMeta(meta)
	}
	nb.scan()
	nb.check()
	return nb, nil
}

// frontmatterEnd is the offset after the closing separator line.
func frontmatterEnd(src []byte) (off int) {
	first := bytes.IndexByte(src, '\n')
	if first < 0 {
		return len(src)
	}
	off = first + 1
	for off < len(src) {
		end := bytes.IndexByte(src[off:], '\n')
		next := len(src)
		if end >= 0 {
			next = off + end + 1
		}
		l := bytes.TrimSpace(src[off:next])
		if len(l) > 0 && len(bytes.Trim(l, "-")) == 0 {
			return next
		}
		off = next
	}
	return len(src)
}

func (inst *Notebook) readMeta(meta map[string]any) {
	inst.Title, _ = meta["title"].(string)
	recs, ok := stringMap(meta["records"])
	if !ok {
		if meta["records"] != nil {
			inst.Problems = append(inst.Problems, Problem{Cell: -1, Text: "frontmatter `records` is not a mapping"})
		}
		return
	}
	// In name order: the records come out sorted, and the problems in the
	// same order on every read.
	names := make([]string, 0, len(recs))
	for name := range recs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ref := RecordRef{Name: name}
		switch t := recs[name].(type) {
		case string:
			ref.Blob = t
		default:
			m, isMap := stringMap(t)
			if !isMap {
				inst.Problems = append(inst.Problems, Problem{Cell: -1, Text: "record " + name + " is neither a path nor a mapping"})
				continue
			}
			ref.Blob, _ = m["blob"].(string)
			ref.Of, _ = m["of"].(string)
		}
		if !reIdent.MatchString(name) {
			inst.Problems = append(inst.Problems, Problem{Cell: -1, Text: "record name " + name + " is not an identifier"})
			continue
		}
		if ref.Blob == "" {
			inst.Problems = append(inst.Problems, Problem{Cell: -1, Text: "record " + name + " names no blob"})
			continue
		}
		inst.Records = append(inst.Records, ref)
	}
}

func sortRecords(records []RecordRef) {
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
}

// stringMap reads a YAML mapping, whichever of the two Go shapes the
// decoder chose for it.
func stringMap(v any) (m map[string]any, ok bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[any]any:
		m = make(map[string]any, len(t))
		for k, val := range t {
			ks, isStr := k.(string)
			if !isStr {
				return nil, false
			}
			m[ks] = val
		}
		return m, true
	}
	return nil, false
}

func splitLines(src []byte, from int) (lines []line) {
	if from < len(src) {
		lines = make([]line, 0, bytes.Count(src[from:], []byte{'\n'})+1)
	}
	off := from
	for off < len(src) {
		end := bytes.IndexByte(src[off:], '\n')
		if end < 0 {
			lines = append(lines, line{off, len(src)})
			break
		}
		stop := off + end
		if stop > off && src[stop-1] == '\r' {
			lines = append(lines, line{off, stop - 1})
		} else {
			lines = append(lines, line{off, stop})
		}
		off = off + end + 1
	}
	return
}

// fenceOpen reads a fence opener: the fence character, its run length and
// the info string. indent is the leading spaces, at most three.
func fenceOpen(text string) (ch byte, n int, indent int, info string, ok bool) {
	for indent < len(text) && text[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent >= len(text) {
		return
	}
	ch = text[indent]
	if ch != '`' && ch != '~' {
		return
	}
	i := indent
	for i < len(text) && text[i] == ch {
		i++
	}
	n = i - indent
	if n < 3 {
		return
	}
	info = strings.TrimSpace(text[i:])
	if ch == '`' && strings.IndexByte(info, '`') >= 0 {
		return
	}
	ok = true
	return
}

// fenceClose reports a line that closes a fence of n ch.
func fenceClose(text string, ch byte, n int) bool {
	t := strings.TrimRight(text, " \t")
	indent := 0
	for indent < len(t) && t[indent] == ' ' {
		indent++
	}
	if indent > 3 {
		return false
	}
	t = t[indent:]
	if len(t) < n {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != ch {
			return false
		}
	}
	return true
}

// scan finds the cells. A cell fence and a media line start in column 0;
// what lies between two of them is one prose cell, other fences included.
func (inst *Notebook) scan() {
	// One copy of the text, so a line is a slice of it and not a string of
	// its own each time it is looked at.
	src := string(inst.Src)
	lines := splitLines(inst.Src, inst.BodyStart)
	text := func(i int) string { return src[lines[i].start:lines[i].stop] }
	blank := func(i int) bool { return strings.TrimSpace(text(i)) == "" }

	proseFirst, proseLast := -1, -1
	flush := func() {
		if proseFirst < 0 {
			return
		}
		inst.Cells = append(inst.Cells, Cell{Kind: CellKindProse, Start: lines[proseFirst].start, Stop: lines[proseLast].stop})
		proseFirst, proseLast = -1, -1
	}
	// trailingId takes a `^id` line after a cell that ended on line last.
	trailingId := func(last int) (id string, end int) {
		end = last
		k := last + 1
		for k < len(lines) && blank(k) {
			k++
		}
		if k >= len(lines) {
			return
		}
		m := reBlockId.FindStringSubmatch(text(k))
		if m == nil || (k+1 < len(lines) && !blank(k+1)) {
			return
		}
		return m[1], k
	}
	cellEnd := -1 // the last line of the cell before the current one
	for i := 0; i < len(lines); {
		t := text(i)
		if ch, n, indent, info, ok := fenceOpen(t); ok {
			j := i + 1
			for j < len(lines) && !fenceClose(text(j), ch, n) {
				j++
			}
			closed := j < len(lines)
			last := j
			if !closed {
				last = len(lines) - 1
			}
			c := Cell{Info: info}
			end := last
			isCell := false
			if indent == 0 {
				inst.Fences = append(inst.Fences, lines[i].start)
				c.Id, end = trailingId(last)
				isCell = classifyFence(&c)
			}
			if !isCell {
				// Not a notebook's fence — indented, or code of any other
				// kind: it is prose's. Skip it whole, so a line in it cannot
				// be taken for a cell.
				if proseFirst < 0 {
					proseFirst = i
				}
				proseLast = last
				i = last + 1
				continue
			}
			flush()
			c.Start, c.Stop = lines[i].start, lines[end].stop
			bodyStop := lines[last].stop
			if closed {
				bodyStop = lines[j].start
			}
			if i+1 <= last {
				c.Body = src[lines[i+1].start:bodyStop]
			}
			inst.Cells = append(inst.Cells, c)
			if !closed {
				inst.Problems = append(inst.Problems, Problem{Cell: len(inst.Cells) - 1, Text: "the fence is not closed"})
			}
			cellEnd = end
			i = end + 1
			continue
		}
		if c, ok := mediaLine(t); ok && (i == 0 || blank(i-1) || i-1 == cellEnd) && (i+1 == len(lines) || blank(i+1) || reBlockId.MatchString(text(i+1))) {
			flush()
			c.Start = lines[i].start
			id, end := trailingId(i)
			c.Id = id
			c.Stop = lines[end].stop
			inst.Cells = append(inst.Cells, c)
			cellEnd = end
			i = end + 1
			continue
		}
		if !blank(i) {
			if proseFirst < 0 {
				proseFirst = i
			}
			proseLast = i
		}
		i++
	}
	flush()
}

// classifyFence reports whether a column-0 fence is a notebook's own: SQL
// with a renderer, or SQL with an id. Every other fence is prose.
func classifyFence(c *Cell) (isCell bool) {
	words := strings.Fields(c.Info)
	if len(words) == 0 || strings.ToLower(words[0]) != "sql" {
		return false
	}
	c.Lang = "sql"
	switch {
	case len(words) == 2:
		c.Kind = CellKindPresentation
		c.Renderer = strings.ToLower(words[1])
	case len(words) == 1 && c.Id != "":
		c.Kind = CellKindRecord
		c.Name = strings.ReplaceAll(c.Id, "-", "_")
	default:
		return false
	}
	return true
}

func mediaLine(text string) (c Cell, ok bool) {
	if m := reMediaCommon.FindStringSubmatch(text); m != nil {
		c = Cell{Kind: CellKindMedia, Alt: m[1], Path: m[2], Title: m[3]}
	} else if m := reMediaEmbed.FindStringSubmatch(text); m != nil {
		c = Cell{Kind: CellKindMedia, Path: strings.TrimSpace(m[1]), Title: strings.TrimSpace(m[2])}
	} else {
		return c, false
	}
	c.MediaType = MediaTypeOf(c.Path)
	return c, c.MediaType != ""
}

// MediaTypeOf reads a media cell's type off a path's extension, "" for a
// path that is not a media cell's.
func MediaTypeOf(p string) (mediaType string) {
	if strings.ToLower(path.Ext(p)) == ".wav" {
		mediaType = MediaTypeWAV
	}
	return
}

// check collects the `val:` links and what the cells and the frontmatter
// disagree on.
func (inst *Notebook) check() {
	seen := make(map[string]int)
	section := ""
	for i := range inst.Cells {
		c := &inst.Cells[i]
		if c.Kind != CellKindProse {
			c.Section = section
		}
		switch c.Kind {
		case CellKindProse:
			text := inst.Src[c.Start:c.Stop]
			if h := lastHeading(text); h != "" {
				section = h
			}
			for _, m := range figures(text) {
				ref := ValRef{Cell: i, Label: string(text[m[2]:m[3]]), Record: string(text[m[4]:m[5]]), Column: string(text[m[6]:m[7]])}
				if m[8] >= 0 {
					ref.Gloss = string(text[m[8]:m[9]])
				}
				inst.Vals = append(inst.Vals, ref)
				if _, found := inst.Record(ref.Record); !found {
					inst.Problems = append(inst.Problems, Problem{Cell: i, Text: "the value " + ref.Record + "." + ref.Column + " names a record the frontmatter does not list", Record: ref.Record})
				}
			}
			for _, p := range images(text) {
				inst.Images = append(inst.Images, ImageRef{Cell: i, Path: p})
			}
		case CellKindRecord:
			if !reIdent.MatchString(c.Name) {
				inst.Problems = append(inst.Problems, Problem{Cell: i, Text: "the id " + c.Id + " does not make a SQL name"})
				continue
			}
			if prev, dup := seen[c.Name]; dup {
				inst.Problems = append(inst.Problems, Problem{Cell: i, Text: "the record " + c.Name + " is also cell " + strconv.Itoa(prev)})
				continue
			}
			seen[c.Name] = i
			if _, found := inst.Record(c.Name); !found {
				inst.Problems = append(inst.Problems, Problem{Cell: i, Text: "the record " + c.Name + " has no stored result", Record: c.Name})
			}
		}
	}
}

// lastHeading is the text of the last heading in a prose cell's text, "" when
// it holds none. A `#` line inside a fenced block is code.
func lastHeading(text []byte) (heading string) {
	lines := splitLines(text, 0)
	for i := 0; i < len(lines); i++ {
		l := text[lines[i].start:lines[i].stop]
		t := bytes.TrimLeft(l, " ")
		if len(t) == 0 {
			continue
		}
		switch t[0] {
		case '`', '~':
			ch, n, _, _, ok := fenceOpen(string(l))
			if !ok {
				continue
			}
			for i++; i < len(lines) && !fenceClose(string(text[lines[i].start:lines[i].stop]), ch, n); i++ {
			}
		case '#':
			if m := reHeading.FindSubmatch(l); m != nil {
				heading = string(m[1])
			}
		}
	}
	return
}

// figures are the `val:` links of a prose cell's text that the markdown
// parse draws as links, each as reValLink's submatch indexes. A link inside
// a fenced block or a code span is text there — a sample of the spelling,
// say — and is no figure: it names no value to check, and collection leaves
// it as it is. Neither is one whose bracket is escaped, which the parse
// draws as the characters it is.
func figures(text []byte) (matches [][]int) {
	for _, m := range outsideCode(text, reValLink.FindAllSubmatchIndex(text, -1)) {
		if !escapedAt(text, m[0]) && !escapedAt(text, m[3]) {
			matches = append(matches, m)
		}
	}
	return
}

// escapedAt reports a character a backslash escapes: one after an odd run
// of them.
func escapedAt(text []byte, at int) bool {
	n := 0
	for at-1-n >= 0 && text[at-1-n] == '\\' {
		n++
	}
	return n%2 == 1
}

// images are the files a prose cell's text draws as images: the targets of
// its image links and embeds that end in an image's extension. An image at
// a URL is not the notebook's to read.
func images(text []byte) (paths []string) {
	for _, m := range outsideCode(text, reImage.FindAllSubmatchIndex(text, -1)) {
		var p string
		if m[2] >= 0 {
			p = string(text[m[2]:m[3]])
		} else {
			p = strings.TrimSpace(string(text[m[4]:m[5]]))
		}
		if _, isImage := imageExts[strings.ToLower(path.Ext(p))]; !isImage || strings.Contains(p, "://") {
			continue
		}
		paths = append(paths, p)
	}
	return
}

// outsideCode keeps the matches, as submatch indexes into text, that do not
// start inside a fenced block or a code span.
func outsideCode(text []byte, all [][]int) (matches [][]int) {
	if len(all) == 0 {
		return nil
	}
	code := codeRanges(text)
	for _, m := range all {
		inCode := false
		for _, r := range code {
			if m[0] >= r[0] && m[0] < r[1] {
				inCode = true
				break
			}
		}
		if !inCode {
			matches = append(matches, m)
		}
	}
	return
}

// codeRanges are the byte ranges of a prose cell's text that are code: its
// fenced blocks, in a list item or a quote as well, and the code spans of
// its paragraphs.
func codeRanges(text []byte) (ranges [][2]int) {
	lines := splitLines(text, 0)
	// A fence in a list item or a quote is indented or marked; what counts
	// here is that it is one, not where it stands.
	bare := func(i int) string { return strings.TrimLeft(string(text[lines[i].start:lines[i].stop]), " \t>") }
	// paraStart is where the run of lines since the last blank line or fence
	// began, -1 for none: a code span does not reach past its paragraph.
	paraStart := -1
	closePara := func(stop int) {
		if paraStart >= 0 {
			ranges = appendCodeSpans(ranges, text, paraStart, stop)
			paraStart = -1
		}
	}
	for i := 0; i < len(lines); i++ {
		t := bare(i)
		if strings.TrimSpace(t) == "" {
			closePara(lines[i].start)
			continue
		}
		if ch, n, _, _, ok := fenceOpen(t); ok {
			closePara(lines[i].start)
			j := i + 1
			for j < len(lines) && !fenceClose(bare(j), ch, n) {
				j++
			}
			stop := len(text)
			if j < len(lines) {
				stop = lines[j].stop
			}
			ranges = append(ranges, [2]int{lines[i].start, stop})
			i = j
			continue
		}
		if paraStart < 0 {
			paraStart = lines[i].start
		}
	}
	closePara(len(text))
	return
}

// appendCodeSpans adds the code spans of text[from:to], one paragraph: a run
// of backticks opens a span that the next run of the same length closes.
func appendCodeSpans(ranges [][2]int, text []byte, from int, to int) [][2]int {
	for i := from; i < to; {
		if text[i] == '\\' {
			i += 2
			continue
		}
		if text[i] != '`' {
			i++
			continue
		}
		open := i
		for i < to && text[i] == '`' {
			i++
		}
		n := i - open
		for k := i; k < to; {
			if text[k] != '`' {
				k++
				continue
			}
			run := k
			for k < to && text[k] == '`' {
				k++
			}
			if k-run == n {
				ranges = append(ranges, [2]int{open, k})
				i = k
				break
			}
		}
	}
	return ranges
}

// ParseVal reads a `val:` link target into the record and column it names.
func ParseVal(url string) (record string, column string, ok bool) {
	rest, isVal := strings.CutPrefix(url, ValScheme)
	if !isVal {
		return
	}
	record, column, ok = strings.Cut(rest, ".")
	ok = ok && reIdent.MatchString(record) && reIdent.MatchString(column)
	return
}
