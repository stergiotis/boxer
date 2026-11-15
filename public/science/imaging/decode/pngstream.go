package decode

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// The row decoder for a non-interlaced PNG (ADR-0374 §SD7), written from the
// PNG specification (W3C, third edition): the chunks are walked and their
// checksums checked, the IDAT chunks' data inflated as one zlib stream, each
// row unfiltered against the one above and turned into samples. Its working
// memory is two rows of the file and a batch of rows of samples, whatever
// the image's height. It gives the planes the whole decode gives
// ([pngLayout] decides both), which its tests and fuzz target hold it to.

// rowBatch is how many rows are handed to the store at once.
const rowBatch = 64

// streamPNG decodes the rows of a non-interlaced PNG into b.
func streamPNG(ctx context.Context, data []byte, info pngInfo, b *tiles.Builder, model imaging.ColorModelE, kind imaging.SampleKindE) error {
	if err := checkPNGHeader(info); err != nil {
		return err
	}
	if err := checkPNGChunksBefore(data, info.idatAt); err != nil {
		return err
	}
	channels := map[uint8]int{pngColorGray: 1, pngColorRGB: 3, pngColorPalette: 1, pngColorGrayAlpha: 2, pngColorRGBA: 4}[info.colorType]
	depth := int(info.bitDepth)
	bitsPerPixel := channels * depth
	rowBytes := (info.width*bitsPerPixel + 7) / 8
	// The filters' distance back: bytes per complete pixel, at least one.
	bpp := max(bitsPerPixel/8, 1)

	idat := &idatReader{data: data, pos: info.idatAt}
	zr, err := zlib.NewReader(idat)
	if err != nil {
		return eb.Build().Errorf("PNG image data does not start a zlib stream: %w", err)
	}
	defer zr.Close()

	conv, err := newRowConverter(info, model, kind)
	if err != nil {
		return err
	}
	prev := make([]byte, rowBytes)
	cur := make([]byte, rowBytes+1)
	for y0 := 0; y0 < info.height; y0 += rowBatch {
		if err = ctx.Err(); err != nil {
			return err
		}
		n := min(rowBatch, info.height-y0)
		conv.start(n)
		for range n {
			if _, err = io.ReadFull(zr, cur); err != nil {
				return eb.Build().Int("row", y0).Errorf("PNG image data ends before its last row: %w", err)
			}
			if err = unfilter(cur[0], cur[1:], prev, bpp); err != nil {
				return err
			}
			conv.row(cur[1:])
			copy(prev, cur[1:])
		}
		planes, err := conv.planes()
		if err != nil {
			return err
		}
		if err = b.AddPlanes(planes); err != nil {
			return err
		}
	}
	// The end of the zlib stream, whose checksum the reader checks there,
	// and any image data past it; then the rest of the file, its checksums
	// and its end, as the whole decode reads them.
	if extra, err := io.Copy(io.Discard, zr); err != nil {
		return eb.Build().Errorf("PNG image data does not end as a zlib stream: %w", err)
	} else if extra > 0 {
		return eb.Build().Int64("bytes", extra).Errorf("PNG image data is longer than its rows")
	}
	if _, err = io.Copy(io.Discard, idat); err != nil {
		return err
	}
	if idat.err != nil {
		return idat.err
	}
	return checkRestOfPNG(data, idat.next)
}

// checkPNGHeader refuses a header the format does not define.
func checkPNGHeader(info pngInfo) error {
	valid := map[uint8][]uint8{
		pngColorGray: {1, 2, 4, 8, 16}, pngColorRGB: {8, 16}, pngColorPalette: {1, 2, 4, 8},
		pngColorGrayAlpha: {8, 16}, pngColorRGBA: {8, 16},
	}[info.colorType]
	ok := false
	for _, d := range valid {
		ok = ok || d == info.bitDepth
	}
	switch {
	case !ok:
		return eb.Build().Uint8("colorType", info.colorType).Uint8("bitDepth", info.bitDepth).Errorf("PNG colour type and bit depth are not a pair the format defines")
	case info.compression != 0 || info.filter != 0 || info.interlace != 0:
		return eb.Build().Uint8("compression", info.compression).Uint8("filter", info.filter).Uint8("interlace", info.interlace).
			Errorf("PNG compression, filter or interlace method is not one the row decoder reads")
	case info.width <= 0 || info.height <= 0:
		return eb.Build().Int("width", info.width).Int("height", info.height).Errorf("PNG has no size")
	case info.idatAt == 0:
		return eb.Build().Errorf("PNG has no image data")
	case info.colorType == pngColorPalette && (len(info.palette) == 0 || len(info.palette)%3 != 0 || len(info.palette) > 3*256):
		return eb.Build().Int("paletteBytes", len(info.palette)).Errorf("PNG palette is missing or malformed")
	}
	return nil
}

// unfilter undoes a row's filter in place (PNG specification, §9).
func unfilter(filter byte, row, prev []byte, bpp int) error {
	switch filter {
	case 0:
	case 1:
		for i := bpp; i < len(row); i++ {
			row[i] += row[i-bpp]
		}
	case 2:
		for i := range row {
			row[i] += prev[i]
		}
	case 3:
		for i := range row {
			var left byte
			if i >= bpp {
				left = row[i-bpp]
			}
			row[i] += byte((int(left) + int(prev[i])) / 2)
		}
	case 4:
		for i := range row {
			var a, c byte
			if i >= bpp {
				a, c = row[i-bpp], prev[i-bpp]
			}
			row[i] += paeth(a, prev[i], c)
		}
	default:
		return eb.Build().Uint8("filter", filter).Errorf("PNG row filter is not one the format defines")
	}
	return nil
}

// paeth is the Paeth predictor.
func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	}
	return c
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// idatReader reads the data of consecutive IDAT chunks as one stream,
// checking each chunk's checksum, and stops at the first other chunk.
type idatReader struct {
	data []byte
	pos  int // the next chunk's length field
	left []byte
	done bool
	next int // where the chunk after the last IDAT starts
	err  error
}

func (inst *idatReader) Read(p []byte) (n int, err error) {
	for len(inst.left) == 0 {
		if inst.done {
			return 0, io.EOF
		}
		length, kind, body, end, cerr := chunkAt(inst.data, inst.pos)
		if cerr != nil {
			inst.err, inst.done = cerr, true
			return 0, cerr
		}
		if kind != "IDAT" {
			inst.done, inst.next = true, inst.pos
			return 0, io.EOF
		}
		_ = length
		inst.left, inst.pos = body, end
	}
	n = copy(p, inst.left)
	inst.left = inst.left[n:]
	return n, nil
}

// chunkAt reads the chunk starting at pos, checking its checksum.
func chunkAt(data []byte, pos int) (length int, kind string, body []byte, end int, err error) {
	if pos+8 > len(data) {
		return 0, "", nil, 0, eb.Build().Int("at", pos).Errorf("PNG ends inside a chunk header")
	}
	l := binary.BigEndian.Uint32(data[pos:])
	if uint64(l) > uint64(len(data)) || pos+12+int(l) > len(data) {
		return 0, "", nil, 0, eb.Build().Int("at", pos).Uint32("length", l).Errorf("PNG chunk runs past the end of the file")
	}
	length, kind = int(l), string(data[pos+4:pos+8])
	body = data[pos+8 : pos+8+length]
	end = pos + 12 + length
	sum := crc32.NewIEEE()
	_, _ = sum.Write(data[pos+4 : pos+8+length])
	if sum.Sum32() != binary.BigEndian.Uint32(data[pos+8+length:]) {
		return 0, "", nil, 0, eb.Build().Str("chunk", kind).Errorf("PNG chunk checksum does not match")
	}
	return
}

// checkPNGChunksBefore checks the chunks before the image data: their
// checksums, and that the header comes first.
func checkPNGChunksBefore(data []byte, idatAt int) error {
	for pos, first := len(pngSignature), true; pos < idatAt; first = false {
		_, kind, _, end, err := chunkAt(data, pos)
		if err != nil {
			return err
		}
		if first != (kind == "IHDR") {
			return eb.Build().Str("chunk", kind).Errorf("PNG does not start with its one header chunk")
		}
		pos = end
	}
	return nil
}

// checkRestOfPNG walks the chunks after the image data to the end chunk,
// checking each.
func checkRestOfPNG(data []byte, pos int) error {
	if pos == 0 {
		return eb.Build().Errorf("PNG image data is longer than its rows")
	}
	for {
		_, kind, _, end, err := chunkAt(data, pos)
		if err != nil {
			return err
		}
		switch kind {
		case "IEND":
			return nil
		case "IDAT":
			return eb.Build().Errorf("PNG image data is split by another chunk")
		}
		pos = end
	}
}

// rowConverter turns unfiltered rows into the samples of the planes
// [pngLayout] decides on, a batch at a time.
type rowConverter struct {
	info   pngInfo
	model  imaging.ColorModelE
	kind   imaging.SampleKindE
	w, n   int
	y      int
	p8     [4][]uint8
	p16    [4][]uint16
	lut    [256][4]uint8 // palette: red, green, blue, alpha
	keyed  bool          // a colour key makes the alpha plane
	key    [3]uint16
	maxVal uint16
}

func newRowConverter(info pngInfo, model imaging.ColorModelE, kind imaging.SampleKindE) (inst *rowConverter, err error) {
	inst = &rowConverter{info: info, model: model, kind: kind, w: info.width}
	inst.maxVal = 0xff
	if kind == imaging.SampleKindU16 {
		inst.maxVal = 0xffff
	}
	switch info.colorType {
	case pngColorPalette:
		for i := range inst.lut {
			inst.lut[i] = [4]uint8{0, 0, 0, 0xff}
		}
		for i := range len(info.palette) / 3 {
			inst.lut[i] = [4]uint8{info.palette[3*i], info.palette[3*i+1], info.palette[3*i+2], 0xff}
		}
		if len(info.trns) > len(info.palette)/3 {
			return nil, eb.Build().Int("trns", len(info.trns)).Errorf("PNG transparency has more entries than the palette")
		}
		for i, a := range info.trns {
			inst.lut[i][3] = a
		}
	case pngColorGray:
		if info.hasTRNS {
			if len(info.trns) < 2 {
				return nil, eb.Build().Errorf("PNG grey colour key is too short")
			}
			inst.keyed, inst.key[0] = true, binary.BigEndian.Uint16(info.trns)
		}
	case pngColorRGB:
		if info.hasTRNS {
			if len(info.trns) < 6 {
				return nil, eb.Build().Errorf("PNG colour key is too short")
			}
			inst.keyed = true
			for c := range 3 {
				inst.key[c] = binary.BigEndian.Uint16(info.trns[2*c:])
			}
		}
	}
	return inst, nil
}

// start begins a batch of n rows.
func (inst *rowConverter) start(n int) {
	inst.n, inst.y = n, 0
	for c := range inst.model.PlaneCount() {
		if inst.kind == imaging.SampleKindU16 {
			inst.p16[c] = make([]uint16, n*inst.w)
		} else {
			inst.p8[c] = make([]uint8, n*inst.w)
		}
	}
}

// row converts one unfiltered row.
func (inst *rowConverter) row(r []byte) {
	o := inst.y * inst.w
	inst.y++
	depth := int(inst.info.bitDepth)
	switch inst.info.colorType {
	case pngColorPalette:
		for x := range inst.w {
			e := inst.lut[packed(r, x, depth)]
			for c := range inst.model.PlaneCount() {
				inst.p8[c][o+x] = e[c]
			}
		}
	case pngColorGray:
		for x := range inst.w {
			var raw uint16
			if depth == 16 {
				raw = binary.BigEndian.Uint16(r[2*x:])
				inst.p16[0][o+x] = raw
			} else {
				raw = uint16(packed(r, x, depth))
				inst.p8[0][o+x] = uint8(uint32(raw) * 0xff / (1<<depth - 1))
			}
			if inst.keyed {
				inst.setAlpha(1, o+x, raw == inst.key[0])
			}
		}
	case pngColorRGB:
		for x := range inst.w {
			var raw [3]uint16
			for c := range 3 {
				if depth == 16 {
					raw[c] = binary.BigEndian.Uint16(r[6*x+2*c:])
					inst.p16[c][o+x] = raw[c]
				} else {
					raw[c] = uint16(r[3*x+c])
					inst.p8[c][o+x] = r[3*x+c]
				}
			}
			if inst.keyed {
				inst.setAlpha(3, o+x, raw == inst.key)
			}
		}
	case pngColorGrayAlpha, pngColorRGBA:
		ch := inst.model.PlaneCount()
		for x := range inst.w {
			for c := range ch {
				if depth == 16 {
					inst.p16[c][o+x] = binary.BigEndian.Uint16(r[2*(ch*x+c):])
				} else {
					inst.p8[c][o+x] = r[ch*x+c]
				}
			}
		}
	}
}

// setAlpha writes the alpha a colour key gives: none where the sample is
// the key, full elsewhere.
func (inst *rowConverter) setAlpha(plane, i int, transparent bool) {
	a := inst.maxVal
	if transparent {
		a = 0
	}
	if inst.kind == imaging.SampleKindU16 {
		inst.p16[plane][i] = a
	} else {
		inst.p8[plane][i] = uint8(a)
	}
}

// packed is sample x of a row of depth-bit samples, most significant first.
func packed(r []byte, x, depth int) uint8 {
	if depth == 8 {
		return r[x]
	}
	bit := x * depth
	return (r[bit/8] >> (8 - depth - bit%8)) & (1<<depth - 1)
}

// planes hands over the batch.
func (inst *rowConverter) planes() (out []imaging.Plane, err error) {
	out = make([]imaging.Plane, inst.model.PlaneCount())
	for c := range out {
		if inst.kind == imaging.SampleKindU16 {
			out[c], err = imaging.NewPlaneU16(inst.w, inst.n, inst.p16[c])
		} else {
			out[c], err = imaging.NewPlaneU8(inst.w, inst.n, inst.p8[c])
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DecodeTiled reads a PNG or JPEG file into a tiles store. A non-interlaced
// PNG is decoded row by row into the store, holding no more of the image
// than the store and a band of rows; an interlaced PNG and a JPEG are
// decoded whole, as [Decode] does, and then stored, so their peak is the
// image twice. onStore, when not nil, is given the store as soon as it
// exists — before the rows of a streamed PNG are decoded — so a viewer can
// show tiles as they are built. The image is the same as [Decode] gives:
// the same planes and the same provenance.
func DecodeTiled(ctx context.Context, data []byte, sourceName string, opts Options, tileOpts tiles.Options, onStore func(*tiles.Store)) (*tiles.Store, error) {
	if Sniff(data) == FormatPNG {
		info, err := scanPNG(data)
		if err == nil && info.interlace == 0 {
			if err = checkSize(data, sourceName, opts); err != nil {
				return nil, err
			}
			return decodePNGTiled(ctx, data, info, sourceName, tileOpts, onStore)
		}
	}
	img, err := Decode(data, sourceName, opts)
	if err != nil {
		return nil, err
	}
	s, err := tiles.FromImage(ctx, img, tileOpts)
	if err != nil {
		return nil, err
	}
	if onStore != nil {
		onStore(s)
	}
	return s, nil
}

func decodePNGTiled(ctx context.Context, data []byte, info pngInfo, sourceName string, tileOpts tiles.Options, onStore func(*tiles.Store)) (*tiles.Store, error) {
	model, kind, prov, err := pngLayout(info, sourceName)
	if err != nil {
		return nil, err
	}
	prov.Decoder = "boxer PNG row decoder"
	b, err := tiles.NewBuilder(info.width, info.height, model, kind, nil, prov, tileOpts)
	if err != nil {
		return nil, err
	}
	if onStore != nil {
		onStore(b.Store())
	}
	if err = streamPNG(ctx, data, info, b, model, kind); err != nil {
		err = eb.Build().Str("source", sourceName).Errorf("the PNG decoder does not read this file: %w", err)
		b.Fail(err)
		return nil, err
	}
	if err = b.Finish(); err != nil {
		return nil, err
	}
	return b.Store(), nil
}

// checkSize reads the header through the standard decoder, as [Decode]
// does, and refuses an image over the pixel limit before anything is
// decoded.
func checkSize(data []byte, sourceName string, opts Options) error {
	maxPixels := opts.MaxPixels
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	cfg, err := pngConfig(bytes.NewReader(data))
	if err != nil {
		return eb.Build().Str("source", sourceName).Stringer("format", FormatPNG).Errorf("read image header: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return eb.Build().Str("source", sourceName).Int("width", cfg.Width).Int("height", cfg.Height).
			Int64("maxPixels", maxPixels).Errorf("image has more pixels than the decode limit")
	}
	return nil
}
