package fits

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The three ways a file is refused. Every error this package returns for a
// file matches one of them under [errors.Is], and its message names what
// was refused.
var (
	// ErrUnsupported: the file uses something this reader does not read.
	ErrUnsupported = errors.New("the file uses something this reader does not read")
	// ErrLimit: the file is over a pixel, byte or count limit.
	ErrLimit = errors.New("over a limit")
	// ErrMalformed: the file does not follow the format.
	ErrMalformed = errors.New("malformed")
)

// refusal is an error that names what was refused and which of the three
// kinds the refusal is.
type refusal struct {
	what string
	kind error
}

func (inst *refusal) Error() string { return inst.what + ": " + inst.kind.Error() }
func (inst *refusal) Unwrap() error { return inst.kind }

func refuse(kind error, what string) error { return &refusal{what: what, kind: kind} }

// DefaultMaxPixels bounds a plane at 64 megapixels.
const DefaultMaxPixels int64 = 64 << 20

// DefaultMaxBytes bounds the samples of a whole stack at 1 GiB.
const DefaultMaxBytes int64 = 1 << 30

// DefaultMaxPlanes bounds the number of planes of a stack, whose per-plane
// bookkeeping is not counted in MaxBytes.
const DefaultMaxPlanes = 1 << 16

// MaxHDUs is the most HDUs a file may hold.
const MaxHDUs = 10_000

// gzipHeaderAllowance is what a gzip-wrapped file may decompress to beyond
// [Options.MaxBytes]: its headers, and HDUs other than the one read.
const gzipHeaderAllowance int64 = 16 << 20

// Options configures a decode. The zero value is usable and reads the
// default HDU.
type Options struct {
	// MaxPixels refuses a plane with more pixels than this; 0 means
	// [DefaultMaxPixels].
	MaxPixels int64
	// MaxBytes refuses a stack whose samples take more bytes than this; 0
	// means [DefaultMaxBytes]. A gzip-wrapped file may decompress to this
	// plus 16 MiB.
	MaxBytes int64
	// MaxPlanes refuses a stack of more planes than this; 0 means
	// [DefaultMaxPlanes].
	MaxPlanes int
	// UseHDU makes Decode read HDU, the zero-based index of an HDU in the
	// file (the primary HDU is 0). Without it Decode reads the first HDU
	// that holds an image of at least two axes with data and that this
	// reader reads ([Info.Default]).
	UseHDU bool
	HDU    int
}

func (inst Options) limits() (maxPixels, maxBytes int64, maxPlanes int) {
	maxPixels, maxBytes, maxPlanes = inst.MaxPixels, inst.MaxBytes, inst.MaxPlanes
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxPlanes <= 0 {
		maxPlanes = DefaultMaxPlanes
	}
	return
}

// HDUTypeE is what kind of header-data unit an HDU is.
type HDUTypeE uint8

const (
	HDUTypePrimary HDUTypeE = iota + 1
	HDUTypeImage
	HDUTypeTable
	HDUTypeBinTable
	// HDUTypeOther is any other extension type; [HDU.XTension] names it.
	HDUTypeOther
)

func (inst HDUTypeE) String() string {
	switch inst {
	case HDUTypePrimary:
		return "primary"
	case HDUTypeImage:
		return "IMAGE"
	case HDUTypeTable:
		return "TABLE"
	case HDUTypeBinTable:
		return "BINTABLE"
	case HDUTypeOther:
		return "other"
	}
	return "unknown"
}

// HDU describes one header-data unit of a file.
type HDU struct {
	Index int
	Type  HDUTypeE
	// XTension is the extension's XTENSION value as written; empty for the
	// primary HDU.
	XTension string
	ExtName  string
	// ExtVer is the EXTVER value, 1 when the header gives none.
	ExtVer int64
	BitPix int
	// Axes are NAXIS1, NAXIS2, … in the file's numbering.
	Axes []int64
	// Readable reports whether [Decode] reads this HDU, limits aside;
	// Reason says why not when it does not.
	Readable bool
	Reason   string
	// HeaderOffset and DataOffset are byte offsets in the (decompressed)
	// file; DataBytes is the length of the data without its padding.
	HeaderOffset, DataOffset, DataBytes int64
	Cards                               []Card
}

// Info is what a file holds, read from its headers alone.
type Info struct {
	// Gzipped reports that the file was gzip-compressed.
	Gzipped bool
	HDUs    []HDU
	// Default is the index of the HDU [Decode] reads without
	// [Options.UseHDU], or -1 when there is none.
	Default int
}

var simpleMagic = []byte("SIMPLE  =")

// Sniff reports whether head, the first bytes of a file, begins a FITS file:
// the SIMPLE keyword, or a gzip stream that decompresses to one.
func Sniff(head []byte) bool {
	if bytes.HasPrefix(head, simpleMagic) {
		return true
	}
	if !isGzip(head) {
		return false
	}
	zr, err := gzip.NewReader(bytes.NewReader(head))
	if err != nil {
		return false
	}
	var b [9]byte
	n, _ := io.ReadFull(zr, b[:])
	return bytes.Equal(b[:n], simpleMagic)
}

func isGzip(head []byte) bool { return len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b }

// Inspect reads every header of a file and reports its HDUs, reading no
// samples. A gzip-wrapped file is decompressed in memory under the default
// limits. ctx is honoured between header blocks and while a gzip-wrapped
// file is decompressed. On an error the Info holds the HDUs read before it.
func Inspect(ctx context.Context, r io.ReaderAt, size int64) (info Info, err error) {
	info.Default = -1
	r, size, info.Gzipped, err = unwrap(ctx, r, size, DefaultMaxBytes+gzipHeaderAllowance)
	if err != nil {
		return
	}
	scans, err := scan(ctx, r, size)
	info.HDUs = make([]HDU, len(scans))
	for i, s := range scans {
		info.HDUs[i] = s.hdu
	}
	if err != nil {
		return
	}
	info.Default = defaultHDU(scans)
	return
}

// unwrap decompresses a gzip-wrapped file into memory, refusing one that
// decompresses to more than limit bytes.
func unwrap(ctx context.Context, r io.ReaderAt, size int64, limit int64) (out io.ReaderAt, outSize int64, gzipped bool, err error) {
	if size < 0 {
		return nil, 0, false, refuse(ErrMalformed, "negative file size")
	}
	var head [2]byte
	if size >= 2 {
		if _, err = r.ReadAt(head[:], 0); err != nil {
			return nil, 0, false, eb.Build().Int64("size", size).Errorf("read the file's first bytes: %w", errors.Join(ErrMalformed, err))
		}
	}
	if !isGzip(head[:min(size, 2)]) {
		return r, size, false, nil
	}
	zr, err := gzip.NewReader(io.NewSectionReader(r, 0, size))
	if err != nil {
		return nil, 0, true, eb.Build().Errorf("open the gzip stream: %w", errors.Join(ErrMalformed, err))
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(ctxReader{ctx: ctx, r: zr}, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, true, ctx.Err()
		}
		return nil, 0, true, eb.Build().Errorf("decompress: %w", errors.Join(ErrMalformed, err))
	}
	if n > limit {
		return nil, 0, true, eb.Build().Int64("limit", limit).
			Errorf("decompressed file: %w", refuse(ErrLimit, "the gzip-compressed file decompresses to more than the byte limit allows"))
	}
	data := buf.Bytes()
	return bytes.NewReader(data), int64(len(data)), true, nil
}

// ctxReader stops a read once its context is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (inst ctxReader) Read(p []byte) (int, error) {
	if err := inst.ctx.Err(); err != nil {
		return 0, err
	}
	return inst.r.Read(p)
}

// hduScan is one HDU as read from its header: what Inspect reports, and
// what Decode needs beyond it.
type hduScan struct {
	hdu  HDU
	hdr  *header
	fail error // why Decode refuses it, nil when it reads it
	// image reports an HDU that holds an image with at least two axes and
	// data, whether or not this reader reads it; the default search refuses
	// the first such when no HDU is readable.
	image bool
}

// scan reads the headers of every HDU in the file.
func scan(ctx context.Context, r io.ReaderAt, size int64) (scans []hduScan, err error) {
	if size < blockLen {
		return nil, refuse(ErrMalformed, "shorter than one 2880-byte block: not a FITS file")
	}
	var block [blockLen]byte
	off := int64(0)
	for off+blockLen <= size {
		if err = ctx.Err(); err != nil {
			return
		}
		if len(scans) >= MaxHDUs {
			return scans, eb.Build().Int("maxHDUs", MaxHDUs).Errorf("count HDUs: %w", refuse(ErrLimit, "more HDUs than the reader lists"))
		}
		if _, err = r.ReadAt(block[:], off); err != nil {
			return scans, eb.Build().Int64("offset", off).Errorf("read a header block: %w", errors.Join(ErrMalformed, err))
		}
		if len(scans) == 0 {
			if !bytes.HasPrefix(block[:], simpleMagic) {
				return nil, refuse(ErrMalformed, "the file does not begin with SIMPLE: not a FITS file")
			}
		} else if !bytes.HasPrefix(block[:], []byte("XTENSION")) {
			// Special records may follow the last HDU (§3.5); they are not
			// read.
			break
		}
		var s hduScan
		var next int64
		s, next, err = scanHDU(ctx, r, size, off, block[:], len(scans))
		if err != nil {
			return scans, err
		}
		scans = append(scans, s)
		if next <= off {
			return scans, refuse(ErrMalformed, "an HDU of no length")
		}
		off = next
	}
	return
}

// scanHDU reads the header starting at off, whose first block is first.
func scanHDU(ctx context.Context, r io.ReaderAt, size, off int64, first []byte, index int) (s hduScan, next int64, err error) {
	h := newHeader()
	block := first
	pos := off
	ended := false
	buf := make([]byte, blockLen)
	for !ended {
		for i := 0; i < cardsPerBlk; i++ {
			raw := block[i*cardLen : (i+1)*cardLen]
			if string(raw[:8]) == "END     " {
				ended = true
				break
			}
			h.add(parseCard(raw))
		}
		pos += blockLen
		if ended {
			break
		}
		if err = ctx.Err(); err != nil {
			return
		}
		if pos+blockLen > size {
			return s, 0, eb.Build().Int("hdu", index).Int64("offset", off).Errorf("read header: %w", refuse(ErrMalformed, "the header has no END card before the end of the file"))
		}
		if _, err = r.ReadAt(buf, pos); err != nil {
			return s, 0, eb.Build().Int64("offset", pos).Errorf("read a header block: %w", errors.Join(ErrMalformed, err))
		}
		block = buf
	}
	s.hdr = h
	s.hdu = HDU{Index: index, HeaderOffset: off, DataOffset: pos, Cards: h.cards, ExtVer: 1}
	if err = s.describe(index); err != nil {
		return s, 0, eb.Build().Int("hdu", index).Errorf("read header: %w", err)
	}
	if s.hdu.DataBytes > size-pos {
		return s, 0, eb.Build().Int("hdu", index).Int64("dataBytes", s.hdu.DataBytes).Int64("available", size-pos).
			Errorf("read header: %w", refuse(ErrMalformed, "the data extends past the end of the file"))
	}
	padded := (s.hdu.DataBytes + blockLen - 1) / blockLen * blockLen
	return s, pos + padded, nil
}

// describe fills in the HDU's type, sample layout, size and readability
// from its header.
func (inst *hduScan) describe(index int) error {
	h, d := inst.hdr, &inst.hdu
	if index == 0 {
		simple, _, valid := h.logical("SIMPLE")
		if !valid {
			return refuse(ErrMalformed, "SIMPLE is not a logical value")
		}
		d.Type = HDUTypePrimary
		if !simple {
			inst.fail = refuse(ErrUnsupported, "SIMPLE = F: the file declares that it does not conform to the standard")
		}
	} else {
		x, ok := h.str("XTENSION")
		if !ok {
			return refuse(ErrMalformed, "XTENSION is not a string")
		}
		d.XTension = x
		switch x {
		case "IMAGE":
			d.Type = HDUTypeImage
		case "TABLE":
			d.Type = HDUTypeTable
		case "BINTABLE":
			d.Type = HDUTypeBinTable
		default:
			d.Type = HDUTypeOther
		}
	}
	d.ExtName, _ = h.str("EXTNAME")
	if v, present, valid := h.integer("EXTVER"); present && valid {
		d.ExtVer = v
	}
	bitpix, present, valid := h.integer("BITPIX")
	if !present || !valid {
		return refuse(ErrMalformed, "BITPIX is missing or not an integer")
	}
	switch bitpix {
	case 8, 16, 32, 64, -32, -64:
	default:
		return eb.Build().Int64("bitpix", bitpix).Errorf("read BITPIX: %w", refuse(ErrMalformed, "BITPIX is not 8, 16, 32, 64, -32 or -64"))
	}
	d.BitPix = int(bitpix)
	naxis, present, valid := h.integer("NAXIS")
	if !present || !valid || naxis < 0 || naxis > 999 {
		return eb.Build().Int64("naxis", naxis).Errorf("read NAXIS: %w", refuse(ErrMalformed, "NAXIS is missing or not an integer in 0 … 999"))
	}
	d.Axes = make([]int64, naxis)
	for i := range d.Axes {
		kw := "NAXIS" + strconv.Itoa(i+1)
		v, present, valid := h.integer(kw)
		if !present || !valid || v < 0 {
			return eb.Build().Str("keyword", kw).Errorf("read an axis length: %w", refuse(ErrMalformed, "an NAXISn keyword is missing or not a non-negative integer"))
		}
		d.Axes[i] = v
	}
	pcount, gcount := int64(0), int64(1)
	if d.Type != HDUTypePrimary {
		if v, present, valid := h.integer("PCOUNT"); present {
			if !valid || v < 0 {
				return refuse(ErrMalformed, "PCOUNT is not a non-negative integer")
			}
			pcount = v
		}
		if v, present, valid := h.integer("GCOUNT"); present {
			if !valid || v < 0 {
				return refuse(ErrMalformed, "GCOUNT is not a non-negative integer")
			}
			gcount = v
		}
	}
	groups, _, _ := h.logical("GROUPS")
	randomGroups := d.Type == HDUTypePrimary && groups && naxis >= 1 && d.Axes[0] == 0
	if randomGroups {
		if v, _, valid := h.integer("PCOUNT"); valid && v >= 0 {
			pcount = v
		}
		if v, _, valid := h.integer("GCOUNT"); valid && v >= 0 {
			gcount = v
		}
	}
	// The data size (§4.4.1.1, §6, §7): |BITPIX|/8 · GCOUNT · (PCOUNT +
	// NAXIS1 · … · NAXISn), with NAXIS1 left out of the product for random
	// groups, and zero when NAXIS is 0.
	var n uint64
	if naxis > 0 {
		n = 1
		for i, a := range d.Axes {
			if randomGroups && i == 0 {
				continue
			}
			hi, lo := bits.Mul64(n, uint64(a))
			if hi != 0 || lo > math.MaxInt64 {
				return refuse(ErrMalformed, "the axis lengths overflow a 64-bit size")
			}
			n = lo
		}
	}
	if naxis > 0 || pcount > 0 {
		sum, carry := bits.Add64(n, uint64(pcount), 0)
		hi, lo := bits.Mul64(sum, uint64(gcount))
		hi2, total := bits.Mul64(lo, uint64(abs(d.BitPix)/8))
		if carry != 0 || hi != 0 || hi2 != 0 || total > math.MaxInt64 {
			return refuse(ErrMalformed, "the data size overflows a 64-bit size")
		}
		d.DataBytes = int64(total)
	}
	hasData := naxis >= 2 && n > 0
	switch {
	case inst.fail != nil:
		inst.image = naxis >= 2 && hasData
	case randomGroups:
		inst.fail = refuse(ErrUnsupported, "random groups (GROUPS = T)")
		inst.image = true
	case d.Type == HDUTypeBinTable:
		if z, _, _ := h.logical("ZIMAGE"); z {
			inst.fail = refuse(ErrUnsupported, "a tile-compressed image (ZIMAGE = T in a BINTABLE)")
			inst.image = true
		} else {
			inst.fail = refuse(ErrUnsupported, "a BINTABLE extension: tables are not read")
		}
	case d.Type == HDUTypeTable:
		inst.fail = refuse(ErrUnsupported, "a TABLE extension: tables are not read")
	case d.Type == HDUTypeOther:
		inst.fail = refuse(ErrUnsupported, "an extension of type "+strconv.Quote(d.XTension))
	case naxis < 2:
		inst.fail = refuse(ErrUnsupported, "NAXIS < 2: the HDU holds no image")
	case !hasData:
		inst.fail = refuse(ErrUnsupported, "an axis of length 0: the HDU holds no data")
	case d.BitPix == 64:
		inst.fail = refuse(ErrUnsupported, "BITPIX 64: 64-bit integer samples")
		inst.image = true
	default:
		inst.image = true
	}
	if inst.fail != nil {
		d.Reason = inst.fail.Error()
	} else {
		d.Readable = true
	}
	return nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// defaultHDU is the first readable HDU, or -1.
func defaultHDU(scans []hduScan) int {
	for i, s := range scans {
		if s.hdu.Readable {
			return i
		}
	}
	return -1
}

// pick chooses the HDU to read.
func pick(scans []hduScan, opts Options) (int, error) {
	if opts.UseHDU {
		if opts.HDU < 0 || opts.HDU >= len(scans) {
			return 0, eb.Build().Int("hdu", opts.HDU).Int("hdus", len(scans)).Errorf("select an HDU: %w", refuse(ErrUnsupported, "the file has no HDU of that index"))
		}
		if f := scans[opts.HDU].fail; f != nil {
			return 0, eb.Build().Int("hdu", opts.HDU).Errorf("read the selected HDU: %w", f)
		}
		return opts.HDU, nil
	}
	if i := defaultHDU(scans); i >= 0 {
		return i, nil
	}
	for i, s := range scans {
		if s.image {
			return 0, eb.Build().Int("hdu", i).Errorf("read the file's only image: %w", s.fail)
		}
	}
	return 0, refuse(ErrUnsupported, "no HDU holds an image of at least two axes with data")
}

func tableNote(scans []hduScan, chosen int) string {
	tables := make([]string, 0, 4)
	others := 0
	for i, s := range scans {
		if i == chosen {
			continue
		}
		switch s.hdu.Type {
		case HDUTypeTable, HDUTypeBinTable:
			if s.image {
				tables = append(tables, "a tile-compressed image")
			} else {
				tables = append(tables, s.hdu.Type.String())
			}
		default:
			if s.hdu.Readable {
				others++
			}
		}
	}
	var parts []string
	if len(tables) > 0 {
		parts = append(parts, "the file holds "+strconv.Itoa(len(tables))+" table extension(s) ("+strings.Join(tables, ", ")+"), not read")
	}
	if others > 0 {
		parts = append(parts, "the file holds "+strconv.Itoa(others)+" other image HDU(s), not read")
	}
	return strings.Join(parts, "; ")
}
