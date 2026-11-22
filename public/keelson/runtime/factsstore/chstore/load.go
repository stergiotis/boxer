package chstore

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/dml"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// DefaultLoadBlockRows is how many rows one insert of a load from a table
// carries unless the caller says otherwise. Facts rows are wide — every
// section's parallel arrays — so a block is kept well below the server's
// own default.
const DefaultLoadBlockRows = 20_000

// LoadOptions says what [LoadFacts] reads and how.
type LoadOptions struct {
	// FromTable is a database-qualified relation holding facts rows — a
	// facts table moved aside, typically. Exactly one of FromTable and
	// FromStream is set.
	FromTable string
	// FromStream is an Arrow IPC stream of facts-shaped batches, as the IPC
	// capture executor writes one.
	FromStream io.Reader
	// Since and Until bound the timestamp of the rows read from FromTable:
	// Since inclusive, Until exclusive, the zero value of each no bound.
	// They are how a large table is loaded in parts.
	Since time.Time
	Until time.Time
	// BlockRows is the largest number of rows one insert carries when
	// reading FromTable; zero is [DefaultLoadBlockRows].
	BlockRows int
	// Append loads although the facts already hold rows in the window. It
	// is off by default because a load that ran before would land twice.
	Append bool
	// Progress, when not nil, is told after each insert.
	Progress func(LoadReport)
}

// LoadReport is what a load did.
type LoadReport struct {
	// WrittenTo is the table the inserts named; where an insert transform
	// is registered for it, the rows went to the transform's table.
	WrittenTo string
	// ReadThrough is the relation the before and after counts were read
	// from.
	ReadThrough string
	// Before and After are that relation's rows in the window, before the
	// load and after it.
	Before uint64
	After  uint64
	// Rows and Batches are what was inserted.
	Rows    uint64
	Batches uint64
}

// LoadFacts sends facts-shaped rows through the insert seam into cfg's
// facts table: rows of another relation that holds the facts columns, or
// the batches of an Arrow IPC capture. Because the rows pass the executor,
// an insert transform registered for the facts table applies to them
// (ADR-0402 §SD2), which is the point: it is how rows written before a
// deployment adopted its own table reach that table through the same Go
// mapping every later row takes, and how a capture written without a
// server is loaded.
//
// Rows read from a relation come back from the server in its Arrow
// rendering of the column types, which is not the one the stores build —
// bytes arrive as strings, timestamps carry a zone. Each batch is brought
// to the stores' schema first, by reinterpretation and without touching a
// value, so a transform sees the batches it sees from a store. A column
// the stores' schema has and the source lacks, or holds under a type that
// is not a reinterpretation away, fails the load before anything is
// written.
//
// The rows keep their ids. Nothing makes a load idempotent: a second load
// of the same rows lands them again, so LoadFacts first counts the facts in
// the window and refuses when there are any, unless opts.Append. After the
// load it counts again, and a difference other than the rows it inserted is
// an error — a transform that drops rows, or a read relation that does not
// return what the transform wrote.
func LoadFacts(ctx context.Context, exec recordstore.ExecutorI, cfg Config, opts LoadOptions) (report LoadReport, err error) {
	if (opts.FromTable == "") == (opts.FromStream == nil) {
		err = eh.Errorf("chstore: load: name exactly one source, a table or a stream")
		return
	}
	if cfg.Database == "" || cfg.Table == "" {
		err = eh.Errorf("chstore: load: cfg requires Database + Table")
		return
	}
	report.WrittenTo = cfg.Database + "." + cfg.Table
	report.ReadThrough = cfg.ReadTable
	if report.ReadThrough == "" {
		report.ReadThrough = report.WrittenTo
	}
	if err = recordstore.CheckTableRef(report.ReadThrough); err != nil {
		err = eh.Errorf("chstore: load: read table: %w", err)
		return
	}
	window := ""
	if opts.FromTable != "" {
		if err = recordstore.CheckTableRef(opts.FromTable); err != nil {
			err = eh.Errorf("chstore: load: source: %w", err)
			return
		}
		from, _, _ := recordstore.CanonicalTableRef(opts.FromTable)
		for _, mine := range []string{report.WrittenTo, report.ReadThrough} {
			if c, _, _ := recordstore.CanonicalTableRef(mine); c == from {
				err = eb.Build().Str("source", opts.FromTable).Errorf("chstore: load: the source is the relation the facts are written to or read from; a load onto itself would double it")
				return
			}
		}
		window = loadWindow(opts.Since, opts.Until)
	}

	if report.Before, err = loadCount(ctx, exec, report.ReadThrough, window); err != nil {
		return
	}
	if report.Before != 0 && !opts.Append {
		err = eb.Build().Str("relation", report.ReadThrough).Uint64("rows", report.Before).
			Errorf("chstore: load: the facts already hold rows in the window; a load that ran before would land twice. Load into an empty window, or say that appending is meant")
		return
	}

	want := factsArrowSchema()
	insert := func(rec arrow.RecordBatch) (ierr error) {
		conformed, ierr := conformBatch(rec, want)
		if ierr != nil {
			return ierr
		}
		defer conformed.Release()
		if conformed.NumRows() == 0 {
			return nil
		}
		if ierr = exec.InsertArrow(ctx, report.WrittenTo, []arrow.RecordBatch{conformed}); ierr != nil {
			return eb.Build().Uint64("rowsLoadedBefore", report.Rows).Errorf("chstore: load: insert: %w", ierr)
		}
		report.Rows += uint64(conformed.NumRows())
		report.Batches++
		if opts.Progress != nil {
			opts.Progress(report)
		}
		return nil
	}

	if opts.FromStream != nil {
		var rdr *ipc.Reader
		rdr, err = ipc.NewReader(opts.FromStream, ipc.WithAllocator(memory.NewGoAllocator()))
		if err != nil {
			err = eh.Errorf("chstore: load: open the stream: %w", err)
			return
		}
		defer rdr.Release()
		for rdr.Next() {
			if err = insert(rdr.RecordBatch()); err != nil {
				return
			}
		}
		if err = rdr.Err(); err != nil {
			err = eb.Build().Uint64("rowsLoadedBefore", report.Rows).Errorf("chstore: load: read the stream: %w", err)
			return
		}
	} else {
		blockRows := opts.BlockRows
		if blockRows <= 0 {
			blockRows = DefaultLoadBlockRows
		}
		sql := "SELECT * FROM " + opts.FromTable
		if window != "" {
			sql += " WHERE " + window
		}
		sql += " SETTINGS max_block_size = " + strconv.Itoa(blockRows)
		for rec, qerr := range exec.QueryArrow(ctx, sql) {
			if qerr != nil {
				err = eb.Build().Uint64("rowsLoadedBefore", report.Rows).Errorf("chstore: load: read the source: %w", qerr)
				return
			}
			// The server's blocks are not bounded by the setting once it
			// has squashed small parts' rows together; the inserts are.
			var ierr error
			for off := int64(0); off < rec.NumRows() && ierr == nil; off += int64(blockRows) {
				part := rec.NewSlice(off, min(off+int64(blockRows), rec.NumRows()))
				ierr = insert(part)
				part.Release()
			}
			rec.Release()
			if ierr != nil {
				err = ierr
				return
			}
		}
	}

	if report.After, err = loadCount(ctx, exec, report.ReadThrough, window); err != nil {
		return
	}
	if report.After-report.Before != report.Rows {
		err = eb.Build().Uint64("inserted", report.Rows).Uint64("before", report.Before).Uint64("after", report.After).Str("relation", report.ReadThrough).
			Errorf("chstore: load: the read relation does not show the rows that were inserted: rows were dropped on the way, the relation does not return them, or another writer was at work")
	}
	return
}

// factsArrowSchema is the Arrow schema of the batches the facts stores
// build: the one an insert transform is written against.
func factsArrowSchema() *arrow.Schema {
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 0)
	return ent.Builder().Schema()
}

// loadWindow is the predicate over the facts timestamp for [since, until),
// "" when neither bounds it.
func loadWindow(since time.Time, until time.Time) (where string) {
	ts := "`" + factsschema.TsColumn + "`"
	lit := func(t time.Time) string {
		return "toDateTime64('" + t.UTC().Format("2006-01-02 15:04:05.000000000") + "', 9, 'UTC')"
	}
	if !since.IsZero() {
		where = ts + " >= " + lit(since)
	}
	if !until.IsZero() {
		if where != "" {
			where += " AND "
		}
		where += ts + " < " + lit(until)
	}
	return
}

// loadCount is the number of rows of relation in the window.
func loadCount(ctx context.Context, exec recordstore.ExecutorI, relation string, window string) (n uint64, err error) {
	sql := "SELECT count() AS n FROM " + relation
	if window != "" {
		sql += " WHERE " + window
	}
	for rec, qerr := range exec.QueryArrow(ctx, sql) {
		if qerr != nil {
			return 0, eb.Build().Str("relation", relation).Errorf("chstore: load: count the facts: %w", qerr)
		}
		if col, ok := rec.Column(0).(*array.Uint64); ok && rec.NumRows() > 0 {
			n = col.Value(0)
		}
		rec.Release()
	}
	return
}

// conformBatch is rec with the columns of want, in want's order and under
// want's types. A column of rec that want does not name is left out. The
// arrays share rec's buffers; the caller releases the result.
func conformBatch(rec arrow.RecordBatch, want *arrow.Schema) (out arrow.RecordBatch, err error) {
	cols := make([]arrow.Array, 0, want.NumFields())
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()
	for _, f := range want.Fields() {
		idx := rec.Schema().FieldIndices(f.Name)
		if len(idx) != 1 {
			return nil, eb.Build().Str("column", f.Name).Int("found", len(idx)).Errorf("chstore: load: the source does not hold this facts column exactly once")
		}
		data, rerr := retype(rec.Column(idx[0]).Data(), f.Type)
		if rerr != nil {
			return nil, eb.Build().Str("column", f.Name).Errorf("chstore: load: %w", rerr)
		}
		cols = append(cols, array.MakeFromData(data))
		data.Release()
	}
	out = array.NewRecordBatch(want, cols, rec.NumRows())
	return
}

// retype is data under the type to, when the two differ only in how the
// same bytes are named: text and bytes, which share one layout; a
// timestamp's zone, which is not in the values; and a list whose items
// differ in one of those ways. Nothing is copied or converted. Any other
// difference is an error.
func retype(data arrow.ArrayData, to arrow.DataType) (out arrow.ArrayData, err error) {
	from := data.DataType()
	if arrow.TypeEqual(from, to) {
		data.Retain()
		return data, nil
	}
	same := func() arrow.ArrayData {
		return array.NewData(to, data.Len(), data.Buffers(), data.Children(), data.NullN(), data.Offset())
	}
	switch t := to.(type) {
	case *arrow.BinaryType, *arrow.StringType:
		if from.ID() == arrow.BINARY || from.ID() == arrow.STRING {
			return same(), nil
		}
	case *arrow.LargeBinaryType, *arrow.LargeStringType:
		if from.ID() == arrow.LARGE_BINARY || from.ID() == arrow.LARGE_STRING {
			return same(), nil
		}
	case *arrow.TimestampType:
		if f, ok := from.(*arrow.TimestampType); ok && f.Unit == t.Unit {
			return same(), nil
		}
	case *arrow.ListType:
		if f, ok := from.(*arrow.ListType); ok {
			_ = f
			child, cerr := retype(data.Children()[0], t.Elem())
			if cerr != nil {
				return nil, cerr
			}
			out = array.NewData(to, data.Len(), data.Buffers(), []arrow.ArrayData{child}, data.NullN(), data.Offset())
			child.Release()
			return out, nil
		}
	}
	return nil, eb.Build().Stringer("sourceType", from).Stringer("factsType", to).Errorf("the source holds the column under a type that is not a renaming of the facts type")
}
