package decode

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// BuiltinReaders are PNG and JPEG as readers of the seam (ADR-0398 §SD6),
// over the whole-file decode of [Decode]: one entry a file, read into a
// stack whose band axis is the image's colour model, so that
// [imaging.Stack.AsStored] gives the image [Decode] gives, digest and all.
// [Default] holds them from the start. They read the file whole, as
// [Decode] does, and honour a cancel before they do; [DecodeTiled] stays the
// way to stream a PNG into tiles.
func BuiltinReaders() []ReaderI { return []ReaderI{pngReader{}, jpegReader{}} }

// builtinEntry is the id of a PNG's or a JPEG's one entry.
const builtinEntry = "image"

// maxChunksBeforeData bounds the chunks or segments a header scan walks
// before the image data: real files have a few dozen.
const maxChunksBeforeData = 4096

type pngReader struct{}

var _ ReaderI = pngReader{}

func (pngReader) Format() Format {
	return Format{Name: FormatPNG.String(), Title: "PNG", Extensions: []string{".png"}, Version: 1,
		Doc: "Portable Network Graphics: greyscale, colour and palette images with or without alpha, 1 to 16 bits a sample, read by the standard library's decoder; an animation is read as its default image."}
}

func (pngReader) Sniff(head []byte) bool { return bytes.HasPrefix(head, pngSignature) }

func (inst pngReader) Entries(ctx context.Context, r io.ReaderAt, size int64) (entries []Entry, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	info, err := scanPNGAt(r, size)
	if err != nil {
		return
	}
	model, kind, prov, err := pngLayout(info, "")
	if err != nil {
		return nil, MalformedWith("the PNG header", err)
	}
	detail := strconv.Itoa(int(info.bitDepth)) + "-bit " + prov.StoredColorType
	if info.interlace != 0 {
		detail += ", interlaced"
	}
	return []Entry{{Id: builtinEntry, Title: "the image", Detail: detail, Width: info.width, Height: info.height,
		Planes: model.PlaneCount(), Kind: kind, Readable: true, Default: true}}, nil
}

func (inst pngReader) Decode(ctx context.Context, r io.ReaderAt, size int64, req Request) (stack imaging.Stack, err error) {
	entries, err := inst.Entries(ctx, r, size)
	if err != nil {
		return
	}
	data, err := readWholeEntry(ctx, r, size, entries, req)
	if err != nil {
		return
	}
	img, err := decodePNG(data, req.SourceName)
	if err != nil {
		return stack, refusalOfStd(err, "PNG")
	}
	return stackOfImage(img)
}

// scanPNGAt reads a PNG's chunks before its image data — the chunk heads
// one by one, then the run of them whole — and is scanPNG of them.
func scanPNGAt(r io.ReaderAt, size int64) (info pngInfo, err error) {
	sig := make([]byte, len(pngSignature))
	if size < int64(len(sig)) {
		return info, Malformed("a file shorter than the PNG signature")
	}
	if err = ReadFullAt(r, sig, 0, "the PNG signature"); err != nil {
		return
	}
	if !bytes.Equal(sig, pngSignature) {
		return info, Malformed("a file that does not begin with the PNG signature")
	}
	end := int64(len(sig))
	var head [8]byte
	for range maxChunksBeforeData {
		if end+8 > size {
			break
		}
		if err = ReadFullAt(r, head[:], end, "a PNG chunk head"); err != nil {
			return
		}
		kind := string(head[4:])
		next := end + 12 + int64(binary.BigEndian.Uint32(head[:4]))
		if kind == "IDAT" || kind == "IEND" || next > size {
			break
		}
		end = next
	}
	prefix := make([]byte, end)
	if err = ReadFullAt(r, prefix, 0, "the PNG chunks"); err != nil {
		return
	}
	if info, err = scanPNG(prefix); err != nil {
		return info, MalformedWith("the chunks before the image data", err)
	}
	if info.width <= 0 || info.height <= 0 {
		return info, Malformed("a PNG header of " + strconv.Itoa(info.width) + " × " + strconv.Itoa(info.height) + " pixels")
	}
	return info, nil
}

type jpegReader struct{}

var _ ReaderI = jpegReader{}

func (jpegReader) Format() Format {
	return Format{Name: FormatJPEG.String(), Title: "JPEG", Extensions: []string{".jpg", ".jpeg"}, Version: 1,
		Doc: "JPEG/JFIF: 8-bit baseline and progressive greyscale, colour and CMYK images, read by the standard library's decoder, chroma and ink converted to red, green and blue; 12-bit, lossless and arithmetic-coded files are refused."}
}

func (jpegReader) Sniff(head []byte) bool {
	return len(head) >= 3 && head[0] == 0xff && head[1] == 0xd8 && head[2] == 0xff
}

func (inst jpegReader) Entries(ctx context.Context, r io.ReaderAt, size int64) (entries []Entry, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	prefix, err := jpegHeadAt(r, size)
	if err != nil {
		return
	}
	info := scanJPEG(prefix)
	detail := strconv.Itoa(int(info.precision)) + "-bit " + info.frameName() + ", " + strconv.Itoa(int(info.components)) + " component"
	if info.components != 1 {
		detail += "s"
	}
	e := Entry{Id: builtinEntry, Title: "the image", Detail: detail, Kind: imaging.SampleKindU8, Planes: 3}
	if info.components == 1 {
		e.Planes = 1
	}
	cfg, cerr := jpeg.DecodeConfig(bytes.NewReader(prefix))
	var unsupported jpeg.UnsupportedError
	switch {
	case errors.As(cerr, &unsupported):
		// The file's one entry, and so the one a plain open means.
		e.Reason, e.Refusal, e.Default = "a "+detail+" JPEG ("+cerr.Error()+")", RefusalUnsupported, true
		return []Entry{e}, nil
	case cerr != nil:
		return nil, MalformedWith("the JPEG header", cerr)
	}
	e.Width, e.Height, e.Readable, e.Default = cfg.Width, cfg.Height, true, true
	return []Entry{e}, nil
}

func (inst jpegReader) Decode(ctx context.Context, r io.ReaderAt, size int64, req Request) (stack imaging.Stack, err error) {
	entries, err := inst.Entries(ctx, r, size)
	if err != nil {
		return
	}
	data, err := readWholeEntry(ctx, r, size, entries, req)
	if err != nil {
		return
	}
	img, err := decodeJPEG(data, req.SourceName)
	if err != nil {
		return stack, refusalOfStd(err, "JPEG")
	}
	return stackOfImage(img)
}

// jpegHeadAt reads a JPEG's marker segments up to its first scan's marker
// and length: the segment heads one by one, then the run of them whole.
func jpegHeadAt(r io.ReaderAt, size int64) (prefix []byte, err error) {
	var head [4]byte
	if size < 3 {
		return nil, Malformed("a file shorter than the JPEG signature")
	}
	if err = ReadFullAt(r, head[:3], 0, "the JPEG signature"); err != nil {
		return
	}
	if head[0] != 0xff || head[1] != 0xd8 || head[2] != 0xff {
		return nil, Malformed("a file that does not begin with the JPEG signature")
	}
	end := int64(2)
	for range maxChunksBeforeData {
		if end+4 > size {
			break
		}
		if err = ReadFullAt(r, head[:], end, "a JPEG marker"); err != nil {
			return
		}
		if head[0] != 0xff {
			break
		}
		if head[1] == 0xff {
			end++
			continue
		}
		if head[1] == 0xda {
			// The scan's marker and length are part of the head: the
			// standard decoder reads them before it ends a configuration.
			end += 4
			break
		}
		length := int64(head[2])<<8 | int64(head[3])
		next := end + 2 + length
		if head[1] == 0xd9 || length < 2 || next > size {
			break
		}
		end = next
	}
	prefix = make([]byte, end)
	if err = ReadFullAt(r, prefix, 0, "the JPEG markers"); err != nil {
		return nil, err
	}
	return prefix, nil
}

// readWholeEntry is a built-in's way from entries to bytes: the entry the
// request names, refused over its limits, then the file read whole once ctx
// still allows it.
func readWholeEntry(ctx context.Context, r io.ReaderAt, size int64, entries []Entry, req Request) (data []byte, err error) {
	e, err := SelectEntry(entries, req.Entry)
	if err != nil {
		return
	}
	if err = req.Limits.Check(int64(e.Width), int64(e.Height), int64(e.Planes), e.Kind); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	data = make([]byte, size)
	if err = ReadFullAt(r, data, 0, "the file"); err != nil {
		return nil, err
	}
	return data, nil
}

// refusalOfStd is a standard decoder's error as a refusal: what it says it
// does not support is unsupported, anything else malformed. An error that
// is a refusal already passes through.
func refusalOfStd(err error, decoder string) error {
	if RefusalOf(err) != 0 {
		return err
	}
	var pu png.UnsupportedError
	var ju jpeg.UnsupportedError
	switch {
	case errors.As(err, &pu):
		return Unsupported("the PNG feature " + strconv.Quote(string(pu)))
	case errors.As(err, &ju):
		return Unsupported("the JPEG feature " + strconv.Quote(string(ju)))
	}
	return MalformedWith("what the "+decoder+" decoder read", err)
}

// stackOfImage is an image as a stack whose band axis is its colour model.
func stackOfImage(img imaging.Image) (stack imaging.Stack, err error) {
	planes := make([]imaging.Plane, img.PlaneCount())
	for i := range planes {
		planes[i] = img.Plane(i)
	}
	axes := []imaging.Axis{{Name: "band", Role: imaging.AxisRoleBand, Size: len(planes)}}
	return imaging.NewStack(axes, planes, img.Model(), img.Placement(), img.Provenance())
}
