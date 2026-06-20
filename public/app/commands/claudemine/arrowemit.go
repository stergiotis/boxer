package claudemine

import (
	"os"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// eventRow is one row of the single denormalized `events` table. Identity
// columns are always set; every payload column is a pointer that is nil when
// it does not apply to this row's kind (emitted as Arrow NULL).
type eventRow struct {
	// identity / context
	SessionID   string
	Seq         int32
	Kind        string
	Ts          time.Time
	ProjectDir  string
	ProjectRepo string
	Cwd         *string
	GitBranch   *string
	Version     *string
	UUID        *string
	ParentUUID  *string
	IsSidechain *bool

	// tokens / model (kind=assistant)
	Model               *string
	InputTokens         *int64
	OutputTokens        *int64
	CacheReadTokens     *int64
	CacheCreationTokens *int64
	ContextTokens       *int64
	ServiceTier         *string
	StopReason          *string
	RequestID           *string
	NToolUse            *int32
	HasThinking         *bool

	// user input (kind=user_input)
	Text           *string
	PromptSource   *string
	PermissionMode *string

	// file op (kind=file_read|file_write|file_edit)
	ToolName     *string
	FilePath     *string
	TargetRepo   *string
	FileRel      *string
	FileExt      *string
	LinesAdded   *int32
	LinesRemoved *int32

	// commit (kind=commit)
	CommitSha          *string
	CommitKind         *string
	CommitSubject      *string
	CommitInsertions   *int32
	CommitDeletions    *int32
	CommitFilesChanged *int32

	// session (kind=session)
	Title *string
}

func nn(name string, t arrow.DataType) arrow.Field { return arrow.Field{Name: name, Type: t} }
func nu(name string, t arrow.DataType) arrow.Field {
	return arrow.Field{Name: name, Type: t, Nullable: true}
}

var (
	tString = arrow.BinaryTypes.String
	tI32    = arrow.PrimitiveTypes.Int32
	tI64    = arrow.PrimitiveTypes.Int64
	tBool   = arrow.FixedWidthTypes.Boolean
	tTs     = arrow.FixedWidthTypes.Timestamp_us // Timestamp(Microsecond, "UTC")
)

var eventsSchema = arrow.NewSchema([]arrow.Field{
	nn("session_id", tString),   // 0
	nn("seq", tI32),             // 1
	nn("kind", tString),         // 2
	nu("ts", tTs),               // 3
	nn("project_dir", tString),  // 4
	nn("project_repo", tString), // 5
	nu("cwd", tString),          // 6
	nu("git_branch", tString),   // 7
	nu("version", tString),      // 8
	nu("uuid", tString),         // 9
	nu("parent_uuid", tString),  // 10
	nu("is_sidechain", tBool),   // 11

	nu("model", tString),              // 12
	nu("input_tokens", tI64),          // 13
	nu("output_tokens", tI64),         // 14
	nu("cache_read_tokens", tI64),     // 15
	nu("cache_creation_tokens", tI64), // 16
	nu("context_tokens", tI64),        // 17
	nu("service_tier", tString),       // 18
	nu("stop_reason", tString),        // 19
	nu("request_id", tString),         // 20
	nu("n_tool_use", tI32),            // 21
	nu("has_thinking", tBool),         // 22

	nu("text", tString),            // 23
	nu("prompt_source", tString),   // 24
	nu("permission_mode", tString), // 25

	nu("tool_name", tString),   // 26
	nu("file_path", tString),   // 27
	nu("target_repo", tString), // 28
	nu("file_rel", tString),    // 29
	nu("file_ext", tString),    // 30
	nu("lines_added", tI32),    // 31
	nu("lines_removed", tI32),  // 32

	nu("commit_sha", tString),        // 33
	nu("commit_kind", tString),       // 34
	nu("commit_subject", tString),    // 35
	nu("commit_insertions", tI32),    // 36
	nu("commit_deletions", tI32),     // 37
	nu("commit_files_changed", tI32), // 38

	nu("title", tString), // 39
}, nil)

const eventsArrowName = "claude_events.arrow"

// WriteEventsArrow writes all event rows as a single Arrow IPC file (Zstd).
func WriteEventsArrow(path string, rows []eventRow) (err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, eventsSchema)
	defer rb.Release()
	for i := range rows {
		r := &rows[i]
		rb.Field(0).(*array.StringBuilder).Append(r.SessionID)
		rb.Field(1).(*array.Int32Builder).Append(r.Seq)
		rb.Field(2).(*array.StringBuilder).Append(r.Kind)
		appTs(rb.Field(3).(*array.TimestampBuilder), r.Ts)
		rb.Field(4).(*array.StringBuilder).Append(r.ProjectDir)
		rb.Field(5).(*array.StringBuilder).Append(r.ProjectRepo)
		appStr(rb.Field(6).(*array.StringBuilder), r.Cwd)
		appStr(rb.Field(7).(*array.StringBuilder), r.GitBranch)
		appStr(rb.Field(8).(*array.StringBuilder), r.Version)
		appStr(rb.Field(9).(*array.StringBuilder), r.UUID)
		appStr(rb.Field(10).(*array.StringBuilder), r.ParentUUID)
		appBool(rb.Field(11).(*array.BooleanBuilder), r.IsSidechain)

		appStr(rb.Field(12).(*array.StringBuilder), r.Model)
		appI64(rb.Field(13).(*array.Int64Builder), r.InputTokens)
		appI64(rb.Field(14).(*array.Int64Builder), r.OutputTokens)
		appI64(rb.Field(15).(*array.Int64Builder), r.CacheReadTokens)
		appI64(rb.Field(16).(*array.Int64Builder), r.CacheCreationTokens)
		appI64(rb.Field(17).(*array.Int64Builder), r.ContextTokens)
		appStr(rb.Field(18).(*array.StringBuilder), r.ServiceTier)
		appStr(rb.Field(19).(*array.StringBuilder), r.StopReason)
		appStr(rb.Field(20).(*array.StringBuilder), r.RequestID)
		appI32(rb.Field(21).(*array.Int32Builder), r.NToolUse)
		appBool(rb.Field(22).(*array.BooleanBuilder), r.HasThinking)

		appStr(rb.Field(23).(*array.StringBuilder), r.Text)
		appStr(rb.Field(24).(*array.StringBuilder), r.PromptSource)
		appStr(rb.Field(25).(*array.StringBuilder), r.PermissionMode)

		appStr(rb.Field(26).(*array.StringBuilder), r.ToolName)
		appStr(rb.Field(27).(*array.StringBuilder), r.FilePath)
		appStr(rb.Field(28).(*array.StringBuilder), r.TargetRepo)
		appStr(rb.Field(29).(*array.StringBuilder), r.FileRel)
		appStr(rb.Field(30).(*array.StringBuilder), r.FileExt)
		appI32(rb.Field(31).(*array.Int32Builder), r.LinesAdded)
		appI32(rb.Field(32).(*array.Int32Builder), r.LinesRemoved)

		appStr(rb.Field(33).(*array.StringBuilder), r.CommitSha)
		appStr(rb.Field(34).(*array.StringBuilder), r.CommitKind)
		appStr(rb.Field(35).(*array.StringBuilder), r.CommitSubject)
		appI32(rb.Field(36).(*array.Int32Builder), r.CommitInsertions)
		appI32(rb.Field(37).(*array.Int32Builder), r.CommitDeletions)
		appI32(rb.Field(38).(*array.Int32Builder), r.CommitFilesChanged)

		appStr(rb.Field(39).(*array.StringBuilder), r.Title)
	}
	return writeRecord(path, rb)
}

func appStr(b *array.StringBuilder, p *string) {
	if p == nil {
		b.AppendNull()
	} else {
		b.Append(*p)
	}
}
func appI32(b *array.Int32Builder, p *int32) {
	if p == nil {
		b.AppendNull()
	} else {
		b.Append(*p)
	}
}
func appI64(b *array.Int64Builder, p *int64) {
	if p == nil {
		b.AppendNull()
	} else {
		b.Append(*p)
	}
}
func appBool(b *array.BooleanBuilder, p *bool) {
	if p == nil {
		b.AppendNull()
	} else {
		b.Append(*p)
	}
}
func appTs(b *array.TimestampBuilder, t time.Time) {
	if t.IsZero() {
		b.AppendNull()
	} else {
		b.Append(arrow.Timestamp(t.UnixMicro()))
	}
}

func writeRecord(path string, rb *array.RecordBuilder) (err error) {
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var f *os.File
	f, err = os.Create(path)
	if err != nil {
		return eh.Errorf("unable to create arrow file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	var w *ipc.FileWriter
	w, err = ipc.NewFileWriter(f, ipc.WithZstd(), ipc.WithAllocator(memory.DefaultAllocator), ipc.WithSchema(eventsSchema))
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
