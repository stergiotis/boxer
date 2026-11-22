package decode_test

import (
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	"github.com/stergiotis/boxer/public/science/imaging/decode/decodetest"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
)

// noisy is a w × h picture of noise, which compresses badly, so that a
// file of it is much larger than its header.
func noisy(w, h int, gray bool) image.Image {
	rng := rand.New(rand.NewPCG(3, 4))
	if gray {
		img := image.NewGray16(image.Rect(0, 0, w, h))
		for i := range img.Pix {
			img.Pix[i] = byte(rng.Uint32())
		}
		return img
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(rng.Uint32())
	}
	return img
}

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func jpegOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}))
	return b.Bytes()
}

func testFile(t *testing.T, members ...decodetest.Member) []byte {
	t.Helper()
	b, err := decodetest.Write(members...)
	require.NoError(t, err)
	return b
}

func bandsStack(t *testing.T) imaging.Stack {
	t.Helper()
	st, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	return st
}

// deepJPEG is a 12-bit baseline JPEG, which the standard decoder does not
// read: a file with no readable entry.
func deepJPEG(t *testing.T) []byte {
	t.Helper()
	data := jpegOf(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	i := bytes.Index(data, []byte{0xff, 0xc0})
	require.Positive(t, i)
	data[i+4] = 12
	return data
}

// The kit passes the built-in readers, over a file with nothing readable
// among the rest.
func TestTheBuiltinReadersPassTheKit(t *testing.T) {
	readers := decode.BuiltinReaders()
	require.Len(t, readers, 2)
	decodetest.Check(t, readers[0],
		decodetest.File{Name: "noise.png", Data: pngOf(t, noisy(96, 64, false)),
			Entries: []decodetest.Expect{{Id: "image", Width: 96, Height: 64, Planes: 4, Kind: imaging.SampleKindU8}}},
		decodetest.File{Name: "noise16.png", Data: pngOf(t, noisy(64, 64, true)),
			Entries: []decodetest.Expect{{Id: "image", Width: 64, Height: 64, Planes: 1, Kind: imaging.SampleKindU16}}})
	decodetest.Check(t, readers[1],
		decodetest.File{Name: "noise.jpg", Data: jpegOf(t, noisy(96, 64, false)),
			Entries: []decodetest.Expect{{Id: "image", Width: 96, Height: 64, Planes: 3, Kind: imaging.SampleKindU8}}},
		decodetest.File{Name: "grey.jpg", Data: jpegOf(t, image.NewGray(image.Rect(0, 0, 40, 30))),
			Entries: []decodetest.Expect{{Id: "image", Width: 40, Height: 30, Planes: 1, Kind: imaging.SampleKindU8}}},
		decodetest.File{Name: "deep.jpg", Data: deepJPEG(t),
			Entries: []decodetest.Expect{{Id: "image", Refusal: decode.RefusalUnsupported}}})
}

// A built-in reader's stack, as stored, is the image Decode gives: the same
// digest, model and provenance.
func TestABuiltinStackAsStoredIsTheDecodedImage(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"noise.png", pngOf(t, noisy(33, 17, false))},
		{"noise16.png", pngOf(t, noisy(20, 10, true))},
		{"noise.jpg", jpegOf(t, noisy(33, 17, false))},
		{"grey.jpg", jpegOf(t, image.NewGray(image.Rect(0, 0, 9, 7)))},
	} {
		want, err := decode.Decode(tc.data, tc.name, decode.Options{})
		require.NoError(t, err, tc.name)
		st, err := decode.Default().Decode(context.Background(), bytes.NewReader(tc.data), int64(len(tc.data)), decode.Request{SourceName: tc.name})
		require.NoError(t, err, tc.name)
		img, ok, err := st.AsStored([]int{0})
		require.NoError(t, err, tc.name)
		require.True(t, ok, "%s: the band axis is the colour model", tc.name)
		assert.Equal(t, want.Hash(), img.Hash(), tc.name)
		assert.Equal(t, want.Model(), img.Model(), tc.name)
		wp, gp := want.Provenance(), img.Provenance()
		wp.SampleHash, gp.SampleHash = [32]byte{}, [32]byte{}
		assert.Equal(t, wp, gp, tc.name)
	}
}

// What the standard decoders do not read is an entry listed as unreadable,
// with the reason, and a decode of it is refused as unsupported.
func TestA12BitJPEGIsListedAndRefusedAsUnsupported(t *testing.T) {
	data := deepJPEG(t)
	f, entries, err := decode.Default().Entries(context.Background(), bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Equal(t, "jpeg", f.Name)
	require.Len(t, entries, 1)
	assert.False(t, entries[0].Readable)
	assert.True(t, entries[0].Default, "the file's one entry is the one meant")
	assert.Equal(t, decode.RefusalUnsupported, entries[0].Refusal)
	assert.Contains(t, entries[0].Reason, "12-bit baseline")
	_, err = decode.Default().Decode(context.Background(), bytes.NewReader(data), int64(len(data)), decode.Request{SourceName: "deep.jpg"})
	require.ErrorIs(t, err, decode.ErrUnsupported)
	assert.Equal(t, decode.RefusalUnsupported, decode.RefusalOf(err))
	assert.Contains(t, err.Error(), "the image: a 12-bit baseline, 1 component JPEG")

	png := pngOf(t, noisy(8, 8, false))
	_, err = decode.Default().Decode(context.Background(), bytes.NewReader(png[:len(png)-20]), int64(len(png)-20), decode.Request{})
	require.ErrorIs(t, err, decode.ErrMalformed)
	assert.Contains(t, err.Error(), "what the PNG decoder read")
}

func TestTheDefaultRegistryHoldsTheBuiltins(t *testing.T) {
	var names []string
	for _, f := range decode.Default().Formats() {
		names = append(names, f.Name)
		assert.NoError(t, f.Validate(), f.Name)
		assert.NotEmpty(t, f.Doc, f.Name)
	}
	assert.Equal(t, []string{"jpeg", "png"}, names, "nothing but the built-ins registers itself")
	r, ok := decode.Default().Lookup("png")
	require.True(t, ok)
	assert.Equal(t, "PNG", r.Format().Title)
	_, ok = decode.Default().Lookup("tiff")
	assert.False(t, ok)
}

func TestRegisterValidatesTheFormat(t *testing.T) {
	reg := decode.NewRegistry()
	for _, tc := range []struct {
		name   string
		mutate func(f *decode.Format)
	}{
		{"an upper-case name", func(f *decode.Format) { f.Name = "TIFF" }},
		{"a name beginning with a digit", func(f *decode.Format) { f.Name = "3d" }},
		{"a name with a space", func(f *decode.Format) { f.Name = "my format" }},
		{"a name too long", func(f *decode.Format) { f.Name = strings.Repeat("a", 33) }},
		{"no title", func(f *decode.Format) { f.Title = " " }},
		{"a title of two lines", func(f *decode.Format) { f.Title = "a\nb" }},
		{"an extension without its dot", func(f *decode.Format) { f.Extensions = []string{"tif"} }},
		{"an upper-case extension", func(f *decode.Format) { f.Extensions = []string{".TIF"} }},
		{"an extension twice", func(f *decode.Format) { f.Extensions = []string{".a", ".a"} }},
		{"version 0", func(f *decode.Format) { f.Version = 0 }},
	} {
		f := decodetest.Reader().Format()
		tc.mutate(&f)
		assert.Error(t, reg.Register(withFormat{decodetest.Reader(), f}), tc.name)
	}
	assert.Empty(t, reg.Formats(), "nothing refused was registered")
	assert.Error(t, reg.Register(nil))

	ok := decodetest.Reader().Format()
	ok.Name, ok.Extensions = "fits-gz", []string{".fits.gz", ".fz"}
	require.NoError(t, reg.Register(withFormat{decodetest.Reader(), ok}))
	require.NoError(t, reg.Register(decodetest.Reader()))
	assert.Error(t, reg.Register(decodetest.Reader()), "a name is registered once")
	var names []string
	for _, f := range reg.Formats() {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{decodetest.FormatName, "fits-gz"}, names, "by name")
}

// withFormat is a reader under another format.
type withFormat struct {
	decode.ReaderI
	f decode.Format
}

func (inst withFormat) Format() decode.Format { return inst.f }

// claimsAll is a reader that claims every file but the empty one.
type claimsAll struct{ decode.ReaderI }

func (claimsAll) Format() decode.Format {
	return decode.Format{Name: "greedy", Title: "Greedy", Extensions: []string{".g"}, Version: 1}
}
func (claimsAll) Sniff(head []byte) bool { return len(head) > 0 }

// A file no reader claims names what the registry reads; one two claim is
// refused naming both, whatever order they were registered in.
func TestSniffRefusesTheUnknownAndTheAmbiguous(t *testing.T) {
	_, err := decode.Default().Sniff([]byte("GIF89a"))
	require.ErrorIs(t, err, decode.ErrUnknownFormat)
	assert.Equal(t, "no reader claims the file (the formats read here: JPEG; PNG)", err.Error())
	assert.Equal(t, decode.RefusalUnknownFormat, decode.RefusalOf(err))
	_, err = decode.NewRegistry().Sniff([]byte("GIF89a"))
	assert.Equal(t, "no reader claims the file; no format is read here", err.Error())

	file := testFile(t, decodetest.Member{Stack: bandsStack(t)})
	for _, order := range [][]decode.ReaderI{{decodetest.Reader(), claimsAll{}}, {claimsAll{}, decodetest.Reader()}} {
		reg := decode.NewRegistry()
		for _, r := range order {
			require.NoError(t, reg.Register(r))
		}
		_, err := reg.Sniff(file)
		require.ErrorIs(t, err, decode.ErrAmbiguous)
		assert.Equal(t, "more than one reader claims the file: the conformance kit's test format (decodetest), Greedy (greedy); the host has to register only one of them", err.Error())
		_, err = reg.Decode(context.Background(), bytes.NewReader(file), int64(len(file)), decode.Request{})
		assert.Equal(t, decode.RefusalAmbiguous, decode.RefusalOf(err))
	}
	reg := decode.NewRegistry()
	require.NoError(t, reg.Register(decodetest.Reader()))
	r, err := reg.Sniff(append(file[:decode.SniffLen:decode.SniffLen], make([]byte, 100)...))
	require.NoError(t, err)
	assert.Equal(t, decodetest.FormatName, r.Format().Name, "the head is cut to SniffLen")
}

// Entries and Decode go to the reader that claims the file, and a refusal
// comes back as the reader's sentence.
func TestTheRegistryDecodesThroughTheReader(t *testing.T) {
	reg := decode.NewRegistry()
	require.NoError(t, reg.Register(decodetest.Reader()))
	bands := bandsStack(t)
	file := testFile(t,
		decodetest.Member{Id: "a", Stack: bands},
		decodetest.Member{Id: "b", Title: "a page refused", Unsupported: "compression 7 (JPEG)"})
	ctx, r, size := context.Background(), bytes.NewReader(file), int64(len(file))

	rd, err := reg.Identify(ctx, r, size)
	require.NoError(t, err)
	assert.Equal(t, decodetest.FormatName, rd.Format().Name)
	f, entries, err := reg.Entries(ctx, r, size)
	require.NoError(t, err)
	assert.Equal(t, decodetest.FormatName, f.Name)
	require.Len(t, entries, 2)
	assert.True(t, entries[0].Default)

	st, err := reg.Decode(ctx, r, size, decode.Request{SourceName: "x"})
	require.NoError(t, err)
	assert.Equal(t, bands.Hash(), st.Hash())

	_, err = reg.Decode(ctx, r, size, decode.Request{Entry: "b"})
	assert.Equal(t, "a page refused: compression 7 (JPEG): this reader does not read it", err.Error())
	assert.Equal(t, decode.RefusalUnsupported, decode.RefusalOf(err))
	_, err = reg.Decode(ctx, r, size, decode.Request{Entry: "c"})
	assert.Equal(t, decode.RefusalNoEntry, decode.RefusalOf(err))
	_, err = reg.Decode(ctx, r, size, decode.Request{Limits: decode.Limits{MaxPlanes: 11}})
	assert.Equal(t, "12 planes (MaxPlanes is 11): over a decode limit", err.Error())
	assert.Equal(t, decode.RefusalLimit, decode.RefusalOf(err))

	_, err = reg.Decode(ctx, r, size-1, decode.Request{})
	assert.Equal(t, decode.RefusalMalformed, decode.RefusalOf(err))
	_, err = reg.Identify(ctx, failingReader{}, 100)
	assert.Error(t, err)
	assert.Zero(t, decode.RefusalOf(err), "a failed read is no refusal")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = reg.Decode(cancelled, r, size, decode.Request{})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, decode.RefusalOf(err))
}

type failingReader struct{}

func (failingReader) ReadAt([]byte, int64) (int, error) { return 0, io.ErrClosedPipe }

func TestTheRefusalsReadAsSentences(t *testing.T) {
	cause := io.ErrUnexpectedEOF
	for _, tc := range []struct {
		err  error
		kind decode.RefusalE
		text string
	}{
		{decode.Unsupported("compression 7 (JPEG)"), decode.RefusalUnsupported, "compression 7 (JPEG): this reader does not read it"},
		{decode.OverLimit("a tile of 70000 × 70000 pixels"), decode.RefusalLimit, "a tile of 70000 × 70000 pixels: over a decode limit"},
		{decode.Malformed("the directory at 1200 is cut short"), decode.RefusalMalformed, "the directory at 1200 is cut short: the file is malformed"},
		{decode.MalformedWith("strip 3", cause), decode.RefusalMalformed, "strip 3: unexpected EOF: the file is malformed"},
		{decode.NoEntry("HDU 9"), decode.RefusalNoEntry, `the file holds no entry "HDU 9"`},
	} {
		assert.Equal(t, tc.text, tc.err.Error())
		assert.Equal(t, tc.kind, decode.RefusalOf(tc.err), tc.text)
		assert.ErrorIs(t, tc.err, tc.kind.Sentinel(), tc.text)
		wrapped := errors.Join(errors.New("context"), tc.err)
		assert.Equal(t, tc.kind, decode.RefusalOf(wrapped), "found through a wrap")
	}
	assert.ErrorIs(t, decode.MalformedWith("strip 3", cause), cause, "the cause matches too")
	for _, k := range []decode.RefusalE{decode.RefusalUnsupported, decode.RefusalLimit, decode.RefusalMalformed} {
		err := decode.Refuse(k, "the thing")
		assert.Equal(t, "the thing: "+k.Sentinel().Error(), err.Error())
		what, kind := decode.ReasonOf(errors.Join(errors.New("context"), err))
		assert.Equal(t, "the thing", what, "the bare words, through a wrap")
		assert.Equal(t, k, kind)
	}
	assert.Equal(t, decode.Unsupported("x").Error(), decode.Refuse(0, "x").Error(), "no kind is unsupported")
	what, kind := decode.ReasonOf(decode.MalformedWith("strip 3", cause))
	assert.Equal(t, "strip 3: unexpected EOF", what)
	assert.Equal(t, decode.RefusalMalformed, kind)
	what, kind = decode.ReasonOf(decode.NoEntry("HDU 9"))
	assert.Equal(t, `the file holds no entry "HDU 9"`, what)
	assert.Equal(t, decode.RefusalNoEntry, kind)
	what, kind = decode.ReasonOf(io.EOF)
	assert.Empty(t, what)
	assert.Zero(t, kind)
	assert.Equal(t, decode.Malformed("x").Error(), decode.MalformedWith("x", nil).Error())
	assert.Zero(t, decode.RefusalOf(nil))
	assert.Zero(t, decode.RefusalOf(io.EOF))
	assert.Equal(t, "none", decode.RefusalE(0).String())
	for _, r := range decode.AllRefusals {
		assert.NotEqual(t, "none", r.String())
		assert.NotNil(t, r.Sentinel())
	}
}

func TestLimitsCheckNamesWhatIsOver(t *testing.T) {
	assert.Equal(t, decode.Limits{MaxPixels: decode.DefaultMaxPixels, MaxBytes: decode.DefaultMaxBytes, MaxPlanes: decode.DefaultMaxPlanes},
		decode.Limits{MaxPixels: -1}.Resolved())
	l := decode.Limits{MaxPixels: 100, MaxBytes: 1000, MaxPlanes: 4}
	assert.NoError(t, l.Check(10, 10, 4, imaging.SampleKindU16), "800 bytes")
	assert.Equal(t, "a plane of 11 × 10 pixels (MaxPixels is 100): over a decode limit", l.Check(11, 10, 1, imaging.SampleKindU8).Error())
	assert.Equal(t, "5 planes (MaxPlanes is 4): over a decode limit", l.Check(2, 2, 5, imaging.SampleKindU8).Error())
	assert.Equal(t, "4 planes of 10 × 10 f32 samples (MaxBytes is 1000): over a decode limit", l.Check(10, 10, 4, imaging.SampleKindF32).Error())
	assert.ErrorIs(t, l.Check(-1, 10, 1, imaging.SampleKindU8), decode.ErrMalformed)
	huge := int64(1) << 62
	assert.ErrorIs(t, decode.Limits{}.Check(huge, huge, huge, imaging.SampleKindF64), decode.ErrLimit, "no overflow")
	assert.ErrorIs(t, decode.Limits{MaxPixels: huge}.Check(1<<31, 1<<31, 1, imaging.SampleKindF64), decode.ErrLimit, "no overflow")
	assert.NoError(t, decode.Limits{}.Check(0, 0, 0, 0))
}

func TestSelectEntry(t *testing.T) {
	entries := []decode.Entry{
		{Id: "0", Title: "page 1", Readable: true},
		{Id: "1", Title: "page 2", Readable: true, Default: true},
		decode.Entry{Id: "2", Title: "page 3"}.Refused(decode.Unsupported("compression 7 (JPEG)")),
		decode.Entry{Id: "3", Title: "page 4"}.Refused(decode.OverLimit("a tile of 70000 × 70000 pixels")),
	}
	assert.Equal(t, "compression 7 (JPEG)", entries[2].Reason, "the bare words")
	assert.Equal(t, decode.RefusalUnsupported, entries[2].Refusal)
	e, err := decode.SelectEntry(entries, "")
	require.NoError(t, err)
	assert.Equal(t, "1", e.Id)
	e, err = decode.SelectEntry(entries, "0")
	require.NoError(t, err)
	assert.Equal(t, "0", e.Id)
	_, err = decode.SelectEntry(entries, "2")
	assert.Equal(t, "page 3: compression 7 (JPEG): this reader does not read it", err.Error())
	_, err = decode.SelectEntry(entries, "3")
	assert.Equal(t, "page 4: a tile of 70000 × 70000 pixels: over a decode limit", err.Error())
	assert.Equal(t, decode.RefusalLimit, decode.RefusalOf(err), "the kind survives")
	_, err = decode.SelectEntry(entries, "9")
	assert.ErrorIs(t, err, decode.ErrNoEntry)

	// A file with nothing readable: its default is the one meant, refused
	// with its own kind.
	meant := []decode.Entry{
		decode.Entry{Id: "0", Title: "primary"}.Refused(decode.Unsupported("NAXIS < 2: the HDU holds no image")),
		decode.Entry{Id: "1", Title: "SCI", Default: true}.Refused(decode.Malformed("BSCALE = 0")),
	}
	_, err = decode.SelectEntry(meant, "")
	assert.Equal(t, "SCI: BSCALE = 0: the file is malformed", err.Error())
	assert.Equal(t, decode.RefusalMalformed, decode.RefusalOf(err))
	_, err = decode.SelectEntry(meant[:1], "")
	assert.ErrorIs(t, err, decode.ErrUnsupported, "no default named")
	_, err = decode.SelectEntry(nil, "")
	assert.ErrorIs(t, err, decode.ErrUnsupported)
	_, err = decode.SelectEntry([]decode.Entry{{Id: "x", Title: "x", Reason: "a reason", Default: true}}, "")
	assert.Equal(t, decode.RefusalUnsupported, decode.RefusalOf(err), "a zero Refusal is unsupported")
}

// A read the file ends before is malformed; any other failed read is no
// refusal, and its cause is found.
func TestReadFullAtTellsACutFileFromAFailedRead(t *testing.T) {
	data := []byte("0123456789")
	p := make([]byte, 4)
	require.NoError(t, decode.ReadFullAt(bytes.NewReader(data), p, 6, "the tail"))
	assert.Equal(t, "6789", string(p))
	err := decode.ReadFullAt(bytes.NewReader(data), p, 8, "the tail")
	assert.Equal(t, "the tail cut short: the file is malformed", err.Error())
	err = decode.ReadFullAt(bytes.NewReader(data), p, -1, "the tail")
	assert.Equal(t, decode.RefusalMalformed, decode.RefusalOf(err), "an offset before the start is a defect")
	_, rerr := decode.MarkReadFailures(bytes.NewReader(data)).ReadAt(p, -4)
	assert.Equal(t, decode.RefusalMalformed, decode.RefusalOf(decode.ReadError("the member", rerr)), "through a stream too")
	err = decode.ReadFullAt(failingReader{}, p, 0, "the head")
	assert.ErrorIs(t, err, io.ErrClosedPipe)
	assert.Zero(t, decode.RefusalOf(err), "a failed read is no refusal")

	assert.NoError(t, decode.ReadError("x", nil))
	assert.Equal(t, decode.RefusalMalformed, decode.RefusalOf(decode.ReadError("the header", io.ErrUnexpectedEOF)))
	assert.Equal(t, decode.RefusalMalformed, decode.RefusalOf(decode.ReadError("the header", io.EOF)))
	refused := decode.Unsupported("zip method 99")
	assert.Equal(t, refused, decode.ReadError("the member", refused), "a refusal passes as it is")
	assert.Equal(t, context.Canceled, decode.ReadError("the member", context.Canceled))
	err = decode.ReadError("the member", errors.New("lzw: invalid code"))
	assert.Equal(t, "the member: lzw: invalid code: the file is malformed", err.Error(), "what a stream reports of the bytes is a defect")

	// A failed read of a marked file keeps its mark through a decompressor.
	src := decode.MarkReadFailures(failingReader{})
	assert.Equal(t, src, decode.MarkReadFailures(src), "marked once")
	_, rerr = io.ReadFull(flate.NewReader(io.NewSectionReader(src, 0, 100)), p)
	err = decode.ReadError("the member", rerr)
	assert.ErrorIs(t, err, io.ErrClosedPipe)
	assert.Zero(t, decode.RefusalOf(err), "a failed read is no refusal, through a stream")
	_, rerr = io.ReadFull(flate.NewReader(io.NewSectionReader(decode.MarkReadFailures(bytes.NewReader([]byte{0xff, 0xff})), 0, 2)), p)
	assert.Equal(t, decode.RefusalMalformed, decode.RefusalOf(decode.ReadError("the member", rerr)), "a corrupt stream is a defect")
	_, rerr = io.ReadFull(io.NewSectionReader(decode.MarkReadFailures(bytes.NewReader(data)), 8, 2), p)
	assert.Equal(t, "the member cut short: the file is malformed", decode.ReadError("the member", rerr).Error())
}

// A registry is used from several goroutines at once.
func TestTheRegistryIsSafeForConcurrentUse(t *testing.T) {
	reg := decode.NewRegistry()
	file := testFile(t, decodetest.Member{Stack: bandsStack(t)})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if i == 0 {
				_ = reg.Register(decodetest.Reader())
			}
			for range 20 {
				_, _ = reg.Decode(context.Background(), bytes.NewReader(file), int64(len(file)), decode.Request{})
				_ = reg.Formats()
				_, _ = reg.Lookup(decodetest.FormatName)
			}
		})
	}
	wg.Wait()
	assert.Len(t, reg.Formats(), 1)
}
