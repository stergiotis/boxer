package decodetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
)

// imageStack is a sample image as a stack whose band axis is its model.
func imageStack(t *testing.T, name string) imaging.Stack {
	t.Helper()
	img, err := samples.ByName(name)
	require.NoError(t, err)
	planes := make([]imaging.Plane, img.PlaneCount())
	for i := range planes {
		planes[i] = img.Plane(i)
	}
	st, err := imaging.NewStack([]imaging.Axis{{Name: "band", Role: imaging.AxisRoleBand, Size: len(planes)}}, planes, img.Model(),
		img.Placement(), img.Provenance())
	require.NoError(t, err)
	return st
}

// cube is a 64 × 48 stack of 64-bit floats along a selector axis of n
// positions: 24 KiB a plane, so its samples dwarf its table.
func cube(t *testing.T, n int) imaging.Stack {
	t.Helper()
	planes := make([]imaging.Plane, n)
	for k := range planes {
		s := make([]float64, 64*48)
		for i := range s {
			s[i] = float64(i*(k+1)) / 7
		}
		p, err := imaging.NewPlaneF64(64, 48, s)
		require.NoError(t, err)
		planes[k] = p
	}
	st, err := imaging.NewStack([]imaging.Axis{{Name: "depth", Role: imaging.AxisRoleSelector, Size: n, Unit: "um"}}, planes, 0,
		imaging.Placement{}, imaging.Provenance{Variant: "cube", Notes: []string{"a cube made for the kit's tests"}})
	require.NoError(t, err)
	return st
}

func write(t *testing.T, members ...Member) []byte {
	t.Helper()
	b, err := Write(members...)
	require.NoError(t, err)
	return b
}

// A stack written in the test format decodes to the same stack: the same
// digest, axes, model, placement, invalid value, transforms, variant and
// notes.
func TestTheTestFormatCarriesTheWholeModel(t *testing.T) {
	bands, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	scaled := imageStack(t, samples.NameScaled)
	file := write(t,
		Member{Id: "bands", Title: "the sample bands", Detail: "u16, 3 × 4", Stack: bands},
		Member{Id: "scaled", Stack: scaled, Default: true},
		Member{Id: "jpeg-compressed", Title: "a page refused", Unsupported: "compression 7 (JPEG)", Stack: scaled},
		Member{Unsupported: "a member of no stack at all"},
		Member{Id: "cube", Stack: cube(t, 2)})
	r := Reader()
	assert.True(t, r.Sniff(file[:min(len(file), decode.SniffLen)]))

	entries, err := r.Entries(context.Background(), bytes.NewReader(file), int64(len(file)))
	require.NoError(t, err)
	assert.Equal(t, []decode.Entry{
		{Id: "bands", Title: "the sample bands", Detail: "u16, 3 × 4", Width: 160, Height: 120, Planes: 12, Kind: imaging.SampleKindU16, Readable: true},
		{Id: "scaled", Title: "member scaled", Width: scaled.Width(), Height: scaled.Height(), Planes: 1, Kind: scaled.Kind(), Readable: true, Default: true},
		{Id: "jpeg-compressed", Title: "a page refused", Width: scaled.Width(), Height: scaled.Height(), Planes: 1, Kind: scaled.Kind(),
			Refusal: decode.RefusalUnsupported, Reason: "compression 7 (JPEG)"},
		{Id: "3", Title: "member 3", Planes: 1, Refusal: decode.RefusalUnsupported, Reason: "a member of no stack at all"},
		{Id: "cube", Title: "member cube", Width: 64, Height: 48, Planes: 2, Kind: imaging.SampleKindF64, Readable: true},
	}, entries)

	for id, want := range map[string]imaging.Stack{"bands": bands, "scaled": scaled, "cube": cube(t, 2)} {
		got, err := r.Decode(context.Background(), bytes.NewReader(file), int64(len(file)), decode.Request{SourceName: "f.decodetest", Entry: id})
		require.NoError(t, err, id)
		assert.Equal(t, want.Hash(), got.Hash(), id)
		assert.Equal(t, want.Axes(), got.Axes(), id)
		assert.Equal(t, want.Model(), got.Model(), id)
		assert.Equal(t, want.Placement(), got.Placement(), id)
		p0, _ := want.Plane(want.Index(0))
		g0, _ := got.Plane(got.Index(0))
		wv, wok := p0.Invalid()
		gv, gok := g0.Invalid()
		assert.Equal(t, [2]any{wv, wok}, [2]any{gv, gok}, id)
		assert.Equal(t, p0.Transform(), g0.Transform(), id)
		assert.Equal(t, want.Provenance().Notes, got.Provenance().Notes, id)
		assert.Equal(t, want.Provenance().Variant, got.Provenance().Variant, id)
		assert.Equal(t, FormatName, got.Provenance().Format, id)
		assert.Equal(t, "f.decodetest", got.Provenance().SourceName, id)
	}

	_, err = r.Decode(context.Background(), bytes.NewReader(file), int64(len(file)), decode.Request{Entry: "jpeg-compressed"})
	require.ErrorIs(t, err, decode.ErrUnsupported)
	assert.Equal(t, "a page refused: compression 7 (JPEG): this reader does not read it", err.Error())
	_, err = r.Decode(context.Background(), bytes.NewReader(file), int64(len(file)), decode.Request{Entry: "page 9"})
	require.ErrorIs(t, err, decode.ErrNoEntry)
	assert.Equal(t, `the file holds no entry "page 9"`, err.Error())
}

func TestWriteRefusesWhatTheFormatCannotHold(t *testing.T) {
	st := cube(t, 1)
	for _, tc := range []struct {
		name    string
		members []Member
	}{
		{"a repeated id", []Member{{Id: "a", Stack: st}, {Id: "a", Stack: st}}},
		{"two defaults", []Member{{Stack: st, Default: true}, {Stack: st, Default: true}}},
		{"a refused default beside a readable member", []Member{{Default: true, Unsupported: "no"}, {Stack: st}}},
		{"a readable member without a stack", []Member{{Id: "empty"}}},
	} {
		_, err := Write(tc.members...)
		assert.Error(t, err, tc.name)
	}
	// With no default named the first readable member is the default.
	b := write(t, Member{Unsupported: "no"}, Member{Stack: st})
	entries, err := Reader().Entries(context.Background(), bytes.NewReader(b), int64(len(b)))
	require.NoError(t, err)
	assert.False(t, entries[0].Default)
	assert.True(t, entries[1].Default)
	// With nothing readable the first member is the default, the one meant.
	b = write(t, Member{Unsupported: "no"}, Member{Unsupported: "nor this"})
	entries, err = Reader().Entries(context.Background(), bytes.NewReader(b), int64(len(b)))
	require.NoError(t, err)
	assert.True(t, entries[0].Default)
	assert.False(t, entries[1].Default)
	_, err = Reader().Decode(context.Background(), bytes.NewReader(b), int64(len(b)), decode.Request{})
	assert.Equal(t, "member 0: no: this reader does not read it", err.Error())
}

// kitFiles are the test format's files the kit runs over.
func kitFiles(t *testing.T) []File {
	bands, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	return []File{
		{Name: "one.decodetest", Data: write(t, Member{Stack: cube(t, 1)})},
		{Name: "several.decodetest", Data: write(t,
			Member{Id: "cube", Stack: cube(t, 3)},
			Member{Id: "bands", Stack: bands, Default: true},
			Member{Id: "refused", Title: "a refused member", Unsupported: "compression 7 (JPEG)", Stack: cube(t, 2)},
			Member{Id: "colour", Stack: imageStack(t, samples.NameColour)}),
			Entries: []Expect{
				{Id: "cube", Width: 64, Height: 48, Planes: 3, Kind: imaging.SampleKindF64},
				{Id: "bands", Width: 160, Height: 120, Planes: 12, Kind: imaging.SampleKindU16},
				{Id: "refused", Unreadable: true},
				{Id: "colour", Width: 640, Height: 480, Planes: 4, Kind: imaging.SampleKindU8},
			}},
		{Name: "nothing-readable.decodetest", Data: write(t,
			Member{Id: "table", Unsupported: "a table: not an image"},
			Member{Id: "refused", Unsupported: "compression 7 (JPEG)", Stack: cube(t, 2), Default: true}),
			Entries: []Expect{
				{Id: "table", Refusal: decode.RefusalUnsupported},
				{Id: "refused", Unreadable: true, Width: 64, Height: 48, Planes: 2},
			}},
	}
}

// The kit passes the test format's own reader.
func TestTheKitPassesTheTestFormat(t *testing.T) {
	Check(t, Reader(), kitFiles(t)...)
}

// recorder is a testerI that records which subtests failed, by path.
type recorder struct {
	path     string
	failures *[]string
	messages *[]string
}

func (inst recorder) Helper() {}
func (inst recorder) Errorf(format string, args ...any) {
	*inst.failures = append(*inst.failures, inst.path)
	*inst.messages = append(*inst.messages, inst.path+": "+strings.SplitN(fmt.Sprintf(format, args...), "\n", 2)[0])
}
func (inst recorder) Logf(string, ...any) {}
func (inst recorder) Run(name string, f func(testerI)) bool {
	f(recorder{path: inst.path + "/" + name, failures: inst.failures, messages: inst.messages})
	return true
}

// failedSubtests runs the kit over a reader and lists the subtests that
// failed, each once, and their first lines.
func failedSubtests(reader decode.ReaderI, files []File) (failed []string, messages []string) {
	var all []string
	run(recorder{failures: &all, messages: &messages}, reader, files)
	for _, p := range all {
		if !slices.Contains(failed, p) {
			failed = append(failed, p)
		}
	}
	return
}

// A reader broken in one way: each override, when set, replaces the test
// format's own.
type broken struct {
	format  func() decode.Format
	sniff   func(head []byte) bool
	entries func(ctx context.Context, r io.ReaderAt, size int64) ([]decode.Entry, error)
	decode  func(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error)
}

var honest = Reader()

func (inst broken) Format() decode.Format {
	if inst.format != nil {
		return inst.format()
	}
	return honest.Format()
}

func (inst broken) Sniff(head []byte) bool {
	if inst.sniff != nil {
		return inst.sniff(head)
	}
	return honest.Sniff(head)
}

func (inst broken) Entries(ctx context.Context, r io.ReaderAt, size int64) ([]decode.Entry, error) {
	if inst.entries != nil {
		return inst.entries(ctx, r, size)
	}
	return honest.Entries(ctx, r, size)
}

func (inst broken) Decode(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
	if inst.decode != nil {
		return inst.decode(ctx, r, size, req)
	}
	return honest.Decode(ctx, r, size, req)
}

// Each broken reader fails exactly the subtest that names its fault, and
// the honest one fails none.
func TestTheKitFindsEachFault(t *testing.T) {
	file := write(t,
		Member{Id: "cube", Stack: cube(t, 3)},
		Member{Id: "single", Stack: cube(t, 1), Default: true},
		Member{Id: "refused", Unsupported: "compression 7 (JPEG)", Stack: cube(t, 1)})
	files := []File{{Name: "f", Data: file}}
	unwrap := func(err error) error {
		if errors.Is(err, decode.ErrMalformed) {
			return errors.New(err.Error())
		}
		return err
	}
	for _, tc := range []struct {
		name   string
		reader decode.ReaderI
		fails  string
	}{
		{"a format without a doc", broken{format: func() decode.Format {
			f := honest.Format()
			f.Doc = ""
			return f
		}}, "/format"},
		{"a sniff that claims the empty input", broken{sniff: func(head []byte) bool { return len(head) == 0 || honest.Sniff(head) }}, "/f/sniff"},
		{"two defaults", broken{entries: func(ctx context.Context, r io.ReaderAt, size int64) ([]decode.Entry, error) {
			entries, err := honest.Entries(ctx, r, size)
			for i := range entries {
				entries[i].Default = entries[i].Readable
			}
			return entries, err
		}}, "/f/entries"},
		{"a wrong plane count announced", broken{entries: func(ctx context.Context, r io.ReaderAt, size int64) ([]decode.Entry, error) {
			entries, err := honest.Entries(ctx, r, size)
			for i := range entries {
				entries[i].Planes++
			}
			return entries, err
		}}, "/f/decode"},
		{"samples read before a limit is checked", broken{decode: func(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
			_, _ = io.ReadAll(io.NewSectionReader(r, 0, size))
			return honest.Decode(ctx, r, size, req)
		}}, "/f/limits"},
		{"a cancel ignored", broken{decode: func(_ context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
			return honest.Decode(context.Background(), r, size, req)
		}}, "/f/cancel"},
		{"an error that wraps no refusal", broken{
			entries: func(ctx context.Context, r io.ReaderAt, size int64) ([]decode.Entry, error) {
				entries, err := honest.Entries(ctx, r, size)
				return entries, unwrap(err)
			},
			decode: func(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
				st, err := honest.Decode(ctx, r, size, req)
				return st, unwrap(err)
			}}, "/f/truncated"},
		{"an unreadable entry without its kind", broken{entries: func(ctx context.Context, r io.ReaderAt, size int64) ([]decode.Entry, error) {
			entries, err := honest.Entries(ctx, r, size)
			for i := range entries {
				if !entries[i].Readable {
					entries[i].Refusal = 0
				}
			}
			return entries, err
		}}, "/f/entries"},
		{"a reason that keeps its kind's words", broken{entries: func(ctx context.Context, r io.ReaderAt, size int64) ([]decode.Entry, error) {
			entries, err := honest.Entries(ctx, r, size)
			for i := range entries {
				if !entries[i].Readable {
					entries[i].Reason = decode.Refuse(entries[i].Refusal, entries[i].Reason).Error()
				}
			}
			return entries, err
		}}, "/f/entries"},
		{"a refusal of another kind than announced", broken{decode: func(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
			st, err := honest.Decode(ctx, r, size, req)
			if what, kind := decode.ReasonOf(err); kind == decode.RefusalUnsupported {
				err = decode.Malformed(what)
			}
			return st, err
		}}, "/f/decode"},
		{"an I/O failure reported as malformed", broken{decode: func(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
			st, err := honest.Decode(ctx, r, size, req)
			if err != nil && decode.RefusalOf(err) == 0 && ctx.Err() == nil {
				err = decode.MalformedWith("the samples", err)
			}
			return st, err
		}}, "/f/io-error"},
		{"a panic on a truncated file", broken{decode: func(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
			if size < int64(len(file)) {
				var planes []imaging.Plane
				_ = planes[size]
			}
			return honest.Decode(ctx, r, size, req)
		}}, "/f/truncated"},
		{"a decode that is not repeatable", func() decode.ReaderI {
			unlimited := 0
			return broken{decode: func(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
				if req.Entry == "cube" && req.Limits == (decode.Limits{}) {
					if unlimited++; unlimited > 1 {
						req.Entry = "single"
					}
				}
				return honest.Decode(ctx, r, size, req)
			}}
		}(), "/f/repeat"},
	} {
		failed, messages := failedSubtests(tc.reader, files)
		assert.Equal(t, []string{tc.fails}, failed, "%s: %s", tc.name, strings.Join(messages, "\n"))
	}
	failed, messages := failedSubtests(honest, files)
	assert.Empty(t, failed, strings.Join(messages, "\n"))
}

func TestTruncationLengths(t *testing.T) {
	assert.Equal(t, []int{0, 1, 2}, truncationLengths(3))
	l := truncationLengths(100_000)
	assert.True(t, slices.IsSorted(l))
	assert.Equal(t, 4095, l[4095])
	assert.Equal(t, 99_999, l[len(l)-1])
	assert.Less(t, len(l), 4096+512+64+1)
	assert.Equal(t, len(l), len(slices.Compact(slices.Clone(l))))
}
