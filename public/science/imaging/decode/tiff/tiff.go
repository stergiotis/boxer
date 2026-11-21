package tiff

import (
	"context"
	"errors"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// The errors a read ends in, for errors.Is. The message of an error wrapping
// one names what was refused: the feature, the limit, or the defect.
var (
	// ErrUnsupported: the file uses something this reader does not read,
	// such as a compression, a sample format or a photometric
	// interpretation outside ADR-0398 §SD6.
	ErrUnsupported = errors.New("the TIFF reader does not read this")
	// ErrLimit: the file is over a limit of [Options], or over one the
	// reader keeps for itself; refused from the directory, before samples
	// are read.
	ErrLimit = errors.New("over a decode limit")
	// ErrMalformed: the file is not a well-formed TIFF.
	ErrMalformed = errors.New("malformed TIFF")
)

// refusal carries what was refused in its message, under one of the three
// sentinels.
type refusal struct {
	base error
	what string
}

func (inst *refusal) Error() string { return inst.base.Error() + ": " + inst.what }
func (inst *refusal) Unwrap() error { return inst.base }

func unsupported(what string) error {
	return eb.Build().Str("feature", what).Errorf("%w", &refusal{base: ErrUnsupported, what: what})
}

func malformed(what string) error {
	return eb.Build().Str("defect", what).Errorf("%w", &refusal{base: ErrMalformed, what: what})
}

func malformedErr(what string, cause error) error {
	return eb.Build().Str("defect", what).Errorf("%w: %w", &refusal{base: ErrMalformed, what: what}, cause)
}

func limitErr(what string, limit uint64) error {
	return eb.Build().Str("limit", what).Uint64("value", limit).Errorf("%w", &refusal{base: ErrLimit, what: what})
}

// DefaultMaxPixels bounds a plane at 64 megapixels.
const DefaultMaxPixels int64 = 64 << 20

// DefaultMaxBytes bounds the samples of a whole stack at 1 GiB.
const DefaultMaxBytes int64 = 1 << 30

// Options configures a decode. The zero value is usable.
type Options struct {
	// MaxPixels refuses a plane with more pixels than this, and a tile of
	// more; 0 means [DefaultMaxPixels].
	MaxPixels int64
	// MaxBytes refuses a stack whose samples, every page and band, take
	// more bytes than this; 0 means [DefaultMaxBytes].
	MaxBytes int64
}

func (inst Options) limits() (maxPixels, maxBytes int64) {
	maxPixels, maxBytes = inst.MaxPixels, inst.MaxBytes
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return
}

// Sniff reports whether head begins with a TIFF or BigTIFF signature, in
// either byte order.
func Sniff(head []byte) bool {
	if len(head) < 4 {
		return false
	}
	switch {
	case head[0] == 'I' && head[1] == 'I' && head[3] == 0:
		return head[2] == 42 || head[2] == 43
	case head[0] == 'M' && head[1] == 'M' && head[2] == 0:
		return head[3] == 42 || head[3] == 43
	}
	return false
}

// DirectoryStatusE says what [Decode] does with a directory.
type DirectoryStatusE uint8

const (
	// DirectoryStatusRead: the directory is the image, or a page of it.
	DirectoryStatusRead DirectoryStatusE = iota + 1
	// DirectoryStatusSkipped: the directory is counted and not read — a
	// reduced-resolution image, a mask, or one unlike the first.
	DirectoryStatusSkipped
	// DirectoryStatusRefused: the directory would be read but uses
	// something the reader refuses; [Decode] refuses the file for it.
	DirectoryStatusRefused
)

// AllDirectoryStatuses lists the statuses a directory can have.
var AllDirectoryStatuses = []DirectoryStatusE{DirectoryStatusRead, DirectoryStatusSkipped, DirectoryStatusRefused}

func (inst DirectoryStatusE) String() string {
	switch inst {
	case DirectoryStatusRead:
		return "read"
	case DirectoryStatusSkipped:
		return "skipped"
	case DirectoryStatusRefused:
		return "refused"
	}
	return "none"
}

// Directory describes one image file directory as the file states it.
type Directory struct {
	// Offset is where the directory begins in the file.
	Offset int64
	Width  int64
	Height int64
	// SamplesPerPixel, and per sample its bits and format (1 unsigned, 2
	// signed, 3 IEEE float), as stored.
	SamplesPerPixel int
	BitsPerSample   []uint16
	SampleFormat    []uint16
	// Kind is the sample kind the directory reads to; zero when there is
	// none.
	Kind imaging.SampleKindE
	// Photometric is the PhotometricInterpretation code, and
	// PhotometricName its name in TIFF 6.0; HasPhotometric is false when
	// the tag is absent.
	Photometric     uint16
	PhotometricName string
	HasPhotometric  bool
	// Compression is the compression code and CompressionName its name.
	Compression     uint16
	CompressionName string
	Predictor       uint16
	// Tiled is true for tiles, false for strips; TileWidth and TileHeight,
	// or RowsPerStrip, give the chunk.
	Tiled        bool
	TileWidth    int64
	TileHeight   int64
	RowsPerStrip int64
	// Planar is PlanarConfiguration 2: one plane after another.
	Planar bool
	// NewSubfileType is the tag's bits: 1 reduced resolution, 2 a page, 4
	// a transparency mask.
	NewSubfileType uint32
	ExtraSamples   []uint16
	// SubIFDs is the number of directories the SubIFDs tag points to; they
	// are not read.
	SubIFDs     int
	Orientation uint16
	Status      DirectoryStatusE
	// Reason says why a directory is skipped or refused.
	Reason string
}

// Info is what a file holds, as its directories say, without its samples.
type Info struct {
	// Format is "tiff" or "bigtiff".
	Format    string
	BigEndian bool
	// Directories are in the order of the file's chain.
	Directories []Directory
	// Pages is the number of directories [Decode] reads: the positions of
	// the "page" axis.
	Pages int
}

// Inspect reads the header and every directory of the chain and says what
// each holds and what [Decode] does with it. It reads no samples, nor the
// strip and tile tables — a defect there, or a sparse file, is found by
// [Decode] — and applies no limits of [Options]. ctx is checked before the
// directories are read.
func Inspect(ctx context.Context, r io.ReaderAt, size int64) (info Info, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	c, err := scan(r, size)
	if err != nil {
		return
	}
	return c.info(), nil
}

// Decode reads a TIFF or BigTIFF file into a stack (ADR-0398 §SD6): the
// first directory and the directories like it as pages, every sample in
// its stored kind, with what GeoTIFF and GDAL's tags say. sourceName is
// recorded in the provenance and appears in errors; it is a name for a
// person, not a path this function opens. ctx is honoured between strips
// and tiles.
func Decode(ctx context.Context, r io.ReaderAt, size int64, sourceName string, opts Options) (stack imaging.Stack, err error) {
	c, err := scan(r, size)
	if err == nil {
		stack, err = c.decode(ctx, sourceName, opts)
	}
	if err != nil {
		err = eb.Build().Str("source", sourceName).Errorf("read TIFF: %w", err)
	}
	return
}
