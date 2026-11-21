package tiff

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// A TIFF written sample by sample, so that a test knows exactly what the
// file stores: either flavour and byte order, strips or tiles, chunky or
// planar, each compression and predictor the reader reads, and any tag a
// case needs besides.

// wtag is one directory entry to write. Integer types take ints, the
// floating and rational types floats, ASCII text.
type wtag struct {
	tag, typ uint16
	ints     []uint64
	floats   []float64
	text     string
	// count overrides the count written, for a malformed entry.
	count uint64
}

func shortTag(tag uint16, v ...uint64) wtag   { return wtag{tag: tag, typ: typeShort, ints: v} }
func longTag(tag uint16, v ...uint64) wtag    { return wtag{tag: tag, typ: typeLong, ints: v} }
func doubleTag(tag uint16, v ...float64) wtag { return wtag{tag: tag, typ: typeDouble, floats: v} }
func asciiTag(tag uint16, s string) wtag      { return wtag{tag: tag, typ: typeASCII, text: s} }
func rationalTag(tag uint16, v ...float64) wtag {
	return wtag{tag: tag, typ: typeRational, floats: v}
}

// wimage is one directory and its samples.
type wimage struct {
	w, h        int
	kind        imaging.SampleKindE
	planes      [][]float64 // samples per plane, row-major
	photometric uint16
	extras      []uint16
	planar      bool
	tiled       bool
	tw, th      int
	rps         int // 0: one strip
	compression uint16
	predictor   uint16
	subfile     uint32
	tags        []wtag // added after the defaults, replacing any of the same tag
	omit        []uint16
}

// byteOrderI is a byte order that also appends.
type byteOrderI interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// wfile is a whole file.
type wfile struct {
	big    bool
	order  byteOrderI
	images []wimage
}

func (inst wimage) bytesPerSample() int { return inst.kind.Bytes() }

// putSample writes one sample of the image's kind in the given order.
func putSample(kind imaging.SampleKindE, order binary.ByteOrder, b []byte, v float64) {
	switch kind {
	case imaging.SampleKindU8:
		b[0] = uint8(v)
	case imaging.SampleKindU16:
		order.PutUint16(b, uint16(v))
	case imaging.SampleKindI16:
		order.PutUint16(b, uint16(int16(v)))
	case imaging.SampleKindI32:
		order.PutUint32(b, uint32(int32(v)))
	case imaging.SampleKindF32:
		order.PutUint32(b, math.Float32bits(float32(v)))
	case imaging.SampleKindF64:
		order.PutUint64(b, math.Float64bits(v))
	}
}

// chunks returns the uncompressed, unpredicted bytes of every strip or
// tile, in the order the offsets list them.
func (inst wimage) chunks(order binary.ByteOrder) (out [][]byte, rowBytes []int) {
	spp := len(inst.planes)
	planeCount, perPixel := 1, spp
	if inst.planar {
		planeCount, perPixel = spp, 1
	}
	bps := inst.bytesPerSample()
	type rect struct{ x0, y0, cw, ch int }
	var rects []rect
	if inst.tiled {
		for ty := 0; ty < inst.h; ty += inst.th {
			for tx := 0; tx < inst.w; tx += inst.tw {
				rects = append(rects, rect{tx, ty, inst.tw, inst.th})
			}
		}
	} else {
		rps := inst.rps
		if rps == 0 {
			rps = inst.h
		}
		for y := 0; y < inst.h; y += rps {
			rects = append(rects, rect{0, y, inst.w, min(rps, inst.h-y)})
		}
	}
	for p := range planeCount {
		for _, r := range rects {
			rb := r.cw * perPixel * bps
			buf := make([]byte, rb*r.ch)
			for y := range r.ch {
				for x := range r.cw {
					ix, iy := r.x0+x, r.y0+y
					if ix >= inst.w || iy >= inst.h {
						continue // padding of an edge tile
					}
					for s := range perPixel {
						v := inst.planes[p+s][iy*inst.w+ix]
						putSample(inst.kind, order, buf[y*rb+(x*perPixel+s)*bps:], v)
					}
				}
			}
			out = append(out, buf)
			rowBytes = append(rowBytes, rb)
		}
	}
	return
}

// predict applies a predictor to one chunk in place.
func predict(buf []byte, rowBytes, perPixel, bps int, predictor uint16, order binary.ByteOrder) {
	for r := 0; r < len(buf); r += rowBytes {
		row := buf[r : r+rowBytes]
		n := rowBytes / bps
		switch predictor {
		case 2:
			for i := n - 1; i >= perPixel; i-- {
				switch bps {
				case 1:
					row[i] -= row[i-perPixel]
				case 2:
					order.PutUint16(row[2*i:], order.Uint16(row[2*i:])-order.Uint16(row[2*(i-perPixel):]))
				case 4:
					order.PutUint32(row[4*i:], order.Uint32(row[4*i:])-order.Uint32(row[4*(i-perPixel):]))
				}
			}
		case 3:
			// Each sample big-endian, then its bytes split into runs by
			// significance, then each byte differenced from the byte one
			// pixel to its left (Adobe Technical Note 3).
			shuffled := make([]byte, rowBytes)
			for j := range n {
				var be [8]byte
				switch bps {
				case 4:
					binary.BigEndian.PutUint32(be[:], order.Uint32(row[4*j:]))
				case 8:
					binary.BigEndian.PutUint64(be[:], order.Uint64(row[8*j:]))
				}
				for k := range bps {
					shuffled[k*n+j] = be[k]
				}
			}
			for i := rowBytes - 1; i >= perPixel; i-- {
				shuffled[i] -= shuffled[i-perPixel]
			}
			copy(row, shuffled)
		}
	}
}

// packBits encodes runs of three or more equal bytes as repeats and the
// rest as literals of at most 128 bytes.
func packBits(src []byte) []byte {
	var out []byte
	i := 0
	for i < len(src) {
		j := i + 1
		for j < len(src) && src[j] == src[i] && j-i < 128 {
			j++
		}
		if j-i >= 3 {
			out = append(out, byte(int8(1-(j-i))), src[i])
			i = j
			continue
		}
		start := i
		for i < len(src) && i-start < 128 {
			if i+2 < len(src) && src[i] == src[i+1] && src[i] == src[i+2] {
				break
			}
			i++
		}
		out = append(out, byte(i-start-1))
		out = append(out, src[start:i]...)
		// A no-op header now and then, which a decoder must skip.
		if len(out)%7 == 0 {
			out = append(out, 0x80)
		}
	}
	return out
}

// lzwTIFF encodes TIFF's LZW (TIFF 6.0 §13): codes most significant bit
// first, 256 Clear and 257 EndOfInformation, codes from 258, the width
// growing from 9 bits one code early — the decoder, one entry behind the
// encoder, widens when its table reaches 511, 1023 and 2047 entries — and a
// Clear before the table passes 4093.
func lzwTIFF(src []byte) []byte {
	var out []byte
	var acc uint64
	var nbits uint
	width := uint(9)
	emit := func(code int) {
		acc = acc<<width | uint64(code)
		nbits += width
		for nbits >= 8 {
			out = append(out, byte(acc>>(nbits-8)))
			nbits -= 8
		}
	}
	type key struct {
		prefix int
		b      byte
	}
	table := map[key]int{}
	next := 258
	reset := func() {
		clear(table)
		next = 258
		width = 9
	}
	emit(256)
	if len(src) == 0 {
		emit(257)
	} else {
		w := int(src[0])
		for _, b := range src[1:] {
			if c, ok := table[key{w, b}]; ok {
				w = c
				continue
			}
			emit(w)
			table[key{w, b}] = next
			next++
			switch next {
			case 512:
				width = 10
			case 1024:
				width = 11
			case 2048:
				width = 12
			case 4094:
				emit(256)
				reset()
			}
			w = int(b)
		}
		emit(w)
		// The code just emitted may have filled the table to the next width.
		next++
		switch next {
		case 512:
			width = 10
		case 1024:
			width = 11
		case 2048:
			width = 12
		}
		emit(257)
	}
	if nbits > 0 {
		out = append(out, byte(acc<<(8-nbits)))
	}
	return out
}

func compress(t testing.TB, raw []byte, compression uint16) []byte {
	switch compression {
	case compressionNone:
		return raw
	case compressionPackBits:
		return packBits(raw)
	case compressionLZW:
		return lzwTIFF(raw)
	case compressionDeflate, compressionDeflateOld:
		var b bytes.Buffer
		zw := zlib.NewWriter(&b)
		if _, err := zw.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	// Any other code: the bytes as they are, for a refusal to name.
	return raw
}

// encode returns an entry's value bytes in the file's order.
func (inst wtag) encode(order byteOrderI) (count uint64, b []byte) {
	switch inst.typ {
	case typeASCII:
		b = append([]byte(inst.text), 0)
		count = uint64(len(b))
	case typeDouble:
		for _, v := range inst.floats {
			b = order.AppendUint64(b, math.Float64bits(v))
		}
		count = uint64(len(inst.floats))
	case typeRational:
		for _, v := range inst.floats {
			b = order.AppendUint32(b, uint32(v*1000))
			b = order.AppendUint32(b, 1000)
		}
		count = uint64(len(inst.floats))
	default:
		for _, v := range inst.ints {
			switch typeSize(inst.typ) {
			case 1:
				b = append(b, byte(v))
			case 2:
				b = order.AppendUint16(b, uint16(v))
			case 4:
				b = order.AppendUint32(b, uint32(v))
			case 8:
				b = order.AppendUint64(b, v)
			}
		}
		count = uint64(len(inst.ints))
	}
	if inst.count != 0 {
		count = inst.count
	}
	return
}

// bytes writes the file: header, every image's chunks, then the
// directories with their values.
func (inst wfile) bytes(t testing.TB) []byte {
	var order byteOrderI = binary.LittleEndian
	if inst.order != nil {
		order = inst.order
	}
	var out []byte
	if order.Uint16([]byte{1, 0}) == 1 {
		out = append(out, 'I', 'I')
	} else {
		out = append(out, 'M', 'M')
	}
	firstAt := 0
	if inst.big {
		out = order.AppendUint16(out, 43)
		out = order.AppendUint16(out, 8)
		out = order.AppendUint16(out, 0)
		firstAt = len(out)
		out = order.AppendUint64(out, 0)
	} else {
		out = order.AppendUint16(out, 42)
		firstAt = len(out)
		out = order.AppendUint32(out, 0)
	}
	offType := uint16(typeLong)
	if inst.big {
		offType = typeLong8
	}
	dirTags := make([][]wtag, len(inst.images))
	for i, img := range inst.images {
		raw, rowBytes := img.chunks(order)
		perPixel := len(img.planes)
		if img.planar {
			perPixel = 1
		}
		var offsets, counts []uint64
		for c, chunk := range raw {
			predict(chunk, rowBytes[c], perPixel, img.bytesPerSample(), img.predictor, order)
			data := compress(t, chunk, img.compression)
			for len(out)%2 != 0 {
				out = append(out, 0)
			}
			offsets = append(offsets, uint64(len(out)))
			counts = append(counts, uint64(len(data)))
			out = append(out, data...)
		}
		bits := make([]uint64, len(img.planes))
		formats := make([]uint64, len(img.planes))
		for p := range bits {
			bits[p] = uint64(8 * img.bytesPerSample())
			switch {
			case img.kind.IsFloat():
				formats[p] = 3
			case img.kind.IsSigned():
				formats[p] = 2
			default:
				formats[p] = 1
			}
		}
		tags := []wtag{
			longTag(tagImageWidth, uint64(img.w)),
			longTag(tagImageLength, uint64(img.h)),
			shortTag(tagBitsPerSample, bits...),
			shortTag(tagCompression, uint64(img.compression)),
			shortTag(tagPhotometric, uint64(img.photometric)),
			shortTag(tagSamplesPerPixel, uint64(len(img.planes))),
			shortTag(tagSampleFormat, formats...),
		}
		if img.subfile != 0 {
			tags = append(tags, longTag(tagNewSubfileType, uint64(img.subfile)))
		}
		if img.planar {
			tags = append(tags, shortTag(tagPlanarConfig, 2))
		}
		if img.predictor > 1 {
			tags = append(tags, shortTag(tagPredictor, uint64(img.predictor)))
		}
		if len(img.extras) > 0 {
			ex := make([]uint64, len(img.extras))
			for k, e := range img.extras {
				ex[k] = uint64(e)
			}
			tags = append(tags, shortTag(tagExtraSamples, ex...))
		}
		if img.tiled {
			tags = append(tags, longTag(tagTileWidth, uint64(img.tw)), longTag(tagTileLength, uint64(img.th)),
				wtag{tag: tagTileOffsets, typ: offType, ints: offsets}, wtag{tag: tagTileByteCounts, typ: offType, ints: counts})
		} else {
			rps := img.rps
			if rps == 0 {
				rps = img.h
			}
			tags = append(tags, longTag(tagRowsPerStrip, uint64(rps)),
				wtag{tag: tagStripOffsets, typ: offType, ints: offsets}, wtag{tag: tagStripByteCounts, typ: offType, ints: counts})
		}
		for _, extra := range img.tags {
			tags = slices.DeleteFunc(tags, func(x wtag) bool { return x.tag == extra.tag })
			tags = append(tags, extra)
		}
		tags = slices.DeleteFunc(tags, func(x wtag) bool { return slices.Contains(img.omit, x.tag) })
		slices.SortStableFunc(tags, func(a, b wtag) int { return int(a.tag) - int(b.tag) })
		dirTags[i] = tags
	}
	// Directories, each followed by the values that do not fit inline.
	nextAt := firstAt
	for _, tags := range dirTags {
		for len(out)%2 != 0 {
			out = append(out, 0)
		}
		here := uint64(len(out))
		if inst.big {
			order.PutUint64(out[nextAt:], here)
		} else {
			order.PutUint32(out[nextAt:], uint32(here))
		}
		entrySize, countSize, inline := 12, 2, 4
		if inst.big {
			entrySize, countSize, inline = 20, 8, 8
		}
		dirLen := countSize + len(tags)*entrySize + inline
		dirBytes := make([]byte, dirLen)
		valueAt := uint64(len(out) + dirLen)
		var values []byte
		if inst.big {
			order.PutUint64(dirBytes, uint64(len(tags)))
		} else {
			order.PutUint16(dirBytes, uint16(len(tags)))
		}
		for k, tg := range tags {
			e := dirBytes[countSize+k*entrySize:]
			count, b := tg.encode(order)
			order.PutUint16(e, tg.tag)
			order.PutUint16(e[2:], tg.typ)
			valueField := e[8:12]
			if inst.big {
				order.PutUint64(e[4:], count)
				valueField = e[12:20]
			} else {
				order.PutUint32(e[4:], uint32(count))
			}
			if len(b) <= inline {
				copy(valueField, b)
				continue
			}
			for len(values)%2 != 0 {
				values = append(values, 0)
			}
			at := valueAt + uint64(len(values))
			if inst.big {
				order.PutUint64(valueField, at)
			} else {
				order.PutUint32(valueField, uint32(at))
			}
			values = append(values, b...)
		}
		nextAt = len(out) + countSize + len(tags)*entrySize
		out = append(out, dirBytes...)
		out = append(out, values...)
	}
	return out
}

// sampleValue is a deterministic sample of a kind at (x, y) of plane p:
// spread over the kind's range, its extremes included, and for floats a
// NaN, an infinity and a negative zero.
func sampleValue(kind imaging.SampleKindE, x, y, p, seed int) float64 {
	h := uint64(x*73856093^y*19349663^p*83492791^seed*2654435761) * 0x9e3779b97f4a7c15
	h ^= h >> 29
	switch kind {
	case imaging.SampleKindU8:
		return float64(uint8(h))
	case imaging.SampleKindU16:
		if x == 0 && y == 0 {
			return math.MaxUint16
		}
		return float64(uint16(h))
	case imaging.SampleKindI16:
		if x == 0 && y == 0 {
			return math.MinInt16
		}
		return float64(int16(h))
	case imaging.SampleKindI32:
		if x == 1 && y == 0 {
			return math.MinInt32
		}
		return float64(int32(h))
	case imaging.SampleKindF32:
		switch {
		case x == 0 && y == 0:
			return math.NaN()
		case x == 1 && y == 0:
			return math.Inf(-1)
		case x == 2 && y == 0:
			return math.Copysign(0, -1)
		}
		return float64(float32(int32(h)) / 977)
	case imaging.SampleKindF64:
		switch {
		case x == 0 && y == 0:
			return math.NaN()
		case x == 1 && y == 0:
			return math.Inf(1)
		}
		return float64(int64(h)) / 1e7
	}
	return 0
}

func samplePlanes(kind imaging.SampleKindE, w, h, n, seed int) [][]float64 {
	planes := make([][]float64, n)
	for p := range planes {
		planes[p] = make([]float64, w*h)
		for y := range h {
			for x := range w {
				planes[p][y*w+x] = sampleValue(kind, x, y, p, seed)
			}
		}
	}
	return planes
}

// storedBits is a plane's samples as bit patterns of their kind, so that
// NaN and negative zero compare as stored.
func storedBits(p imaging.Plane) []uint64 {
	n := p.Width() * p.Height()
	out := make([]uint64, n)
	for i := range n {
		switch p.Kind() {
		case imaging.SampleKindU8:
			out[i] = uint64(p.U8()[i])
		case imaging.SampleKindU16:
			out[i] = uint64(p.U16()[i])
		case imaging.SampleKindI16:
			out[i] = uint64(uint16(p.I16()[i]))
		case imaging.SampleKindI32:
			out[i] = uint64(uint32(p.I32()[i]))
		case imaging.SampleKindF32:
			out[i] = uint64(math.Float32bits(p.F32()[i]))
		case imaging.SampleKindF64:
			out[i] = math.Float64bits(p.F64()[i])
		}
	}
	return out
}

func expectedBits(kind imaging.SampleKindE, v []float64) []uint64 {
	out := make([]uint64, len(v))
	for i, x := range v {
		switch kind {
		case imaging.SampleKindU8:
			out[i] = uint64(uint8(x))
		case imaging.SampleKindU16:
			out[i] = uint64(uint16(x))
		case imaging.SampleKindI16:
			out[i] = uint64(uint16(int16(x)))
		case imaging.SampleKindI32:
			out[i] = uint64(uint32(int32(x)))
		case imaging.SampleKindF32:
			out[i] = uint64(math.Float32bits(float32(x)))
		case imaging.SampleKindF64:
			out[i] = math.Float64bits(x)
		}
	}
	return out
}
