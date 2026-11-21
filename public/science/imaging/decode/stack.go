package decode

import (
	"context"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode/fits"
	"github.com/stergiotis/boxer/public/science/imaging/decode/npy"
	"github.com/stergiotis/boxer/public/science/imaging/decode/tiff"
)

// sniffLen is how much of a file [DecodeStack] reads to tell its format:
// enough for an NPZ archive's first member name and for the first FITS
// card behind a gzip header.
const sniffLen = 4096

// DecodeStack reads a TIFF, FITS, NPY or NPZ file into a stack (ADR-0398
// §SD6–§SD8) through the reader of its format: [tiff.Decode], [fits.Decode]
// or [npy.Decode], which say what each reads and refuses. The format is
// told from the file's first bytes, as [Sniff] tells it; a file of another
// format, PNG and JPEG included, is refused. sourceName is recorded in the
// provenance and appears in errors; it is a name for a person, not a path
// this function opens.
//
// The limits of opts are the readers' own: MaxPixels bounds a plane,
// MaxBytes the samples of the whole stack and MaxPlanes the planes, each 0
// taking the reader's default. A FITS file is read from its default HDU and
// an NPZ archive from its only array. A refusal keeps the reader's error:
// errors.Is matches the reader's ErrUnsupported, ErrLimit or ErrMalformed,
// and its message names the feature, the limit or the defect. ctx is
// honoured as the reader honours it, between chunks of samples.
func DecodeStack(ctx context.Context, r io.ReaderAt, size int64, sourceName string, opts Options) (stack imaging.Stack, err error) {
	if size < 0 {
		return stack, eb.Build().Str("source", sourceName).Int64("size", size).Errorf("negative file size")
	}
	head := make([]byte, min(size, sniffLen))
	if n, rerr := r.ReadAt(head, 0); n < len(head) {
		return stack, eb.Build().Str("source", sourceName).Int("read", n).Errorf("read the file's first bytes: %w", rerr)
	}
	switch format := Sniff(head); format {
	case FormatTIFF:
		return tiff.Decode(ctx, r, size, sourceName, tiff.Options{MaxPixels: opts.MaxPixels, MaxBytes: opts.MaxBytes})
	case FormatFITS:
		return fits.Decode(ctx, r, size, sourceName, fits.Options{MaxPixels: opts.MaxPixels, MaxBytes: opts.MaxBytes, MaxPlanes: opts.MaxPlanes})
	case FormatNPY, FormatNPZ:
		return npy.Decode(ctx, r, size, sourceName, npy.Options{MaxPixels: opts.MaxPixels, MaxBytes: opts.MaxBytes, MaxPlanes: opts.MaxPlanes})
	case FormatPNG, FormatJPEG:
		return stack, eb.Build().Str("source", sourceName).Stringer("format", format).
			Errorf("the file is an image, not a stack; Decode or DecodeTiled reads it")
	}
	return stack, eb.Build().Str("source", sourceName).Errorf("not a TIFF, FITS, NPY or NPZ file")
}
