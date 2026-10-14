package watchbill

import (
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// The job list is a recordtable.RenderGrid over the visible jobs. The grid
// draws records, so the list is held as one: a column per job column,
// carrying the value the header sort compares — a string, or a number where
// the shown text does not sort (an attempt count, a time) — while the cell
// text and tone come from the job itself. The record is rebuilt only when
// the list or the kind filter changes.

// jobColumn is one column: its header, width, alignment, the cell text,
// and the key the sort compares — a string or a number, never both.
type jobColumn struct {
	header  string
	width   float32
	numeric bool
	text    func(j watchbillstore.Job) string
	strKey  func(j watchbillstore.Job) string
	numKey  func(j watchbillstore.Job) int64
	tone    func(j watchbillstore.Job) (color.Color, bool)
	hover   string
}

var jobColumns = []jobColumn{
	{header: "id", width: 120, text: func(j watchbillstore.Job) string { return j.ID }, strKey: func(j watchbillstore.Job) string { return j.ID }, hover: "the job id, the run's task id"},
	{header: "kind", width: 150, text: func(j watchbillstore.Job) string { return j.Kind }, strKey: func(j watchbillstore.Job) string { return j.Kind }, hover: "the handler's kind"},
	{header: "subject", width: 170, text: func(j watchbillstore.Job) string { return j.Subject }, strKey: func(j watchbillstore.Job) string { return j.Subject }, hover: "the row in the consumer's own table that says what to do"},
	{header: "queue", width: 80, text: func(j watchbillstore.Job) string { return j.Queue }, strKey: func(j watchbillstore.Job) string { return j.Queue }, hover: "the queue a worker drains"},
	{header: "state", width: 96, text: func(j watchbillstore.Job) string { return j.State }, strKey: func(j watchbillstore.Job) string { return j.State }, tone: stateTone, hover: "the row's state (ADR-0223 §SD2)"},
	{header: "attempt", width: 72, numeric: true, text: func(j watchbillstore.Job) string { return fmt.Sprintf("%d/%d", j.Attempt, j.MaxAttempts) }, numKey: func(j watchbillstore.Job) int64 { return int64(j.Attempt) }, hover: "attempts made of attempts allowed"},
	{header: "run after", width: 160, text: func(j watchbillstore.Job) string { return when(j.RunAfter) }, numKey: func(j watchbillstore.Job) int64 { return j.RunAfter.UnixNano() }, hover: "when the job became or becomes due"},
	{header: "worker run", width: 130, text: func(j watchbillstore.Job) string { return j.WorkerRun }, strKey: func(j watchbillstore.Job) string { return j.WorkerRun }, hover: "the run that holds or last changed the row"},
	{header: "last error", width: 300, text: func(j watchbillstore.Job) string { return firstLine(j.LastError) }, strKey: func(j watchbillstore.Job) string { return j.LastError }, hover: "the last attempt's error, first line"},
}

// stateTone colours the state cell the way its chip is toned.
func stateTone(j watchbillstore.Job) (col color.Color, ok bool) {
	var def styletokens.RGBA8
	switch j.State {
	case watchbillstore.StateQueued:
		def = styletokens.InfoDefault
	case watchbillstore.StateRunning:
		def = styletokens.AccentDefault
	case watchbillstore.StateSucceeded:
		def = styletokens.SuccessDefault
	case watchbillstore.StateFailed, watchbillstore.StateCancel:
		def = styletokens.WarningDefault
	case watchbillstore.StateDiscarded, watchbillstore.StateAbandoned:
		def = styletokens.ErrorDefault
	default:
		return
	}
	return color.Hex(def.AsHex()), true
}

// jobsSchema is the record's shape: one field per job column, named by its
// header so the width identity is the header, as it was before the list
// was a record.
var jobsSchema = func() *arrow.Schema {
	fields := make([]arrow.Field, 0, len(jobColumns))
	for _, col := range jobColumns {
		typ := arrow.DataType(arrow.BinaryTypes.String)
		if col.numKey != nil {
			typ = arrow.PrimitiveTypes.Int64
		}
		fields = append(fields, arrow.Field{Name: col.header, Type: typ})
	}
	return arrow.NewSchema(fields, nil)
}()

// jobsRecord holds jobs' sort keys as a record, row i being jobs[i].
func jobsRecord(jobs []watchbillstore.Job) (rec arrow.RecordBatch) {
	mem := memory.DefaultAllocator
	arrs := make([]arrow.Array, 0, len(jobColumns))
	for _, col := range jobColumns {
		if col.numKey != nil {
			b := array.NewInt64Builder(mem)
			for _, j := range jobs {
				b.Append(col.numKey(j))
			}
			arrs = append(arrs, b.NewArray())
			b.Release()
			continue
		}
		b := array.NewStringBuilder(mem)
		for _, j := range jobs {
			b.Append(col.strKey(j))
		}
		arrs = append(arrs, b.NewArray())
		b.Release()
	}
	rec = array.NewRecordBatch(jobsSchema, arrs, int64(len(jobs)))
	for _, a := range arrs {
		a.Release()
	}
	return
}

// jobsRecKey names what the list record was built from: a refresh lands a
// new snapshot with a new time, and the kind filter narrows it.
type jobsRecKey struct {
	refreshed time.Time
	kind      string
	n         int
}

// listRecord returns the record for this frame's visible jobs, rebuilding
// it only when the key moved.
func (inst *App) listRecord(key jobsRecKey, jobs []watchbillstore.Job) (rec arrow.RecordBatch) {
	if inst.listRec != nil && inst.listRecKey == key {
		return inst.listRec
	}
	if inst.listRec != nil {
		inst.listRec.Release()
	}
	inst.listRec, inst.listRecKey = jobsRecord(jobs), key
	return inst.listRec
}

// jobGridColumns lays the job columns out for the grid. The width identity
// each yields — the header, and "text" or "number" — is the one the list
// stored widths under before it was a grid, so dragged widths carry over.
func jobGridColumns(jobs []watchbillstore.Job) (cols []recordtable.Column) {
	cols = make([]recordtable.Column, 0, len(jobColumns))
	for i, col := range jobColumns {
		typ := "text"
		if col.numeric {
			typ = "number"
		}
		cols = append(cols, recordtable.Column{
			Arrow: i, Header: col.header, Hover: col.hover, Width: col.width, WidthType: typ,
			Cell: func(row int64) (cell recordtable.Cell) {
				j := jobs[row]
				cell = recordtable.Cell{Text: col.text(j), LeftAlign: !col.numeric}
				if col.tone != nil {
					cell.Tone, cell.Toned = col.tone(j)
				}
				return
			},
		})
	}
	return
}

// jobsTableTag scopes the list's stored widths and the grid, one constant so
// the two cannot drift apart.
const jobsTableTag = "jobs"

func (inst *App) renderList(snap snapshot, jobs []watchbillstore.Job) {
	rec := inst.listRecord(jobsRecKey{refreshed: snap.refreshed, kind: inst.filters.kind, n: len(jobs)}, jobs)
	selected := int64(-1)
	for i, j := range jobs {
		if j.ID == inst.selectedID {
			selected = int64(i)
			break
		}
	}
	res := recordtable.RenderGrid(recordtable.Input{
		Ids: inst.ids, ScopeKey: jobsTableTag, Record: rec, Columns: jobGridColumns(jobs),
		Widths: inst.widths, TableTag: jobsTableTag,
		Sort: &inst.sort, Order: inst.sort.OrderFor(rec, rec.NumRows()),
		Selected: selected, HasSelection: selected >= 0, State: &inst.list,
	})
	if res.Err != nil {
		inst.logger.Warn().Err(res.Err).Msg("watchbill app: job list refused its input")
	}
	if res.WidthsErr != nil {
		inst.logger.Warn().Err(res.WidthsErr).Msg("watchbill app: storing column widths failed; will retry")
	}
	if res.SortClicked >= 0 {
		inst.sort.Clicked(res.SortClicked)
	}
	if res.Clicked >= 0 && res.Clicked < int64(len(jobs)) {
		clicked := jobs[res.Clicked].ID
		gesture(inst, opSelect, JobArgs{Id: clicked}, func() { inst.select_(clicked) })
	}
}
