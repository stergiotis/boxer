package adr

import (
	"os"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// strList yields Arrow List<String> with non-nullable elements so ClickHouse
// reads the column as Array(String) (arrayStringConcat etc. work directly).
func strList() arrow.DataType { return arrow.ListOfNonNullable(arrow.BinaryTypes.String) }

var adrSchema = arrow.NewSchema([]arrow.Field{
	{Name: "num", Type: arrow.PrimitiveTypes.Int32},
	{Name: "slug", Type: arrow.BinaryTypes.String},
	{Name: "title", Type: arrow.BinaryTypes.String},
	{Name: "status", Type: arrow.BinaryTypes.String},
	{Name: "date", Type: arrow.BinaryTypes.String},
	{Name: "reviewed_by", Type: arrow.BinaryTypes.String},
	{Name: "reviewed_date", Type: arrow.BinaryTypes.String},
	{Name: "superseded_by", Type: arrow.BinaryTypes.String},
	{Name: "withdrawn_date", Type: arrow.BinaryTypes.String},
	{Name: "body_bytes", Type: arrow.PrimitiveTypes.Int64},
	{Name: "has_update", Type: arrow.FixedWidthTypes.Boolean},
	{Name: "update_count", Type: arrow.PrimitiveTypes.Int32},
	{Name: "last_date", Type: arrow.BinaryTypes.String},
	{Name: "plan_markers", Type: strList()},
	{Name: "plan_max_phase", Type: arrow.PrimitiveTypes.Int32},
	{Name: "code_refs", Type: arrow.PrimitiveTypes.Int32},
	{Name: "code_files", Type: arrow.PrimitiveTypes.Int32},
	{Name: "code_pkgs", Type: arrow.PrimitiveTypes.Int32},
	{Name: "code_langs", Type: strList()},
	{Name: "code_qualifiers", Type: strList()},
	{Name: "impl_evidence", Type: arrow.BinaryTypes.String},
	{Name: "subtasks_total", Type: arrow.PrimitiveTypes.Int32},
	{Name: "subtasks_done", Type: arrow.PrimitiveTypes.Int32},
	{Name: "subtasks_cited", Type: arrow.PrimitiveTypes.Int32},
	{Name: "path", Type: arrow.BinaryTypes.String},
}, nil)

var coderefSchema = arrow.NewSchema([]arrow.Field{
	{Name: "num", Type: arrow.PrimitiveTypes.Int32},
	{Name: "path", Type: arrow.BinaryTypes.String},
	{Name: "line", Type: arrow.PrimitiveTypes.Int32},
	{Name: "lang", Type: arrow.BinaryTypes.String},
	{Name: "pkg", Type: arrow.BinaryTypes.String},
	{Name: "qualifier", Type: arrow.BinaryTypes.String},
	{Name: "snippet", Type: arrow.BinaryTypes.String},
}, nil)

var subtaskSchema = arrow.NewSchema([]arrow.Field{
	{Name: "num", Type: arrow.PrimitiveTypes.Int32},
	{Name: "marker", Type: arrow.BinaryTypes.String},
	{Name: "kind", Type: arrow.BinaryTypes.String},
	{Name: "ordinal", Type: arrow.PrimitiveTypes.Int32},
	{Name: "title", Type: arrow.BinaryTypes.String},
	{Name: "done", Type: arrow.FixedWidthTypes.Boolean},
	{Name: "shape", Type: arrow.BinaryTypes.String},
	{Name: "line", Type: arrow.PrimitiveTypes.Int32},
	{Name: "code_refs", Type: arrow.PrimitiveTypes.Int32},
}, nil)

// WriteAdrArrow writes the adr registry + code-evidence rows as an Arrow IPC file.
func WriteAdrArrow(path string, adrs []adrcorpus.Adr) (err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, adrSchema)
	defer rb.Release()
	for _, a := range adrs {
		rb.Field(0).(*array.Int32Builder).Append(int32(a.Num))
		rb.Field(1).(*array.StringBuilder).Append(a.Slug)
		rb.Field(2).(*array.StringBuilder).Append(a.Title)
		rb.Field(3).(*array.StringBuilder).Append(a.Status)
		rb.Field(4).(*array.StringBuilder).Append(a.Date)
		rb.Field(5).(*array.StringBuilder).Append(a.ReviewedBy)
		rb.Field(6).(*array.StringBuilder).Append(a.ReviewedDate)
		rb.Field(7).(*array.StringBuilder).Append(a.SupersededBy)
		rb.Field(8).(*array.StringBuilder).Append(a.WithdrawnDate)
		rb.Field(9).(*array.Int64Builder).Append(int64(a.BodyBytes))
		rb.Field(10).(*array.BooleanBuilder).Append(a.HasUpdate)
		rb.Field(11).(*array.Int32Builder).Append(int32(a.UpdateCount))
		rb.Field(12).(*array.StringBuilder).Append(a.LastDate)
		appendStrList(rb.Field(13).(*array.ListBuilder), a.PlanMarkers)
		rb.Field(14).(*array.Int32Builder).Append(int32(a.PlanMaxPhase))
		rb.Field(15).(*array.Int32Builder).Append(int32(a.CodeRefs))
		rb.Field(16).(*array.Int32Builder).Append(int32(a.CodeFiles))
		rb.Field(17).(*array.Int32Builder).Append(int32(a.CodePkgs))
		appendStrList(rb.Field(18).(*array.ListBuilder), a.CodeLangs)
		appendStrList(rb.Field(19).(*array.ListBuilder), a.CodeQualifiers)
		rb.Field(20).(*array.StringBuilder).Append(a.ImplEvidence)
		rb.Field(21).(*array.Int32Builder).Append(int32(a.SubtasksTotal))
		rb.Field(22).(*array.Int32Builder).Append(int32(a.SubtasksDone))
		rb.Field(23).(*array.Int32Builder).Append(int32(a.SubtasksCited))
		rb.Field(24).(*array.StringBuilder).Append(a.Path)
	}
	return writeRecord(path, adrSchema, rb)
}

// WriteSubtaskArrow writes the per-sub-item rows as an Arrow IPC file.
func WriteSubtaskArrow(path string, subs []adrcorpus.Subtask) (err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, subtaskSchema)
	defer rb.Release()
	for _, s := range subs {
		rb.Field(0).(*array.Int32Builder).Append(int32(s.Num))
		rb.Field(1).(*array.StringBuilder).Append(s.Marker)
		rb.Field(2).(*array.StringBuilder).Append(s.Kind)
		rb.Field(3).(*array.Int32Builder).Append(int32(s.Ordinal))
		rb.Field(4).(*array.StringBuilder).Append(s.Title)
		rb.Field(5).(*array.BooleanBuilder).Append(s.Done)
		rb.Field(6).(*array.StringBuilder).Append(s.Shape)
		rb.Field(7).(*array.Int32Builder).Append(int32(s.Line))
		rb.Field(8).(*array.Int32Builder).Append(int32(s.CodeRefs))
	}
	return writeRecord(path, subtaskSchema, rb)
}

// WriteCoderefArrow writes the per-citation detail rows as an Arrow IPC file.
func WriteCoderefArrow(path string, refs []adrcorpus.CodeRef) (err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, coderefSchema)
	defer rb.Release()
	for _, r := range refs {
		rb.Field(0).(*array.Int32Builder).Append(int32(r.Num))
		rb.Field(1).(*array.StringBuilder).Append(r.Path)
		rb.Field(2).(*array.Int32Builder).Append(int32(r.Line))
		rb.Field(3).(*array.StringBuilder).Append(r.Lang)
		rb.Field(4).(*array.StringBuilder).Append(r.Pkg)
		rb.Field(5).(*array.StringBuilder).Append(r.Qualifier)
		rb.Field(6).(*array.StringBuilder).Append(r.Snippet)
	}
	return writeRecord(path, coderefSchema, rb)
}

func appendStrList(lb *array.ListBuilder, xs []string) {
	lb.Append(true)
	vb := lb.ValueBuilder().(*array.StringBuilder)
	for _, s := range xs {
		vb.Append(s)
	}
}

func writeRecord(path string, schema *arrow.Schema, rb *array.RecordBuilder) (err error) {
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var f *os.File
	f, err = os.Create(path)
	if err != nil {
		return eh.Errorf("unable to create arrow file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	var w *ipc.FileWriter
	w, err = ipc.NewFileWriter(f, ipc.WithZstd(), ipc.WithAllocator(memory.DefaultAllocator), ipc.WithSchema(schema))
	if err != nil {
		return eh.Errorf("unable to create arrow writer %q: %w", path, err)
	}
	if err = w.Write(rec); err != nil {
		_ = w.Close()
		return eh.Errorf("unable to write arrow record %q: %w", path, err)
	}
	if err = w.Close(); err != nil {
		return eh.Errorf("unable to close arrow writer %q: %w", path, err)
	}
	return nil
}
