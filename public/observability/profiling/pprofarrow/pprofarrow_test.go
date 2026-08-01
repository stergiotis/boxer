package pprofarrow

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/google/pprof/profile"
)

// --- hand-built profiles -------------------------------------------------
//
// The fixtures below are authored as profile.Profile structs — the struct
// IS the expected content — then serialized through the library and fed to
// Convert as bytes, so the test exercises the same parse path production
// uses without trusting the converter to test itself.

type fnSpec struct {
	id   uint64
	name string
}

func buildFunctions(specs ...fnSpec) (fns []*profile.Function, byId map[uint64]*profile.Function) {
	byId = make(map[uint64]*profile.Function, len(specs))
	for _, s := range specs {
		fn := &profile.Function{ID: s.id, Name: s.name, SystemName: s.name, Filename: s.name + ".go"}
		fns = append(fns, fn)
		byId[s.id] = fn
	}
	return
}

// loc makes one location; several function ids mean inlining, ordered
// innermost-first exactly like profile.proto stores them.
func loc(id uint64, byId map[uint64]*profile.Function, fnIds ...uint64) *profile.Location {
	l := &profile.Location{ID: id, Address: 0x1000 * id}
	for _, fid := range fnIds {
		l.Line = append(l.Line, profile.Line{Function: byId[fid], Line: int64(10 * fid)})
	}
	return l
}

func serialize(t *testing.T, p *profile.Profile) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatalf("serialize profile: %v", err)
	}
	return buf.Bytes()
}

func cpuValueTypes() []*profile.ValueType {
	return []*profile.ValueType{
		{Type: "samples", Unit: "count"},
		{Type: "cpu", Unit: "nanoseconds"},
	}
}

func heapValueTypes() []*profile.ValueType {
	return []*profile.ValueType{
		{Type: "alloc_objects", Unit: "count"},
		{Type: "alloc_space", Unit: "bytes"},
		{Type: "inuse_objects", Unit: "count"},
		{Type: "inuse_space", Unit: "bytes"},
	}
}

// decode reads the IPCStream back through the same arrow/ipc layer the
// ad-hoc dataset service uses and returns the concatenated rows as
// column-name → values.
func decode(t *testing.T, stream []byte) (schema *arrow.Schema, stacks [][]string, cols map[string][]any) {
	t.Helper()
	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("ipc reader: %v", err)
	}
	defer rdr.Release()
	schema = rdr.Schema()
	cols = make(map[string][]any)
	for rdr.Next() {
		rec := rdr.RecordBatch()
		for c := range int(rec.NumCols()) {
			name := rec.ColumnName(c)
			switch col := rec.Column(c).(type) {
			case *array.String:
				for i := range col.Len() {
					cols[name] = append(cols[name], col.Value(i))
				}
			case *array.Int64:
				for i := range col.Len() {
					cols[name] = append(cols[name], col.Value(i))
				}
			case *array.List:
				vals := col.ListValues().(*array.String)
				for i := range col.Len() {
					s, e := col.ValueOffsets(i)
					var frames []string
					for j := s; j < e; j++ {
						frames = append(frames, vals.Value(int(j)))
					}
					stacks = append(stacks, frames)
				}
			default:
				t.Fatalf("unexpected column type %T for %q", col, name)
			}
		}
	}
	if err = rdr.Err(); err != nil {
		t.Fatalf("ipc read: %v", err)
	}
	return
}

func int64s(t *testing.T, cols map[string][]any, name string) (out []int64) {
	t.Helper()
	for _, v := range cols[name] {
		i, ok := v.(int64)
		if !ok {
			t.Fatalf("column %q holds %T, want int64", name, v)
		}
		out = append(out, i)
	}
	return
}

func strs(t *testing.T, cols map[string][]any, name string) (out []string) {
	t.Helper()
	for _, v := range cols[name] {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("column %q holds %T, want string", name, v)
		}
		out = append(out, s)
	}
	return
}

// --- unit tests ----------------------------------------------------------

func TestConvertCPUMergesAndOrdersStacks(t *testing.T) {
	fns, byId := buildFunctions(
		fnSpec{1, "main.main"},
		fnSpec{2, "github.com/acme/mod/pkga.Work"},
	)
	lMain := loc(1, byId, 1)
	lWork := loc(2, byId, 2)
	p := &profile.Profile{
		SampleType:    cpuValueTypes(),
		PeriodType:    &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:        10_000_000,
		TimeNanos:     1_700_000_000_123_456_000,
		DurationNanos: 2_000_000_000,
		Function:      fns,
		Location:      []*profile.Location{lMain, lWork},
		Sample: []*profile.Sample{
			// Locations are leaf-first on the wire.
			{Location: []*profile.Location{lWork, lMain}, Value: []int64{3, 30}},
			{Location: []*profile.Location{lWork, lMain}, Value: []int64{2, 20}},
			// Recursion: repeated frame must be preserved, not deduped.
			{Location: []*profile.Location{lWork, lWork, lMain}, Value: []int64{1, 10}},
		},
	}

	res, err := Convert(bytes.NewReader(serialize(t, p)))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Kind != "cpu" {
		t.Fatalf("kind = %q, want cpu", res.Kind)
	}
	if res.DefaultType != "cpu/nanoseconds" {
		t.Fatalf("default type = %q", res.DefaultType)
	}
	if len(res.ExtraColumns) != 1 || res.ExtraColumns[0] != "samples" || res.ExtraTypes[0] != "samples/count" {
		t.Fatalf("extras = %v / %v", res.ExtraColumns, res.ExtraTypes)
	}
	if res.Rows != 2 {
		t.Fatalf("rows = %d, want 2 (identical stacks merged)", res.Rows)
	}
	if res.TotalValue != 60 {
		t.Fatalf("total value = %d, want 60", res.TotalValue)
	}

	schema, stacks, cols := decode(t, res.IPCStream)
	wantCols := []string{"stack", "leaf", "pkg", "value", "samples", "kind", "captured_at_unix_us", "period", "duration_ns"}
	if len(schema.Fields()) != len(wantCols) {
		t.Fatalf("schema has %d fields, want %d", len(schema.Fields()), len(wantCols))
	}
	for i, want := range wantCols {
		if schema.Field(i).Name != want {
			t.Fatalf("field %d = %q, want %q", i, schema.Field(i).Name, want)
		}
	}

	wantStacks := [][]string{
		{"main.main", "github.com/acme/mod/pkga.Work"},
		{"main.main", "github.com/acme/mod/pkga.Work", "github.com/acme/mod/pkga.Work"},
	}
	if len(stacks) != 2 {
		t.Fatalf("stacks = %v", stacks)
	}
	for i := range wantStacks {
		if strings.Join(stacks[i], "|") != strings.Join(wantStacks[i], "|") {
			t.Fatalf("stack %d = %v, want %v", i, stacks[i], wantStacks[i])
		}
	}
	if got := strs(t, cols, "leaf"); got[0] != "github.com/acme/mod/pkga.Work" || got[1] != got[0] {
		t.Fatalf("leaf = %v", got)
	}
	if got := strs(t, cols, "pkg"); got[0] != "github.com/acme/mod/pkga" {
		t.Fatalf("pkg = %v", got)
	}
	if got := int64s(t, cols, "value"); got[0] != 50 || got[1] != 10 {
		t.Fatalf("value = %v", got)
	}
	if got := int64s(t, cols, "samples"); got[0] != 5 || got[1] != 1 {
		t.Fatalf("samples = %v", got)
	}
	if got := strs(t, cols, "kind"); got[0] != "cpu" {
		t.Fatalf("kind col = %v", got)
	}
	if got := int64s(t, cols, "captured_at_unix_us"); got[0] != 1_700_000_000_123_456 {
		t.Fatalf("captured_at_unix_us = %v", got)
	}
	if got := int64s(t, cols, "period"); got[0] != 10_000_000 {
		t.Fatalf("period = %v", got)
	}
	if got := int64s(t, cols, "duration_ns"); got[0] != 2_000_000_000 {
		t.Fatalf("duration_ns = %v", got)
	}
}

func TestInlineExpansion(t *testing.T) {
	fns, byId := buildFunctions(
		fnSpec{1, "main.main"},
		fnSpec{2, "github.com/acme/mod/pkga.Caller"},
		fnSpec{3, "github.com/acme/mod/pkga.Inlined"},
	)
	lMain := loc(1, byId, 1)
	// One location carrying an inline pair: Line[0] is the innermost
	// (callee), Line[1] the function it was inlined into.
	lInl := loc(2, byId, 3, 2)
	p := &profile.Profile{
		SampleType: cpuValueTypes(),
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Function:   fns,
		Location:   []*profile.Location{lMain, lInl},
		Sample: []*profile.Sample{
			{Location: []*profile.Location{lInl, lMain}, Value: []int64{1, 7}},
		},
	}
	res, err := Convert(bytes.NewReader(serialize(t, p)))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	_, stacks, cols := decode(t, res.IPCStream)
	want := []string{"main.main", "github.com/acme/mod/pkga.Caller", "github.com/acme/mod/pkga.Inlined"}
	if strings.Join(stacks[0], "|") != strings.Join(want, "|") {
		t.Fatalf("stack = %v, want %v", stacks[0], want)
	}
	if got := strs(t, cols, "leaf"); got[0] != "github.com/acme/mod/pkga.Inlined" {
		t.Fatalf("leaf = %v", got)
	}
}

func TestHeapKindDefaultAndExtraRouting(t *testing.T) {
	fns, byId := buildFunctions(fnSpec{1, "main.alloc"})
	l := loc(1, byId, 1)
	base := func() *profile.Profile {
		return &profile.Profile{
			SampleType: heapValueTypes(),
			PeriodType: &profile.ValueType{Type: "space", Unit: "bytes"},
			Function:   fns,
			Location:   []*profile.Location{l},
			Sample: []*profile.Sample{
				{Location: []*profile.Location{l}, Value: []int64{11, 22, 33, 44}},
			},
		}
	}

	// No DefaultSampleType: pprof's rule picks the last type.
	res, err := Convert(bytes.NewReader(serialize(t, base())))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Kind != "heap" || res.DefaultType != "inuse_space/bytes" {
		t.Fatalf("kind/default = %q/%q", res.Kind, res.DefaultType)
	}
	_, _, cols := decode(t, res.IPCStream)
	if got := int64s(t, cols, "value"); got[0] != 44 {
		t.Fatalf("value = %v", got)
	}
	for col, want := range map[string]int64{"alloc_objects": 11, "alloc_space": 22, "inuse_objects": 33} {
		if got := int64s(t, cols, col); got[0] != want {
			t.Fatalf("%s = %v, want %d", col, got, want)
		}
	}

	// DefaultSampleType=alloc_space (the runtime's "allocs" profile):
	// kind flips, value re-routes, inuse_space becomes an extra.
	pa := base()
	pa.DefaultSampleType = "alloc_space"
	res, err = Convert(bytes.NewReader(serialize(t, pa)))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Kind != "allocs" || res.DefaultType != "alloc_space/bytes" {
		t.Fatalf("kind/default = %q/%q", res.Kind, res.DefaultType)
	}
	_, _, cols = decode(t, res.IPCStream)
	if got := int64s(t, cols, "value"); got[0] != 22 {
		t.Fatalf("value = %v", got)
	}
	if got := int64s(t, cols, "inuse_space"); got[0] != 44 {
		t.Fatalf("inuse_space = %v", got)
	}
}

func TestContentionKindAndHint(t *testing.T) {
	fns, byId := buildFunctions(fnSpec{1, "main.lock"})
	l := loc(1, byId, 1)
	p := &profile.Profile{
		SampleType: []*profile.ValueType{
			{Type: "contentions", Unit: "count"},
			{Type: "delay", Unit: "nanoseconds"},
		},
		PeriodType: &profile.ValueType{Type: "contentions", Unit: "count"},
		Function:   fns,
		Location:   []*profile.Location{l},
		Sample: []*profile.Sample{
			{Location: []*profile.Location{l}, Value: []int64{1, 100}},
		},
	}
	raw := serialize(t, p)

	res, err := Convert(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Kind != "contention" {
		t.Fatalf("kind = %q, want contention", res.Kind)
	}
	res, err = Convert(bytes.NewReader(raw), WithKindHint("mutex"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Kind != "mutex" {
		t.Fatalf("kind = %q, want mutex", res.Kind)
	}
}

func TestGoroutineSingleType(t *testing.T) {
	fns, byId := buildFunctions(fnSpec{1, "runtime.gopark"})
	l := loc(1, byId, 1)
	p := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "goroutine", Unit: "count"}},
		PeriodType: &profile.ValueType{Type: "goroutine", Unit: "count"},
		Function:   fns,
		Location:   []*profile.Location{l},
		Sample: []*profile.Sample{
			{Location: []*profile.Location{l}, Value: []int64{12}},
		},
	}
	res, err := Convert(bytes.NewReader(serialize(t, p)))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Kind != "goroutine" || len(res.ExtraColumns) != 0 || res.TotalValue != 12 {
		t.Fatalf("kind/extras/total = %q/%v/%d", res.Kind, res.ExtraColumns, res.TotalValue)
	}
	_, _, cols := decode(t, res.IPCStream)
	if got := strs(t, cols, "pkg"); got[0] != "runtime" {
		t.Fatalf("pkg = %v", got)
	}
}

func TestUnsymbolizedFallback(t *testing.T) {
	l := &profile.Location{ID: 1, Address: 0x1234}
	p := &profile.Profile{
		SampleType: cpuValueTypes(),
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Location:   []*profile.Location{l},
		Sample: []*profile.Sample{
			{Location: []*profile.Location{l}, Value: []int64{1, 5}},
		},
	}
	res, err := Convert(bytes.NewReader(serialize(t, p)))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	_, stacks, cols := decode(t, res.IPCStream)
	if len(stacks[0]) != 1 || stacks[0][0] != "0x1234" {
		t.Fatalf("stack = %v", stacks[0])
	}
	if got := strs(t, cols, "pkg"); got[0] != "" {
		t.Fatalf("pkg = %q, want empty", got[0])
	}
}

func TestPkgOf(t *testing.T) {
	cases := []struct{ name, want string }{
		{"runtime.mallocgc", "runtime"},
		{"main.main", "main"},
		{"main.main.func1", "main"},
		{"github.com/acme/mod/pkga.Work", "github.com/acme/mod/pkga"},
		{"github.com/acme/mod/pkga.(*Thing).Work", "github.com/acme/mod/pkga"},
		{"github.com/acme/mod/pkga.Generic[go.shape.int]", "github.com/acme/mod/pkga"},
		{"0x1234", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := pkgOf(c.name); got != c.want {
			t.Fatalf("pkgOf(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSanitizeColumn(t *testing.T) {
	cases := []struct{ in, want string }{
		{"alloc_objects", "alloc_objects"},
		{"inuse space", "inuse_space"},
		{"délai", "d_lai"},
		{"9lives", "_9lives"},
		{"", "_"},
	}
	for _, c := range cases {
		if got := sanitizeColumn(c.in); got != c.want {
			t.Fatalf("sanitizeColumn(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReservedColumnCollision(t *testing.T) {
	fns, byId := buildFunctions(fnSpec{1, "main.main"})
	l := loc(1, byId, 1)
	p := &profile.Profile{
		SampleType: []*profile.ValueType{
			{Type: "kind", Unit: "count"},
			{Type: "cpu", Unit: "nanoseconds"},
		},
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Function:   fns,
		Location:   []*profile.Location{l},
		Sample: []*profile.Sample{
			{Location: []*profile.Location{l}, Value: []int64{1, 5}},
		},
	}
	_, err := Convert(bytes.NewReader(serialize(t, p)))
	if err == nil || !strings.Contains(err.Error(), "reserved column") {
		t.Fatalf("err = %v, want reserved-column refusal", err)
	}
}

func TestEmptyProfile(t *testing.T) {
	p := &profile.Profile{
		SampleType: cpuValueTypes(),
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
	}
	res, err := Convert(bytes.NewReader(serialize(t, p)))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if res.Rows != 0 || res.TotalValue != 0 {
		t.Fatalf("rows/total = %d/%d", res.Rows, res.TotalValue)
	}
	schema, stacks, _ := decode(t, res.IPCStream)
	if len(stacks) != 0 || len(schema.Fields()) != 9 {
		t.Fatalf("empty stream decodes to %d stacks, %d fields", len(stacks), len(schema.Fields()))
	}
}

func TestDeterminism(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cpu.pb.gz"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	a, err := Convert(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	b, err := Convert(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !bytes.Equal(a.IPCStream, b.IPCStream) {
		t.Fatal("two conversions of the same profile differ")
	}
}

// --- captured fixtures ---------------------------------------------------
//
// testdata/*.pb.gz are real runtime/pprof captures (regenerate with
// BOXER_PPROFARROW_REGEN=1 go test -run TestRegenFixtures ./...). The
// assertions are structural — kind, schema, and value conservation
// against an independent parse — so a regenerated fixture stays green.

func convertFixture(t *testing.T, name string) (res Result, p *profile.Profile) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture (regenerate with BOXER_PPROFARROW_REGEN=1): %v", err)
	}
	p, err = profile.Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("independent parse: %v", err)
	}
	res, err = Convert(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return
}

func assertConservation(t *testing.T, res Result, p *profile.Profile) {
	t.Helper()
	defaultIdx := len(p.SampleType) - 1
	if p.DefaultSampleType != "" {
		for i, st := range p.SampleType {
			if st.Type == p.DefaultSampleType {
				defaultIdx = i
			}
		}
	}
	var want int64
	for _, s := range p.Sample {
		want += s.Value[defaultIdx]
	}
	if res.TotalValue != want {
		t.Fatalf("total value = %d, independent sum = %d", res.TotalValue, want)
	}
	// The IPC payload must agree with the metadata, not just the meta
	// with the profile.
	_, stacks, cols := decode(t, res.IPCStream)
	if uint64(len(stacks)) != res.Rows {
		t.Fatalf("ipc rows = %d, meta rows = %d", len(stacks), res.Rows)
	}
	var got int64
	for _, v := range int64s(t, cols, "value") {
		got += v
	}
	if got != want {
		t.Fatalf("ipc value sum = %d, want %d", got, want)
	}
}

func TestFixtureCPU(t *testing.T) {
	res, p := convertFixture(t, "cpu.pb.gz")
	if res.Kind != "cpu" {
		t.Fatalf("kind = %q", res.Kind)
	}
	if res.Rows == 0 || res.Rows > uint64(len(p.Sample)) {
		t.Fatalf("rows = %d for %d samples", res.Rows, len(p.Sample))
	}
	assertConservation(t, res, p)
}

func TestFixtureHeap(t *testing.T) {
	res, p := convertFixture(t, "heap.pb.gz")
	if res.Kind != "heap" {
		t.Fatalf("kind = %q", res.Kind)
	}
	if res.Rows == 0 {
		t.Fatal("no rows")
	}
	assertConservation(t, res, p)
}

// --- fixture regeneration ------------------------------------------------

// burn keeps the CPU busy with a call chain deep enough to be
// interesting, recursing so the recursive-stack path shows up in real
// fixtures too.
func burn(n int, until time.Time) (acc int) {
	if time.Now().After(until) {
		return
	}
	if n > 0 {
		acc = burn(n-1, until) + 1
		return
	}
	for i := range 1 << 14 {
		acc += i * i
	}
	if time.Now().Before(until) {
		acc += burn(3, until)
	}
	return
}

var sink [][]byte

func TestRegenFixtures(t *testing.T) {
	if os.Getenv("BOXER_PPROFARROW_REGEN") == "" {
		t.Skip("set BOXER_PPROFARROW_REGEN=1 to regenerate testdata fixtures")
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}

	cpuF, err := os.Create(filepath.Join("testdata", "cpu.pb.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if err = pprof.StartCPUProfile(cpuF); err != nil {
		t.Fatal(err)
	}
	burn(3, time.Now().Add(600*time.Millisecond))
	pprof.StopCPUProfile()
	if err = cpuF.Close(); err != nil {
		t.Fatal(err)
	}

	sink = nil
	for range 512 {
		sink = append(sink, make([]byte, 4096))
	}
	runtime.GC()
	heapF, err := os.Create(filepath.Join("testdata", "heap.pb.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if err = pprof.Lookup("heap").WriteTo(heapF, 0); err != nil {
		t.Fatal(err)
	}
	if err = heapF.Close(); err != nil {
		t.Fatal(err)
	}
}
