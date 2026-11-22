package imagescope

import (
	"bytes"
	"context"
	"encoding/hex"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	"github.com/stergiotis/boxer/public/science/imaging/decode/decodetest"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/egui2test"
)

// testReaders is the registry the tests' windows read files through: the
// built-in readers and the conformance kit's test format, which stands in
// for a format a host registers (ADR-0398 §SD9).
var testReaders = func() *decode.Registry {
	reg := decode.NewRegistry()
	for _, r := range append(decode.BuiltinReaders(), decodetest.Reader()) {
		if err := reg.Register(r); err != nil {
			panic(err)
		}
	}
	return reg
}()

// testFile writes members in the test format.
func testFile(t *testing.T, members ...decodetest.Member) []byte {
	t.Helper()
	b, err := decodetest.Write(members...)
	require.NoError(t, err)
	return b
}

// stackOf assembles a stack, failing the test on a mistake.
func stackOf(t *testing.T, axes []imaging.Axis, planes []imaging.Plane, model imaging.ColorModelE, p imaging.Placement, notes ...string) imaging.Stack {
	t.Helper()
	st, err := imaging.NewStack(axes, planes, model, p, imaging.Provenance{Notes: notes})
	require.NoError(t, err)
	return st
}

// ramp is a plane of kind whose samples count up from base.
func ramp(t *testing.T, kind imaging.SampleKindE, w, h int, base int) imaging.Plane {
	t.Helper()
	var p imaging.Plane
	var err error
	n := w * h
	switch kind {
	case imaging.SampleKindU8:
		s := make([]uint8, n)
		for i := range s {
			s[i] = uint8(base + i)
		}
		p, err = imaging.NewPlaneU8(w, h, s)
	case imaging.SampleKindU16:
		s := make([]uint16, n)
		for i := range s {
			s[i] = uint16(base + i*3)
		}
		p, err = imaging.NewPlaneU16(w, h, s)
	case imaging.SampleKindI16:
		s := make([]int16, n)
		for i := range s {
			s[i] = int16(base + i - n/2)
		}
		p, err = imaging.NewPlaneI16(w, h, s)
	case imaging.SampleKindF32:
		s := make([]float32, n)
		for i := range s {
			s[i] = float32(base) + float32(i)/7
		}
		p, err = imaging.NewPlaneF32(w, h, s)
	}
	require.NoError(t, err)
	return p
}

// geoPlacement places pixels of half a metre in a reference system.
var geoPlacement = imaging.Placement{
	Affine: [6]float64{2_600_000, 0.5, 0, 1_200_000, 0, -0.5},
	Frame: imaging.Frame{Kind: imaging.FrameKindReferenceSystem, Code: "EPSG:2056",
		AxisNames: [2]string{"E", "N"}, Units: [2]string{"m", "m"}},
	Source: "a test placement",
}

// skyPlacement is a gnomonic placement of 1″ pixels about RA 150°, Dec 2°.
func skyPlacement() imaging.Placement {
	const s = 1.0 / 3600
	return imaging.Placement{
		Affine: [6]float64{8 * s, -s, 0, 6 * s, 0, -s},
		Frame: imaging.Frame{Kind: imaging.FrameKindCelestialGnomonic, Code: "ICRS",
			AxisNames: [2]string{"RA", "Dec"}, Units: [2]string{"deg", "deg"},
			RefLon: 150, RefLat: 2, LonPole: imaging.DefaultLonPole(2)},
		Source: "a test projection",
	}
}

// The stacks the tests open from files, each written in the test format.
func pagesStack(t *testing.T) imaging.Stack {
	return stackOf(t, []imaging.Axis{{Name: "page", Role: imaging.AxisRoleSelector, Size: 3}, {Name: "band", Role: imaging.AxisRoleBand, Size: 1}},
		[]imaging.Plane{ramp(t, imaging.SampleKindU8, 24, 16, 0), ramp(t, imaging.SampleKindU8, 24, 16, 10), ramp(t, imaging.SampleKindU8, 24, 16, 20)},
		imaging.ColorModelGray, imaging.Placement{})
}

func nodataStack(t *testing.T) imaging.Stack {
	p := ramp(t, imaging.SampleKindI16, 20, 10, 0)
	p, err := p.WithTransform(imaging.ValueTransform{Scale: 0.5, Offset: 10})
	require.NoError(t, err)
	p, err = p.WithInvalid(-100)
	require.NoError(t, err)
	return stackOf(t, []imaging.Axis{{Name: "band", Role: imaging.AxisRoleBand, Size: 1, Labels: []string{"near infrared"}}},
		[]imaging.Plane{p}, 0, imaging.Placement{}, "the band's scale and offset were read from the file's metadata")
}

func geoStack(t *testing.T) imaging.Stack {
	return stackOf(t, []imaging.Axis{{Name: "band", Role: imaging.AxisRoleBand, Size: 1}},
		[]imaging.Plane{ramp(t, imaging.SampleKindU16, 32, 24, 0)}, imaging.ColorModelGray, geoPlacement)
}

// cubeStack is a cube read, its provenance says, as a variant of the test
// format.
func cubeStack(t *testing.T) imaging.Stack {
	t.Helper()
	st, err := imaging.NewStack([]imaging.Axis{{Name: "axis3", Role: imaging.AxisRoleSelector, Size: 3}},
		[]imaging.Plane{ramp(t, imaging.SampleKindI16, 12, 8, 0), ramp(t, imaging.SampleKindI16, 12, 8, 100), ramp(t, imaging.SampleKindI16, 12, 8, 200)},
		0, imaging.Placement{}, imaging.Provenance{Variant: "cubic", Notes: []string{"rows were flipped so that row 0 is the top"}})
	require.NoError(t, err)
	return st
}

func skyStack(t *testing.T) imaging.Stack {
	return stackOf(t, nil, []imaging.Plane{ramp(t, imaging.SampleKindF32, 16, 12, 0)}, 0, skyPlacement())
}

func rgbF32Stack(t *testing.T) imaging.Stack {
	return stackOf(t, []imaging.Axis{{Name: "band", Role: imaging.AxisRoleBand, Size: 3}},
		[]imaging.Plane{ramp(t, imaging.SampleKindF32, 24, 16, 0), ramp(t, imaging.SampleKindF32, 24, 16, 5), ramp(t, imaging.SampleKindF32, 24, 16, 9)},
		imaging.ColorModelRGB, imaging.Placement{})
}

// multiFile is a file of four entries: a cube, the sample bands — the
// default — an entry the reader refuses, and pages.
func multiFile(t *testing.T) []byte {
	bands, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	return testFile(t,
		decodetest.Member{Id: "cube", Title: "the cube", Stack: cubeStack(t)},
		decodetest.Member{Id: "bands", Title: "the bands", Stack: bands, Default: true},
		decodetest.Member{Id: "jpeg", Title: "a page in JPEG", Unsupported: "compression 7 (JPEG)", Stack: geoStack(t)},
		decodetest.Member{Id: "pages", Title: "the pages", Stack: pagesStack(t)})
}

// decodedEntry is an entry of a file decoded directly, to hold the window
// against.
func decodedEntry(t *testing.T, body []byte, id string) imaging.Stack {
	t.Helper()
	st, err := testReaders.Decode(context.Background(), bytes.NewReader(body), int64(len(body)), decode.Request{SourceName: "x", Entry: id})
	require.NoError(t, err)
	return st
}

// list_formats lists what the window's registry reads.
func TestListFormatsIsTheRegistrysFormats(t *testing.T) {
	inst := testApp(t)
	out, err := query[appops.None, Formats](t, inst, opListFormats, appops.None{})
	require.NoError(t, err)
	var names []string
	for _, f := range out.Formats {
		names = append(names, f.Name)
		assert.NotEmpty(t, f.Title)
		assert.Positive(t, f.Version)
	}
	assert.Equal(t, []string{decodetest.FormatName, "jpeg", "png"}, names)
	assert.Equal(t, []string{".png"}, out.Formats[2].Extensions)
	assert.Contains(t, out.Note, "first bytes")

	// The window of a host that registered nothing reads the built-ins.
	plain := testApp(t)
	plain.readers = nil
	out, err = query[appops.None, Formats](t, plain, opListFormats, appops.None{})
	require.NoError(t, err)
	assert.Len(t, out.Formats, 2)
}

// A file of several entries opens on its default, and get_view names the
// file, its format and the entry shown.
func TestAFileOfSeveralEntriesOpensOnItsDefault(t *testing.T) {
	body := multiFile(t)
	inst := testApp(t)
	openBytes(t, inst, "multi.decodetest", body)
	require.Empty(t, inst.openErr)
	v := getView(t, inst)
	require.NotNil(t, v.File)
	assert.Equal(t, FileInfo{Name: "multi.decodetest", Format: decodetest.FormatName, FormatTitle: "the conformance kit's test format",
		ReaderVersion: 1, Entry: "bands", EntryTitle: "the bands", Entries: 4, Held: true}, *v.File)
	require.NotNil(t, v.Stack)
	assert.Equal(t, "bands", v.Stack.Shown.Entry)
	assert.Equal(t, decodedEntry(t, body, "bands").PlaneCount(), v.Stack.Planes)

	page, err := query[GetEntriesArgs, EntriesPage](t, inst, opGetEntries, GetEntriesArgs{})
	require.NoError(t, err)
	assert.Equal(t, 4, page.Total)
	assert.Equal(t, "bands", page.Shown)
	assert.False(t, page.Truncated)
	require.Len(t, page.Entries, 4)
	assert.Equal(t, EntryInfo{Index: 0, Id: "cube", Title: "the cube", Width: 12, Height: 8, Planes: 3, Kind: "i16", Readable: true}, page.Entries[0])
	assert.True(t, page.Entries[1].Default)
	assert.True(t, page.Entries[1].Shown)
	assert.Equal(t, EntryInfo{Index: 2, Id: "jpeg", Title: "a page in JPEG", Width: 32, Height: 24, Planes: 1, Kind: "u16",
		Refusal: "unsupported", Reason: "compression 7 (JPEG)"}, page.Entries[2])
	page, err = query[GetEntriesArgs, EntriesPage](t, inst, opGetEntries, GetEntriesArgs{From: 1, Count: 2})
	require.NoError(t, err)
	assert.Equal(t, 1, page.From)
	assert.Equal(t, []string{"bands", "jpeg"}, []string{page.Entries[0].Id, page.Entries[1].Id})
	assert.True(t, page.Truncated)
	_, err = query[GetEntriesArgs, EntriesPage](t, inst, opGetEntries, GetEntriesArgs{From: 4})
	assert.Contains(t, refusal(t, err), "entry 4 is outside the file, whose entries are 0 to 3")
}

// set_entry shows another entry from the bytes held, off the frame, and
// settles when it is shown; the regions and the results stay; read_pixels
// and measure name the entry.
func TestSetEntryShowsAnotherEntry(t *testing.T) {
	body := multiFile(t)
	inst := testApp(t)
	openBytes(t, inst, "multi.decodetest", body)
	_, err := commandAs[RegionSpec, AddRegionOutcome](t, inst, "person", opAddRegion, rectSpec(5, 5, 4, 4))
	require.NoError(t, err)
	regions := len(inst.regs)
	require.Positive(t, regions)

	require.NoError(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "cube"}))
	assert.Equal(t, phaseSelecting, inst.observePhase(), "an entry is read as a selection is taken")
	assert.Contains(t, refusal(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "time", Position: 1})), "another entry of the file is being read")
	settleShown(t, inst)
	out, settled, err := settleAsync[SetEntryArgs, SelectionOutcome](t, inst, opSetEntry, SetEntryArgs{Id: "cube"})
	require.NoError(t, err)
	require.True(t, settled)
	assert.Equal(t, "cube", out.Selection.Entry)
	assert.Equal(t, "viewing", out.Phase)
	assert.Contains(t, out.Note, "the entry the cube is shown")

	cube := decodedEntry(t, body, "cube")
	v := getView(t, inst)
	assert.Equal(t, "cube", v.File.Entry)
	assert.Equal(t, "the cube", v.File.EntryTitle)
	assert.Equal(t, "cubic", v.File.Variant, "the variant the entry shown was read as")
	assert.Contains(t, inst.phaseLine(), "multi.decodetest · decodetest (cubic) · 12 × 8", "beside the format in the status row")
	assert.Equal(t, 3, v.Stack.Planes)
	assert.Equal(t, "axis3", v.Stack.Axes[0].Name)
	want, err := imageOf(cube, defaultSelection(cube))
	require.NoError(t, err)
	assert.Equal(t, 12, v.Image.Width)
	assert.Contains(t, v.Image.Notes, "rows were flipped so that row 0 is the top")
	h := want.Hash()
	assert.Equal(t, hex.EncodeToString(h[:]), v.Image.SampleHash)
	assert.Equal(t, regions, len(inst.regs), "the regions stay")
	assert.Equal(t, openedId, v.ShownImage)

	px, err := query[RegionArgs, Pixels](t, inst, opReadPixels, RegionArgs{Col: 1, Row: 1})
	require.NoError(t, err)
	require.NotNil(t, px.Selection)
	assert.Equal(t, "cube", px.Selection.Entry, "read_pixels names the entry")
	require.NoError(t, command(t, inst, opSetRegion, RegionArgs{Col: 0, Row: 0, Width: 4, Height: 4}))
	settle(t, inst, func() bool { return inst.measured != nil })
	m, err := query[RegionArgs, Measurement](t, inst, opMeasure, RegionArgs{})
	require.NoError(t, err)
	assert.Equal(t, "cube", m.Selection.Entry, "measure names the entry")

	// A position of the new stack, then the same entry again: nothing more.
	require.NoError(t, command(t, inst, opSetPosition, SetPositionArgs{Axis: "axis3", Position: 2}))
	settleShown(t, inst)
	require.NoError(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "cube"}))
	assert.False(t, inst.selectionPending(), "the entry shown asks for nothing")
	assert.Equal(t, 2, inst.stk.shown.at[0])

	// Back to the default, and the refusals.
	require.NoError(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "bands"}))
	settleShown(t, inst)
	assert.Equal(t, "bands", getView(t, inst).File.Entry)
	assert.Contains(t, refusal(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "jpeg"})),
		`the entry "jpeg" (a page in JPEG) is one its reader does not read: compression 7 (JPEG); choose a readable one`)
	assert.Contains(t, refusal(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "page 9"})),
		`the file has no entry "page 9"; its entries are "cube", "bands", "jpeg", "pages"`)
	assert.False(t, inst.selectionPending(), "a refused command asks for nothing")
}

// An entry the reader refuses while reading it leaves the entry shown, and
// the outcome says why, with the kind of refusal.
func TestAnEntryRefusedWhileReadKeepsTheOneShown(t *testing.T) {
	body := multiFile(t)
	inst := testApp(t)
	openBytes(t, inst, "multi.decodetest", body)
	shown := inst.pic
	// The bytes held are cut short, as a file whose entry points past its
	// end is: the reader finds out only when it reads the entry.
	f := *inst.file
	f.body = body[:len(body)-100]
	inst.file = &f
	require.NoError(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "pages"}))
	settleShown(t, inst)
	out, settled, err := settleAsync[SetEntryArgs, SelectionOutcome](t, inst, opSetEntry, SetEntryArgs{Id: "pages"})
	require.NoError(t, err)
	require.True(t, settled)
	assert.Contains(t, out.Error, `the entry "pages" could not be read`)
	assert.Contains(t, out.Error, "the file is malformed")
	assert.Same(t, shown, inst.pic)
	v := getView(t, inst)
	assert.Equal(t, "malformed", v.SelectionErrorKind)
	assert.Equal(t, "bands", v.File.Entry)
}

// With no file open, or a file of one entry, the entry operations say why
// not.
func TestTheEntryOperationsNeedAFileOfSeveral(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	_, err := query[GetEntriesArgs, EntriesPage](t, inst, opGetEntries, GetEntriesArgs{})
	assert.Contains(t, refusal(t, err), "no file is open")
	assert.Contains(t, refusal(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "x"})), "no file is open")
	assert.Nil(t, getView(t, inst).File)

	one := testFile(t, decodetest.Member{Id: "only", Stack: cubeStack(t)})
	inst = testApp(t)
	openBytes(t, inst, "one.decodetest", one)
	v := getView(t, inst)
	require.NotNil(t, v.File)
	assert.Equal(t, 1, v.File.Entries)
	assert.False(t, v.File.Held, "a file of one entry is not held")
	assert.Nil(t, inst.file.body)
	require.NoError(t, command(t, inst, opSetEntry, SetEntryArgs{Id: "only"}), "the entry shown asks for nothing")

	// A PNG names its format and its one entry.
	png := pngFile(t)
	inst = testApp(t)
	openBytes(t, inst, "x.png", png)
	require.Empty(t, inst.openErr)
	v = getView(t, inst)
	require.NotNil(t, v.File)
	assert.Equal(t, "png", v.File.Format)
	assert.Equal(t, "image", v.File.Entry)
	assert.Equal(t, 1, v.File.Entries)
	assert.False(t, v.StackSelection, "a PNG is streamed into tiles, not read as a stack")
}

// A file no reader claims is refused naming the formats this host reads;
// one two readers claim is refused naming both; get_view says which.
func TestUnknownAndAmbiguousFilesAreRefused(t *testing.T) {
	inst := withSample(t, samples.NameSmall)
	before := inst.pic
	openBytes(t, inst, "notes.txt", []byte("not an image at all"))
	assert.Equal(t, "notes.txt was refused: no reader claims the file (the formats read here: the conformance kit's test format; JPEG; PNG)", inst.openErr)
	assert.Same(t, before, inst.pic)
	v := getView(t, inst)
	assert.Equal(t, "unknown-format", v.ErrorKind)
	assert.Equal(t, inst.openErr, v.Error)

	reg := decode.NewRegistry()
	require.NoError(t, reg.Register(decodetest.Reader()))
	require.NoError(t, reg.Register(greedy{}))
	inst = testApp(t)
	inst.readers = reg
	openBytes(t, inst, "multi.decodetest", multiFile(t))
	assert.Contains(t, inst.openErr, "more than one reader claims the file: the conformance kit's test format (decodetest), Greedy (greedy)")
	assert.Equal(t, "ambiguous", getView(t, inst).ErrorKind)
	assert.Nil(t, inst.pic)

	// A new open clears the kind with the error.
	require.True(t, inst.openSample(samples.NameSmall, image.Rectangle{}))
	assert.Empty(t, getView(t, inst).ErrorKind)
}

// greedy claims every file.
type greedy struct{}

func (greedy) Format() decode.Format {
	return decode.Format{Name: "greedy", Title: "Greedy", Extensions: []string{".g"}, Version: 1}
}
func (greedy) Sniff(head []byte) bool { return len(head) > 0 }
func (greedy) Entries(context.Context, io.ReaderAt, int64) ([]decode.Entry, error) {
	return nil, decode.Unsupported("everything")
}
func (greedy) Decode(context.Context, io.ReaderAt, int64, decode.Request) (imaging.Stack, error) {
	return imaging.Stack{}, decode.Unsupported("everything")
}

// The entry picker draws for a file of several entries, and choosing one is
// a set_entry.
func TestTheEntryPickerDraws(t *testing.T) {
	inst := testApp(t)
	openBytes(t, inst, "multi.decodetest", multiFile(t))
	frame := renderFrames(t, inst)
	frame()
	inst.entryGesture("pages")
	assert.Equal(t, "pages", inst.want.entry)
	settle(t, inst, func() bool { frame(); return !inst.selectionPending() })
	assert.Equal(t, "pages", inst.file.shown)
	frame()
	inst.entryGesture("jpeg")
	assert.Contains(t, inst.problem, "compression 7 (JPEG)", "an unreadable entry is refused with its reason")
}

// renderFrames is a frame of the whole window against the headless
// harness, failing on a binding error.
func renderFrames(t *testing.T, inst *App) func() {
	t.Helper()
	t.Cleanup(egui2test.Install())
	sm := c.CurrentApplicationState.StateManager
	return func() {
		inst.takeResults()
		inst.mirrorPhase()
		inst.renderBody()
		require.False(t, typed.HasErrors(), typed.GetError())
		sm.ScriptReset()
	}
}

// pngFile is a small PNG.
func pngFile(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 12, 9))
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}
