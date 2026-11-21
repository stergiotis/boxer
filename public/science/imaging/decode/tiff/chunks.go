package tiff

import (
	"bufio"
	"compress/zlib"
	"context"
	"encoding/binary"
	"io"
	"math"

	"golang.org/x/image/tiff/lzw"
)

// sampleI is a Go type a plane holds.
type sampleI interface {
	uint8 | uint16 | int16 | int32 | float32 | float64
}

// chunkReader decompresses strips and tiles into one reused buffer.
type chunkReader struct {
	f    *file
	buf  []byte
	tmp  []byte
	bufr *bufio.Reader
	zr   io.ReadCloser
}

// geometry is where a chunk's samples go.
type geometry struct {
	plane      int // the plane of a planar chunk; 0 for a chunky one
	x0, y0     int // the chunk's first pixel in the image
	cw, ch     int // the chunk's width and height as stored
	vw, vh     int // the part of it inside the image
	perPixel   int // samples per pixel in the chunk: 1 planar, spp chunky
	rowBytes   int
	chunkBytes int
}

func (inst *dir) geometryOf(i int) (g geometry) {
	perPlane := int(inst.across * inst.down)
	g.perPixel = inst.SamplesPerPixel
	if inst.Planar {
		g.plane, i = i/perPlane, i%perPlane
		g.perPixel = 1
	}
	w, h := int(inst.Width), int(inst.Height)
	if inst.Tiled {
		tx, ty := i%int(inst.across), i/int(inst.across)
		g.cw, g.ch = int(inst.TileWidth), int(inst.TileHeight)
		g.x0, g.y0 = tx*g.cw, ty*g.ch
	} else {
		rps := int(inst.RowsPerStrip)
		g.cw, g.x0, g.y0 = w, 0, i*rps
		// The last strip stores only the rows the image has left.
		g.ch = min(rps, h-g.y0)
	}
	g.vw, g.vh = min(g.cw, w-g.x0), min(g.ch, h-g.y0)
	g.rowBytes = g.cw * g.perPixel * inst.bytesPerSample
	g.chunkBytes = g.rowBytes * g.ch
	return
}

// maxChunkBytes is the largest decompressed chunk of the directory.
func (inst *dir) maxChunkBytes() uint64 {
	perPixel := uint64(inst.SamplesPerPixel)
	if inst.Planar {
		perPixel = 1
	}
	var w, h uint64
	if inst.Tiled {
		w, h = uint64(inst.TileWidth), uint64(inst.TileHeight)
	} else {
		w, h = uint64(inst.Width), uint64(inst.RowsPerStrip)
	}
	return mulSat(mulSat(mulSat(w, h), perPixel), uint64(inst.bytesPerSample))
}

// mulSat multiplies, saturating at the largest uint64.
func mulSat(a, b uint64) uint64 {
	if a != 0 && b > math.MaxUint64/a {
		return math.MaxUint64
	}
	return a * b
}

// decodeInto reads every chunk of a directory into dst, one slice of
// width·height samples per plane the directory stores, converting each
// stored sample with get.
func decodeInto[T sampleI](ctx context.Context, cr *chunkReader, d *dir, offsets, counts []uint64, dst [][]T, get func(binary.ByteOrder, []byte) T) error {
	w := int(d.Width)
	bps := d.bytesPerSample
	for i := range offsets {
		if err := ctx.Err(); err != nil {
			return err
		}
		g := d.geometryOf(i)
		if g.vw <= 0 || g.vh <= 0 {
			continue
		}
		raw, err := cr.inflate(d, offsets[i], counts[i], g.chunkBytes)
		if err != nil {
			return err
		}
		order := cr.f.order
		switch d.Predictor {
		case 2:
			undoHorizontal(raw, g, bps, order)
		case 3:
			cr.undoFloat(raw, g, bps)
			// The floating-point predictor leaves each sample's bytes most
			// significant first, whatever the file's byte order.
			order = binary.BigEndian
		}
		for r := range g.vh {
			row := raw[r*g.rowBytes:]
			base := (g.y0+r)*w + g.x0
			for c := range g.vw {
				px := row[c*g.perPixel*bps:]
				for s := range g.perPixel {
					dst[g.plane+s][base+c] = get(order, px[s*bps:])
				}
			}
		}
	}
	return nil
}

// inflate decompresses one chunk into the reused buffer and returns its n
// bytes. A chunk that yields fewer is malformed; what a chunk holds beyond
// n is not read.
func (inst *chunkReader) inflate(d *dir, off, count uint64, n int) (out []byte, err error) {
	if cap(inst.buf) < n {
		inst.buf = make([]byte, n)
	}
	out = inst.buf[:n]
	sec := io.NewSectionReader(inst.f.r, int64(off), int64(count))
	switch d.Compression {
	case compressionNone:
		if count < uint64(n) {
			return nil, malformed("an uncompressed strip or tile is shorter than its samples")
		}
		_, err = io.ReadFull(sec, out)
	case compressionPackBits:
		err = unpackBits(inst.reader(sec), out)
	case compressionLZW:
		rd := lzw.NewReader(inst.reader(sec), lzw.MSB, 8)
		_, err = io.ReadFull(rd, out)
		_ = rd.Close()
	case compressionDeflate, compressionDeflateOld:
		if inst.zr == nil {
			inst.zr, err = zlib.NewReader(inst.reader(sec))
		} else {
			err = inst.zr.(zlib.Resetter).Reset(inst.reader(sec), nil)
		}
		if err == nil {
			_, err = io.ReadFull(inst.zr, out)
		}
	default:
		return nil, unsupported(named("compression", uint64(d.Compression), d.CompressionName))
	}
	if err != nil {
		return nil, malformedErr("a strip or tile does not decompress to its samples", err)
	}
	return out, nil
}

func (inst *chunkReader) reader(r io.Reader) *bufio.Reader {
	if inst.bufr == nil {
		inst.bufr = bufio.NewReaderSize(r, 32<<10)
	} else {
		inst.bufr.Reset(r)
	}
	return inst.bufr
}

// unpackBits decodes PackBits (TIFF 6.0 §9) until out is full: a header
// byte n of 0…127 copies the next n+1 bytes, −1…−127 repeats the next byte
// 1−n times, −128 is skipped. A run that would pass the end of out is cut.
func unpackBits(r *bufio.Reader, out []byte) error {
	i := 0
	for i < len(out) {
		h, err := r.ReadByte()
		if err != nil {
			return io.ErrUnexpectedEOF
		}
		n := int(int8(h))
		switch {
		case n >= 0:
			k := min(n+1, len(out)-i)
			if _, err = io.ReadFull(r, out[i:i+k]); err != nil {
				return io.ErrUnexpectedEOF
			}
			i += k
			if k < n+1 {
				return nil
			}
		case n == -128:
		default:
			b, err := r.ReadByte()
			if err != nil {
				return io.ErrUnexpectedEOF
			}
			k := min(1-n, len(out)-i)
			for j := range k {
				out[i+j] = b
			}
			i += k
		}
	}
	return nil
}

// undoHorizontal reverses predictor 2 (TIFF 6.0 §14): each sample of a
// row was stored as its difference from the same sample of the pixel to
// its left, in the sample's own width, wrapping.
func undoHorizontal(raw []byte, g geometry, bps int, order binary.ByteOrder) {
	n := g.cw * g.perPixel
	sp := g.perPixel
	for r := range g.ch {
		row := raw[r*g.rowBytes : (r+1)*g.rowBytes]
		switch bps {
		case 1:
			for i := sp; i < n; i++ {
				row[i] += row[i-sp]
			}
		case 2:
			for i := sp; i < n; i++ {
				order.PutUint16(row[2*i:], order.Uint16(row[2*i:])+order.Uint16(row[2*(i-sp):]))
			}
		case 4:
			for i := sp; i < n; i++ {
				order.PutUint32(row[4*i:], order.Uint32(row[4*i:])+order.Uint32(row[4*(i-sp):]))
			}
		case 8:
			for i := sp; i < n; i++ {
				order.PutUint64(row[8*i:], order.Uint64(row[8*i:])+order.Uint64(row[8*(i-sp):]))
			}
		}
	}
}

// undoFloat reverses predictor 3 (Adobe TIFF Technical Note 3): a row of
// n samples of b bytes was stored as b runs of n bytes — every sample's
// most significant byte, then every next byte — and each byte as its
// difference from the byte one pixel to its left. The row is restored with
// each sample's bytes most significant first.
func (inst *chunkReader) undoFloat(raw []byte, g geometry, bps int) {
	n := g.cw * g.perPixel
	if cap(inst.tmp) < g.rowBytes {
		inst.tmp = make([]byte, g.rowBytes)
	}
	tmp := inst.tmp[:g.rowBytes]
	sp := g.perPixel
	for r := range g.ch {
		row := raw[r*g.rowBytes : (r+1)*g.rowBytes]
		for i := sp; i < len(row); i++ {
			row[i] += row[i-sp]
		}
		for k := range bps {
			run := row[k*n : (k+1)*n]
			for j, v := range run {
				tmp[j*bps+k] = v
			}
		}
		copy(row, tmp)
	}
}

func getU8(_ binary.ByteOrder, b []byte) uint8 { return b[0] }
func getU16(o binary.ByteOrder, b []byte) uint16 {
	return o.Uint16(b)
}
func getIndex8(_ binary.ByteOrder, b []byte) uint16 { return uint16(b[0]) }
func getI16(o binary.ByteOrder, b []byte) int16     { return int16(o.Uint16(b)) }
func getI32(o binary.ByteOrder, b []byte) int32     { return int32(o.Uint32(b)) }
func getF32(o binary.ByteOrder, b []byte) float32 {
	return math.Float32frombits(o.Uint32(b))
}
func getF64(o binary.ByteOrder, b []byte) float64 {
	return math.Float64frombits(o.Uint64(b))
}
