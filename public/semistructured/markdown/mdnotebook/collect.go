package mdnotebook

import (
	"bytes"
	"context"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"gopkg.in/yaml.v2"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RunFunc runs one statement on a ClickHouse server and returns the body it
// answered with. The statement ends in `FORMAT ArrowStream`.
type RunFunc func(ctx context.Context, statement string) (arrowStream []byte, err error)

// recordCells are the notebook's record cells in document order.
func (inst *Notebook) recordCells() (cells []*Cell) {
	for i := range inst.Cells {
		if inst.Cells[i].Kind == CellKindRecord {
			cells = append(cells, &inst.Cells[i])
		}
	}
	return
}

// recordStatement is a record's statement, checked to be one SELECT with no
// SET before it and no FORMAT after it, and where its own WITH list starts.
type recordStatement struct {
	name string
	text string
	// withEnd is the offset in text after the statement's own WITH keyword,
	// -1 when it has no WITH list.
	withEnd int
}

// statementText is a record's statement as its fence holds it, without the
// white space around it and the `;` that may end it. It is the text that is
// parsed, sent and hashed, by collection and by the reader alike: a hash of
// anything else would call a record stale that was never edited.
func statementText(body string) (text string) {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), ";"))
}

func readRecordStatement(c *Cell) (st recordStatement, err error) {
	text := statementText(c.Body)
	st = recordStatement{name: c.Name, text: text, withEnd: -1}
	fail := func(what string) (recordStatement, error) {
		return st, eb.Build().Str("record", c.Name).Errorf("a record is one SELECT: %s", what)
	}
	if text == "" {
		return fail("the fence is empty")
	}
	pr, pErr := nanopass.Parse(text)
	if pErr != nil {
		return st, eb.Build().Str("record", c.Name).Errorf("the record's statement does not parse: %w", pErr)
	}
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok || qs.Query() == nil {
		return fail("it is another kind of statement")
	}
	if qs.FORMAT() != nil {
		return fail("it names a FORMAT, which collection chooses")
	}
	q := qs.Query().(*grammar1.QueryContext)
	if len(q.AllSetStmt()) > 0 {
		return fail("it starts with SET, and a notebook holds no session state")
	}
	su, ok := q.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
	if !ok || su == nil {
		return fail("it is another kind of statement")
	}
	if c := su.Ctes(); c != nil {
		cc := c.(*grammar1.CtesContext)
		if cc.RECURSIVE() != nil {
			return fail("WITH RECURSIVE cannot follow the records before it in one WITH list")
		}
		// Token offsets count runes.
		stop := cc.WITH().GetSymbol().GetStop()
		st.withEnd = len(string([]rune(text)[:stop+1]))
	}
	return st, nil
}

// compose is the statement that computes record i: its own statement, with
// the records before it as WITH queries ahead of its own WITH list. The
// record's statement stays the outermost SELECT, so an ORDER BY of its own
// orders what is stored.
func compose(sts []recordStatement, i int) (sql string) {
	st := sts[i]
	if i == 0 {
		return st.text
	}
	var b strings.Builder
	b.WriteString("WITH ")
	for j := 0; j < i; j++ {
		if j > 0 {
			b.WriteString(",\n")
		}
		// A record name is a plain identifier (reIdent); quoted, a keyword
		// is a name too.
		b.WriteString("`" + sts[j].name + "`")
		b.WriteString(" AS (\n")
		b.WriteString(sts[j].text)
		b.WriteString("\n)")
	}
	if st.withEnd >= 0 {
		b.WriteString(",")
		b.WriteString(st.text[st.withEnd:])
	} else {
		b.WriteString("\n")
		b.WriteString(st.text)
	}
	return b.String()
}

// Buffer is the notebook's records as one statement: the play buffer of
// ADR-0371 §SD6. Each record is a WITH query in document order and the last
// is the statement's own SELECT, so the buffer shows the last record and
// play's graph holds them all.
func Buffer(nb *Notebook) (sql string, err error) {
	sts, err := recordStatements(nb)
	if err != nil {
		return "", err
	}
	if len(sts) == 0 {
		return "", eh.Errorf("the notebook has no record")
	}
	return compose(sts, len(sts)-1), nil
}

func recordStatements(nb *Notebook) (sts []recordStatement, err error) {
	seen := make(map[string]struct{})
	for _, c := range nb.recordCells() {
		if !reIdent.MatchString(c.Name) {
			return nil, eb.Build().Str("id", c.Id).Errorf("the record's id does not make a SQL name")
		}
		if _, dup := seen[c.Name]; dup {
			return nil, eb.Build().Str("record", c.Name).Errorf("two records of one name")
		}
		seen[c.Name] = struct{}{}
		st, sErr := readRecordStatement(c)
		if sErr != nil {
			return nil, sErr
		}
		sts = append(sts, st)
	}
	return sts, nil
}

// Collected is a collected notebook: what to write.
type Collected struct {
	// Text is the notebook's text with its frontmatter listing the new
	// blobs and every figure's label the rendering of its new value.
	Text []byte
	// Blobs are the stored results by path, relative to the notebook. A
	// result whose bytes did not change keeps its path.
	Blobs map[string][]byte
	// Records are the records as the new frontmatter lists them, sorted by
	// name.
	Records []RecordRef
}

// Collect computes every record of nb on the server, once, and returns the
// notebook sealed over the results (ADR-0371 §SD8). blobDir is where the
// blobs go, relative to the notebook. Nothing is written: the caller writes
// the blobs, then the text.
//
// Each result is stored as an Arrow stream of one batch, whatever the
// server streamed it in. A frontmatter entry with no cell — a relation
// stored by other means — is kept as it is.
func Collect(ctx context.Context, nb *Notebook, blobDir string, run RunFunc) (out *Collected, err error) {
	sts, err := recordStatements(nb)
	if err != nil {
		return nil, err
	}
	records := make(map[string]arrow.RecordBatch)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	for i, st := range sts {
		var sql string
		sql, err = passes.SetFormat("ArrowStream").Run(compose(sts, i))
		if err != nil {
			return nil, eb.Build().Str("record", st.name).Errorf("compose the record's statement: %w", err)
		}
		var stream []byte
		stream, err = run(ctx, sql)
		if err != nil {
			return nil, eb.Build().Str("record", st.name).Errorf("collect the record: %w", err)
		}
		var rec arrow.RecordBatch
		rec, err = oneBatch(stream)
		if err != nil {
			return nil, eb.Build().Str("record", st.name).Errorf("read the record's result: %w", err)
		}
		records[st.name] = rec
	}
	return Seal(nb, records, blobDir, nil)
}

// Seal is nb sealed over records: the half of [Collect] that stores, for
// relations that were computed by other means — a notebook bound to what
// its host held ([Bind]). Each record is a blob under blobDir, relative to
// the notebook, and an entry of the frontmatter, with the hash of its
// cell's statement when it has a cell; every figure's label is the
// rendering of its value in cat, nil for [gloss.Default]. A frontmatter
// entry records does not name is kept as it is. Nothing is written: the
// caller writes the blobs, then the text.
//
// A record is stored as an Arrow stream of one batch without schema
// metadata, so the same rows are the same blob whoever stores them.
func Seal(nb *Notebook, records map[string]arrow.RecordBatch, blobDir string, cat *gloss.Catalog) (out *Collected, err error) {
	// The records that have a cell in document order, then the rest by name.
	names := make([]string, 0, len(records))
	of := make(map[string]string, len(records))
	for _, c := range nb.recordCells() {
		if _, given := records[c.Name]; !given {
			continue
		}
		if _, dup := of[c.Name]; !dup {
			names = append(names, c.Name)
			of[c.Name] = StatementHash(statementText(c.Body))
		}
	}
	cellless := len(names)
	for name := range records {
		if _, hasCell := of[name]; !hasCell {
			if !reIdent.MatchString(name) {
				return nil, eb.Build().Str("record", name).Errorf("the record's name is not an identifier")
			}
			names = append(names, name)
		}
	}
	sort.Strings(names[cellless:])

	out = &Collected{Blobs: make(map[string][]byte, len(names))}
	for _, name := range names {
		var blob []byte
		blob, err = encodeRecord(records[name])
		if err != nil {
			return nil, eb.Build().Str("record", name).Errorf("encode the record: %w", err)
		}
		p := path.Join(blobDir, BlobHash(blob)+RecordExt)
		out.Blobs[p] = blob
		out.Records = append(out.Records, RecordRef{Name: name, Blob: p, Of: of[name]})
	}
	listed := append([]RecordRef(nil), out.Records...)
	for _, ref := range nb.Records {
		if _, redone := records[ref.Name]; !redone {
			listed = append(listed, ref)
			out.Records = append(out.Records, ref)
		}
	}
	sortRecords(out.Records)
	body, _, err := relabel(nb, records, cat, true)
	if err != nil {
		return nil, err
	}
	out.Text = append(writeFrontmatter(nb.Src[:nb.BodyStart], listed), body...)
	return out, nil
}

// Relabel is nb's text with every figure's label the rendering of its value
// in records: what a reader of a bound notebook draws, where the label as
// written is a placeholder ([Bind]). cat is the catalog the glosses are
// resolved in, nil for [gloss.Default]. A figure whose value cannot be read,
// or reads as text a link's label cannot hold, keeps its label. changed is
// false, and text is nb's own, when no label differs.
func Relabel(nb *Notebook, records map[string]arrow.RecordBatch, cat *gloss.Catalog) (text []byte, changed bool) {
	if len(nb.Vals) == 0 {
		return nb.Src, false
	}
	body, changed, _ := relabel(nb, records, cat, false)
	if !changed {
		return nb.Src, false
	}
	return append(append(make([]byte, 0, nb.BodyStart+len(body)), nb.Src[:nb.BodyStart]...), body...), true
}

// oneBatch reads an Arrow stream of any number of batches as one record.
func oneBatch(stream []byte) (rec arrow.RecordBatch, err error) {
	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	if err != nil {
		return nil, eh.Errorf("open arrow stream: %w", err)
	}
	defer rdr.Release()
	var batches []arrow.RecordBatch
	defer func() {
		for _, b := range batches {
			b.Release()
		}
	}()
	for rdr.Next() {
		b := rdr.RecordBatch()
		b.Retain()
		batches = append(batches, b)
	}
	if err = rdr.Err(); err != nil {
		return nil, eh.Errorf("read arrow stream: %w", err)
	}
	// Schema metadata is the writer's, not the data's.
	schema := arrow.NewSchema(rdr.Schema().Fields(), nil)
	if len(batches) == 1 {
		b := batches[0]
		return array.NewRecordBatch(schema, b.Columns(), b.NumRows()), nil
	}
	cols := make([]arrow.Array, schema.NumFields())
	defer func() {
		for _, c := range cols {
			if c != nil {
				c.Release()
			}
		}
	}()
	var rows int64
	for i, f := range schema.Fields() {
		if len(batches) == 0 {
			cols[i] = array.MakeArrayOfNull(memory.DefaultAllocator, f.Type, 0)
			continue
		}
		chunks := make([]arrow.Array, len(batches))
		for j, b := range batches {
			chunks[j] = b.Column(i)
		}
		cols[i], err = array.Concatenate(chunks, memory.DefaultAllocator)
		if err != nil {
			return nil, eb.Build().Str("column", f.Name).Errorf("join the stream's batches: %w", err)
		}
		rows = int64(cols[i].Len())
	}
	return array.NewRecordBatch(schema, cols, rows), nil
}

// encodeRecord writes a record as a blob: an Arrow stream of one batch.
// Schema metadata is the writer's, not the data's, and is not stored.
func encodeRecord(rec arrow.RecordBatch) (blob []byte, err error) {
	if rec.Schema().Metadata().Len() > 0 {
		rec = array.NewRecordBatch(arrow.NewSchema(rec.Schema().Fields(), nil), rec.Columns(), rec.NumRows())
		defer rec.Release()
	}
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()))
	if err = w.Write(rec); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// relabel is the notebook's body with every figure's label the rendering
// of its value in records. A figure of a record that is not among them
// keeps its label. strict fails on a figure whose value cannot be read or
// held by a label; otherwise such a figure keeps its label too.
func relabel(nb *Notebook, records map[string]arrow.RecordBatch, cat *gloss.Catalog, strict bool) (body []byte, changed bool, err error) {
	if cat == nil && len(nb.Vals) > 0 {
		cat = gloss.Default()
	}
	var b bytes.Buffer
	at := nb.BodyStart
	for i := range nb.Cells {
		c := &nb.Cells[i]
		if c.Kind != CellKindProse {
			continue
		}
		text := nb.Src[c.Start:c.Stop]
		for _, m := range figures(text) {
			record, column := string(text[m[4]:m[5]]), string(text[m[6]:m[7]])
			if _, found := records[record]; !found {
				continue
			}
			token := ""
			if m[8] >= 0 {
				token = string(text[m[8]:m[9]])
			}
			v, vErr := ValueOf(cat, records, record, column, token)
			if vErr != nil {
				if !strict {
					continue
				}
				return nil, false, eb.Build().Int("cell", i).Errorf("the figure %s.%s: %w", record, column, vErr)
			}
			// An empty label would leave a link the reader no longer finds
			// (reValLink), so the figure would be lost to every later check.
			if v.Face.Text == "" || strings.ContainsAny(v.Face.Text, "[]\n") {
				if !strict {
					continue
				}
				return nil, false, eb.Build().Int("cell", i).Str("value", v.Face.Text).Errorf("the figure %s.%s reads as text a link's label cannot hold", record, column)
			}
			changed = changed || v.Face.Text != string(text[m[2]:m[3]])
			b.Write(nb.Src[at : c.Start+m[2]])
			b.WriteString(v.Face.Text)
			at = c.Start + m[3]
		}
	}
	b.Write(nb.Src[at:])
	return b.Bytes(), changed, nil
}

// yamlScalar is s as a value of a YAML flow mapping: plain where the
// frontmatter's decoder reads that back as the string s, double-quoted
// otherwise. A blob's path follows the notebook's file name, which may hold
// a comma, a `#` or a colon; plain, such a path ends the value early or the
// mapping with it, and the notebook no longer reads.
func yamlScalar(s string) (text string) {
	var m map[string]any
	if err := yaml.Unmarshal([]byte("{k: "+s+"}"), &m); err == nil && len(m) == 1 {
		if got, isString := m["k"].(string); isString && got == s {
			return s
		}
	}
	return strconv.Quote(s)
}

// writeFrontmatter is the frontmatter block head with its `records` entry
// replaced by records, in the order given. head is empty for a notebook
// that has none.
func writeFrontmatter(head []byte, records []RecordRef) (out []byte) {
	var entry bytes.Buffer
	entry.WriteString("records:\n")
	for _, r := range records {
		entry.WriteString("  " + r.Name + ": {blob: " + yamlScalar(r.Blob))
		if r.Of != "" {
			entry.WriteString(", of: " + yamlScalar(r.Of))
		}
		entry.WriteString("}\n")
	}
	if len(head) == 0 {
		return append([]byte("---\n"+entry.String()+"---\n"), '\n')
	}
	lines := bytes.SplitAfter(head, []byte("\n"))
	var b bytes.Buffer
	written := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		closing := i > 0 && len(bytes.TrimSpace(l)) > 0 && len(bytes.Trim(bytes.TrimSpace(l), "-")) == 0
		switch {
		case bytes.HasPrefix(l, []byte("records:")):
			// The entry runs to the next key in column 0.
			for i+1 < len(lines) && (len(bytes.TrimSpace(lines[i+1])) == 0 || lines[i+1][0] == ' ' || lines[i+1][0] == '\t') {
				i++
			}
			b.Write(entry.Bytes())
			written = true
		case closing && !written:
			b.Write(entry.Bytes())
			b.Write(l)
			written = true
		default:
			b.Write(l)
		}
	}
	return b.Bytes()
}
