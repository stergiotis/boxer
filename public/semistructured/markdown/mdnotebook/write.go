package mdnotebook

import (
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// reRenderer is a renderer's name as a fence's info string holds it.
var reRenderer = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Writer writes a notebook's text cell by cell, for a programme that emits
// one. It keeps what [Read]'s line scan depends on: a cell starts in column
// 0 with a blank line before it, its fence is one its statement cannot
// close, and text given as data is escaped, so no value can open a cell, a
// figure or a link. The zero value is an empty notebook.
//
// A name the text cannot hold — a renderer that is not one word, a record
// that is not an identifier — is not written; [Writer.Err] reports the
// first.
type Writer struct {
	b   []byte
	err error
}

// Reset empties the writer and keeps its buffer.
func (inst *Writer) Reset() {
	inst.b = inst.b[:0]
	inst.err = nil
}

// Bytes is the text written so far. It is the writer's buffer, valid until
// the next call.
func (inst *Writer) Bytes() (text []byte) { return inst.b }

// Err is the first thing that could not be written, nil when everything
// was.
func (inst *Writer) Err() (err error) { return inst.err }

// block starts a block: a blank line between it and what was written.
func (inst *Writer) block() {
	if len(inst.b) == 0 {
		return
	}
	if inst.b[len(inst.b)-1] != '\n' {
		inst.b = append(inst.b, '\n')
	}
	inst.b = append(inst.b, '\n')
}

// Heading writes a heading of the level, 1 to 6, whose text is data.
func (inst *Writer) Heading(level int, text string) {
	inst.block()
	inst.b = append(inst.b, "######"[:min(max(level, 1), 6)]...)
	inst.b = append(inst.b, ' ')
	inst.b = appendEscaped(inst.b, text)
	inst.b = append(inst.b, '\n')
}

// Paragraph writes a paragraph whose text is data: it is drawn as written,
// on one line, whatever markdown it spells.
func (inst *Writer) Paragraph(text string) {
	inst.block()
	inst.b = appendEscaped(inst.b, text)
	inst.b = append(inst.b, '\n')
}

// Markdown writes prose as its writer spelled it: markdown the caller
// vouches for, figures ([Figure]) and escaped values ([Escape]) included. A
// cell written into it is a cell.
func (inst *Writer) Markdown(markdown string) {
	inst.block()
	inst.b = append(inst.b, strings.Trim(markdown, "\n")...)
	inst.b = append(inst.b, '\n')
}

// Cell writes a presentation cell: a statement over the notebook's records,
// drawn by the renderer named.
func (inst *Writer) Cell(renderer string, statement string) {
	if !reRenderer.MatchString(renderer) {
		inst.fail(eb.Build().Str("renderer", renderer).Errorf("a renderer's name is one lower-case word"))
		return
	}
	statement = strings.Trim(statement, "\n")
	fence := fenceFor(statement, '`', 3)
	inst.block()
	inst.b = append(inst.b, fence...)
	inst.b = append(inst.b, "sql "...)
	inst.b = append(inst.b, renderer...)
	inst.b = append(inst.b, '\n')
	inst.b = append(inst.b, statement...)
	inst.b = append(inst.b, '\n')
	inst.b = append(inst.b, fence...)
	inst.b = append(inst.b, '\n')
}

// Record writes a record cell: a statement and the name its result is read
// under.
func (inst *Writer) Record(name string, statement string) {
	if !reIdent.MatchString(name) {
		inst.fail(eb.Build().Str("record", name).Errorf("a record's name is an identifier"))
		return
	}
	inst.block()
	inst.b = append(inst.b, recordCellText(name, strings.Trim(statement, "\n"))...)
}

func (inst *Writer) fail(err error) {
	if inst.err == nil {
		inst.err = err
	}
}

// Escape is text as markdown that draws it as written, on one line: every
// character markdown gives a meaning is escaped and a line break is a
// space. It is how a value enters prose ([Writer.Markdown]).
func Escape(text string) (markdown string) {
	return string(appendEscaped(nil, text))
}

// escaped marks the ASCII punctuation markdown may read as markup, or set
// as other punctuation, wherever it stands. A backslash before any ASCII
// punctuation is that character.
var escaped = [128]bool{
	'\\': true, '`': true, '*': true, '_': true, '[': true, ']': true, '<': true, '>': true,
	'#': true, '|': true, '!': true, '~': true, '$': true, '&': true, '=': true, '%': true,
	'^': true, '{': true, '}': true, '\'': true, '"': true,
}

func appendEscaped(b []byte, text string) []byte {
	text = strings.TrimSpace(text)
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case ch == '\n' || ch == '\r' || ch == '\t':
			b = append(b, ' ')
			continue
		case ch < 128 && escaped[ch]:
			b = append(b, '\\')
		case i == 0 && (ch == '-' || ch == '+'):
			// A list item, a rule or a setext underline at the start of a line.
			b = append(b, '\\')
		case (ch == '-' || ch == '.') && i+1 < len(text) && text[i+1] == ch:
			// A run a typesetter would set as a dash or an ellipsis.
			b = append(b, '\\')
		case (ch == '.' || ch == ')') && i > 0 && digits(text[:i]):
			// An ordered list item.
			b = append(b, '\\')
		}
		b = append(b, ch)
	}
	return b
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FigureLabel is the label [Figure] writes: a placeholder until the
// notebook is bound or sealed, when the value's rendering takes its place.
const FigureLabel = "…"

// Figure is a figure in a sentence: a link to the value of a column of a
// record of one row, shown through the gloss token when one is given. It
// is "" when the names are not ones a link can hold.
func Figure(record string, column string, token string) (markdown string) {
	if !reIdent.MatchString(record) || !reIdent.MatchString(column) || strings.ContainsAny(token, "\"\n") {
		return ""
	}
	markdown = "[" + FigureLabel + "](" + ValScheme + record + "." + column
	if token != "" {
		markdown += ` "` + token + `"`
	}
	return markdown + ")"
}

// Alias is a column's name in a statement, quoted: the label a reader sees
// and, when one is given, the gloss token its values are shown through.
// `SELECT lag AS ` + Alias("p99", "gloss/duration;unit=ms") + ` FROM stats`
// is how a cell names a column for its renderer.
func Alias(label string, token string) (identifier string) {
	if token != "" {
		label += gloss.Sep + token
	}
	return nanopass.QuoteIdentifier(label)
}
