// Package pprofarrow converts pprof profiles (profile.proto, as written
// by runtime/pprof or served by net/http/pprof) into a flat Arrow table
// designed for SQL exploration — the O1 shape of
// doc/adr-background-work/pprof-profiles-as-data.md. The output is an
// ArrowStream IPC payload, directly publishable as an ad-hoc dataset
// (ADR-0134) and queryable as `keelson('<handle>')`.
//
// One row per unique call stack; samples with identical stacks are
// merged (values summed). Columns:
//
//	stack  Array(String)  frame display names, ROOT-FIRST, inlining expanded
//	leaf   String         last stack element, denormalized for GROUP BY
//	pkg    String         the leaf's Go package path, "" when underivable
//	value  Int64          the profile's default sample type
//	<one Int64 column per remaining sample type, named after it>
//	kind, captured_at_unix_us, period, duration_ns   profile constants
//
// The default sample type is the profile's own DefaultSampleType when
// set, else the last sample type (the pprof tool's rule) — so `value`
// is cpu/nanoseconds for CPU profiles and inuse_space for heap
// profiles. Frames without symbols fall back to the location's hex
// address. Deep stacks arrive already truncated by the runtime;
// truncation is preserved, not repaired. Sample labels are dropped in
// this version.
package pprofarrow

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/google/pprof/profile"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// fixedColumns are the column names the converter itself emits; a
// sample type sanitizing to one of these is refused rather than
// silently renamed.
var fixedColumns = map[string]struct{}{
	"stack": {}, "leaf": {}, "pkg": {}, "value": {},
	"kind": {}, "captured_at_unix_us": {}, "period": {}, "duration_ns": {},
}

// Result carries the converted table and the metadata a publisher
// needs (alias naming, conservation checks).
type Result struct {
	// Kind is the inferred profile kind ("cpu", "heap", "allocs",
	// "goroutine", "contention", …, "custom") or the caller's hint.
	Kind string
	// DefaultType is the sample type behind the value column, as
	// "type/unit" (e.g. "cpu/nanoseconds").
	DefaultType string
	// ExtraColumns and ExtraTypes describe the per-type Int64 columns
	// emitted besides value, index-aligned with each other.
	ExtraColumns []string
	ExtraTypes   []string
	// Rows is the number of emitted rows (unique stacks).
	Rows uint64
	// TotalValue is the sum of the value column — equals the profile's
	// own total for the default sample type (conservation invariant).
	TotalValue int64
	// IPCStream is the ArrowStream payload
	// (adhocdata.PublishInput.ArrowIPCStream takes it verbatim).
	IPCStream []byte
}

// Option adjusts a conversion.
type Option func(*options)

type options struct {
	kindHint string
}

// WithKindHint overrides kind inference. Block and mutex profiles are
// indistinguishable by sample types (both are contentions/delay), so a
// capturing producer that knows which one it asked for should say so.
func WithKindHint(kind string) Option {
	return func(o *options) { o.kindHint = kind }
}

// Convert parses one pprof profile (plain or gzipped, per
// profile.Parse) and converts it. The whole profile is materialized in
// memory; profiles are small (typically well under a few MiB).
func Convert(r io.Reader, opts ...Option) (res Result, err error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	var p *profile.Profile
	p, err = profile.Parse(r)
	if err != nil {
		err = eh.Errorf("pprofarrow: parse profile: %w", err)
		return
	}
	if len(p.SampleType) == 0 {
		err = eh.Errorf("pprofarrow: profile declares no sample types")
		return
	}

	defaultIdx := defaultSampleTypeIndex(p)
	res.DefaultType = valueTypeName(p.SampleType[defaultIdx])
	res.Kind = o.kindHint
	if res.Kind == "" {
		res.Kind = inferKind(p, defaultIdx)
	}

	for i, st := range p.SampleType {
		if i == defaultIdx {
			continue
		}
		col := sanitizeColumn(st.Type)
		if _, clash := fixedColumns[col]; clash {
			err = eh.Errorf("pprofarrow: sample type %q sanitizes to reserved column name %q", valueTypeName(st), col)
			return
		}
		if slices.Contains(res.ExtraColumns, col) {
			err = eb.Build().Str("col", col).Errorf("pprofarrow: two sample types sanitize to column name")
			return
		}
		res.ExtraColumns = append(res.ExtraColumns, col)
		res.ExtraTypes = append(res.ExtraTypes, valueTypeName(st))
	}

	rows, err := aggregate(p)
	if err != nil {
		return
	}
	res.Rows = uint64(len(rows))
	for _, row := range rows {
		res.TotalValue += row.values[defaultIdx]
	}

	res.IPCStream, err = emit(p, rows, defaultIdx, res)
	return
}

// row is one output row: a unique root-first stack and the summed
// values across all samples that carried it, one per sample type.
type row struct {
	stack  []string
	values []int64
}

// aggregate folds the profile's samples into unique-stack rows,
// preserving first-seen order (deterministic for a given input).
func aggregate(p *profile.Profile) (rows []*row, err error) {
	index := make(map[string]*row, len(p.Sample))
	nTypes := len(p.SampleType)
	var frames []string
	for i, s := range p.Sample {
		if len(s.Value) != nTypes {
			err = eh.Errorf("pprofarrow: sample %d carries %d values for %d sample types", i, len(s.Value), nTypes)
			return
		}
		frames = appendFramesRootFirst(frames[:0], s)
		key := strings.Join(frames, "\x00")
		agg := index[key]
		if agg == nil {
			agg = &row{
				stack:  append([]string(nil), frames...),
				values: make([]int64, nTypes),
			}
			index[key] = agg
			rows = append(rows, agg)
		}
		for t, v := range s.Value {
			agg.values[t] += v
		}
	}
	return
}

// appendFramesRootFirst expands one sample's stack into display names,
// outermost caller first. profile.proto stores locations leaf-first,
// and within a location the inline entries innermost-first — so both
// walks run backwards.
func appendFramesRootFirst(frames []string, s *profile.Sample) []string {
	for li := len(s.Location) - 1; li >= 0; li-- {
		loc := s.Location[li]
		if len(loc.Line) == 0 {
			frames = append(frames, fallbackFrameName(loc))
			continue
		}
		for ln := len(loc.Line) - 1; ln >= 0; ln-- {
			fn := loc.Line[ln].Function
			if fn == nil || fn.Name == "" {
				frames = append(frames, fallbackFrameName(loc))
			} else {
				frames = append(frames, fn.Name)
			}
		}
	}
	if len(frames) == 0 {
		frames = append(frames, "(no stack)")
	}
	return frames
}

func fallbackFrameName(loc *profile.Location) string {
	return fmt.Sprintf("0x%x", loc.Address)
}

// pkgOf derives the Go package path from a function's display name:
// everything up to the first dot after the last slash. Names without a
// dot there (hex fallbacks, some native frames) yield "".
func pkgOf(name string) string {
	slash := strings.LastIndexByte(name, '/')
	rest := name[slash+1:]
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return ""
	}
	return name[:slash+1+dot]
}

// defaultSampleTypeIndex resolves the value column's sample type: the
// profile's DefaultSampleType when it names one, else the last sample
// type (the pprof tool's own default).
func defaultSampleTypeIndex(p *profile.Profile) int {
	if p.DefaultSampleType != "" {
		for i, st := range p.SampleType {
			if st.Type == p.DefaultSampleType {
				return i
			}
		}
	}
	return len(p.SampleType) - 1
}

func valueTypeName(vt *profile.ValueType) string {
	return vt.Type + "/" + vt.Unit
}

// inferKind classifies the profile by its sample-type signature. Block
// and mutex profiles share one signature and come back as
// "contention"; WithKindHint disambiguates.
func inferKind(p *profile.Profile, defaultIdx int) string {
	st := p.SampleType
	switch {
	case len(st) == 2 &&
		valueTypeName(st[0]) == "samples/count" &&
		valueTypeName(st[1]) == "cpu/nanoseconds":
		return "cpu"
	case len(st) == 2 &&
		valueTypeName(st[0]) == "contentions/count" &&
		valueTypeName(st[1]) == "delay/nanoseconds":
		return "contention"
	case len(st) == 4 &&
		valueTypeName(st[0]) == "alloc_objects/count" &&
		valueTypeName(st[1]) == "alloc_space/bytes" &&
		valueTypeName(st[2]) == "inuse_objects/count" &&
		valueTypeName(st[3]) == "inuse_space/bytes":
		if st[defaultIdx].Type == "alloc_space" {
			return "allocs"
		}
		return "heap"
	case len(st) == 1 && st[0].Type != "" && st[0].Unit == "count":
		return st[0].Type
	}
	return "custom"
}

// sanitizeColumn maps a sample-type name onto a ClickHouse-friendly
// identifier: [A-Za-z0-9_] kept, everything else '_', leading digit
// prefixed with '_'. Empty input yields "_".
func sanitizeColumn(name string) string {
	var b strings.Builder
	b.Grow(len(name) + 1)
	for i, r := range name {
		ok := r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')
		if !ok {
			r = '_'
		}
		if i == 0 && r >= '0' && r <= '9' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// emit materializes the rows as one Arrow record and serializes it as
// an ArrowStream payload.
func emit(p *profile.Profile, rows []*row, defaultIdx int, res Result) (stream []byte, err error) {
	fields := make([]arrow.Field, 0, 8+len(res.ExtraColumns))
	fields = append(fields,
		arrow.Field{Name: "stack", Type: arrow.ListOfNonNullable(arrow.BinaryTypes.String)},
		arrow.Field{Name: "leaf", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "pkg", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "value", Type: arrow.PrimitiveTypes.Int64},
	)
	for _, col := range res.ExtraColumns {
		fields = append(fields, arrow.Field{Name: col, Type: arrow.PrimitiveTypes.Int64})
	}
	fields = append(fields,
		arrow.Field{Name: "kind", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "captured_at_unix_us", Type: arrow.PrimitiveTypes.Int64},
		arrow.Field{Name: "period", Type: arrow.PrimitiveTypes.Int64},
		arrow.Field{Name: "duration_ns", Type: arrow.PrimitiveTypes.Int64},
	)
	schema := arrow.NewSchema(fields, nil)

	allocator := memory.NewGoAllocator()
	rb := array.NewRecordBuilder(allocator, schema)
	defer rb.Release()

	// extraSrc maps each extra column to its sample-type index: all
	// indices except the default, in declaration order.
	extraSrc := make([]int, 0, len(res.ExtraColumns))
	for i := range p.SampleType {
		if i != defaultIdx {
			extraSrc = append(extraSrc, i)
		}
	}

	stackB := rb.Field(0).(*array.ListBuilder)
	frameB := stackB.ValueBuilder().(*array.StringBuilder)
	leafB := rb.Field(1).(*array.StringBuilder)
	pkgB := rb.Field(2).(*array.StringBuilder)
	valueB := rb.Field(3).(*array.Int64Builder)
	extraB := make([]*array.Int64Builder, len(extraSrc))
	for i := range extraSrc {
		extraB[i] = rb.Field(4 + i).(*array.Int64Builder)
	}
	base := 4 + len(extraSrc)
	kindB := rb.Field(base).(*array.StringBuilder)
	capturedB := rb.Field(base + 1).(*array.Int64Builder)
	periodB := rb.Field(base + 2).(*array.Int64Builder)
	durationB := rb.Field(base + 3).(*array.Int64Builder)

	capturedAtUnixUs := p.TimeNanos / 1000
	for _, r := range rows {
		stackB.Append(true)
		for _, f := range r.stack {
			frameB.Append(f)
		}
		leaf := r.stack[len(r.stack)-1]
		leafB.Append(leaf)
		pkgB.Append(pkgOf(leaf))
		valueB.Append(r.values[defaultIdx])
		for i, src := range extraSrc {
			extraB[i].Append(r.values[src])
		}
		kindB.Append(res.Kind)
		capturedB.Append(capturedAtUnixUs)
		periodB.Append(p.Period)
		durationB.Append(p.DurationNanos)
	}

	rec := rb.NewRecordBatch()
	defer rec.Release()

	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(allocator))
	err = w.Write(rec)
	if err != nil {
		_ = w.Close()
		err = eh.Errorf("pprofarrow: ipc write: %w", err)
		return
	}
	err = w.Close()
	if err != nil {
		err = eh.Errorf("pprofarrow: ipc close: %w", err)
		return
	}
	stream = buf.Bytes()
	return
}
