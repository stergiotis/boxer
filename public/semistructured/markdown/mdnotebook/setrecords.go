package mdnotebook

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// SetRecords is nb's text with its records taken from a play buffer: the
// way back of [Buffer], for a notebook that has prose and presentation
// cells to keep (ADR-0371 §SD10). Each query of the buffer's WITH list and
// its own SELECT is written into the record cell of its name; the buffer's
// own SELECT is the record sink, the notebook's last record when sink is
// empty. A part no record is named for becomes a record cell after the one
// before it in the buffer.
//
// Everything else keeps its bytes: prose, presentation cells, and a record
// whose statement the buffer holds as the notebook does. So the text of a
// notebook's own buffer is the notebook, and [Buffer] of the result is the
// buffer, statement for statement. The one exception is a WITH list of the
// last record's own: [Buffer] folds it into the buffer's, so it comes back
// as records.
//
// The frontmatter is not touched. A record whose statement changed is stale
// and a new one has no stored result, until the notebook is collected.
//
// A buffer no notebook can hold is refused as [FromBuffer] refuses it. A
// buffer that drops one of nb's records, or orders two of them the other
// way round, is refused naming the record: either is an edit of the text
// around the statements, which this does not make.
func SetRecords(nb *Notebook, sql string, sink string) (text []byte, err error) {
	cells := nb.recordCells()
	if len(cells) == 0 {
		return nil, eh.Errorf("the notebook has no record")
	}
	if sink == "" {
		sink = cells[len(cells)-1].Name
	}
	parts, err := bufferRecords(sql, sink)
	if err != nil {
		return nil, err
	}
	at := make(map[string]int, len(cells))
	for i, c := range cells {
		at[c.Name] = i
	}
	held := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		held[p.name] = struct{}{}
	}
	for _, c := range cells {
		if _, ok := held[c.Name]; !ok {
			return nil, eh.Errorf("the buffer does not hold the record %s: a record is removed in the text, with what reads it", c.Name) //boxer:lint disable=CS013 reason="SetRecords' error is the notebook's refusal text (app.RefuseOperation(err.Error())); the record name is what the reader acts on"
		}
	}

	type edit struct {
		from, to int
		text     string
	}
	var edits []edit
	// insert adds text at an offset; several at one offset keep their order.
	insert := func(offset int, text string) {
		if n := len(edits); n > 0 && edits[n-1].from == offset && edits[n-1].to == offset {
			edits[n-1].text += text
			return
		}
		edits = append(edits, edit{from: offset, to: offset, text: text})
	}
	last := -1
	for _, p := range parts {
		i, known := at[p.name]
		if !known {
			if last < 0 {
				insert(cells[0].Start, recordCellText(p.name, p.statement)+"\n")
				continue
			}
			sep := "\n"
			if stop := cells[last].Stop; stop > 0 && nb.Src[stop-1] != '\n' {
				sep = "\n\n"
			}
			insert(cells[last].Stop, sep+recordCellText(p.name, p.statement))
			continue
		}
		if i < last {
			return nil, eh.Errorf("the buffer has the record %s after %s, and the notebook has it before: records are reordered in the text", p.name, cells[last].Name) //boxer:lint disable=CS013 reason="SetRecords' error is the notebook's refusal text (app.RefuseOperation(err.Error())); the record name is what the reader acts on"
		}
		last = i
		c := cells[i]
		if statementText(c.Body) == p.statement {
			continue
		}
		from, to, replacement, eErr := bodyEdit(nb.Src, c, p.statement)
		if eErr != nil {
			return nil, eErr
		}
		edits = append(edits, edit{from: from, to: to, text: replacement})
	}
	if len(edits) == 0 {
		return append([]byte(nil), nb.Src...), nil
	}
	sort.SliceStable(edits, func(a, b int) bool { return edits[a].from < edits[b].from })
	var b strings.Builder
	pos := 0
	for _, e := range edits {
		b.Write(nb.Src[pos:e.from])
		b.WriteString(e.text)
		pos = e.to
	}
	b.Write(nb.Src[pos:])
	return []byte(b.String()), nil
}

// bodyEdit is the replacement of a record cell's fence, opening line to
// closing line, by one holding statement. The info string and what follows
// the fence — the id — are kept, and the fence grows only when statement
// would close it.
func bodyEdit(src []byte, c *Cell, statement string) (from, to int, replacement string, err error) {
	cell := string(src[c.Start:c.Stop])
	open, rest, found := strings.Cut(cell, "\n")
	if !found || !strings.HasPrefix(rest, c.Body) {
		return 0, 0, "", eh.Errorf("the record %s: its fence could not be found in the text", c.Name) //boxer:lint disable=CS013 reason="SetRecords' error is the notebook's refusal text (app.RefuseOperation(err.Error())); the record name is what the reader acts on"
	}
	after := rest[len(c.Body):]
	closing, _, _ := strings.Cut(after, "\n")
	closing = strings.TrimRight(closing, " \t\r")
	ch := open[0]
	n := len(open) - len(strings.TrimLeft(open, string(ch)))
	if len(closing) < n || strings.Trim(closing, string(ch)) != "" {
		return 0, 0, "", eh.Errorf("the record %s: its fence is not closed", c.Name) //boxer:lint disable=CS013 reason="SetRecords' error is the notebook's refusal text (app.RefuseOperation(err.Error())); the record name is what the reader acts on"
	}
	fence := fenceFor(statement, ch, n)
	from = c.Start
	to = c.Start + len(open) + 1 + len(c.Body) + len(closing)
	replacement = fence + open[n:] + "\n" + statement + "\n" + fence
	return from, to, replacement, nil
}
