package tiff

import (
	"io"
	"math"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// Tags this reader looks at (TIFF 6.0 Appendix A, the BigTIFF and GeoTIFF
// descriptions, GDAL's private tags).
const (
	tagNewSubfileType   = 254
	tagSubfileType      = 255
	tagImageWidth       = 256
	tagImageLength      = 257
	tagBitsPerSample    = 258
	tagCompression      = 259
	tagPhotometric      = 262
	tagStripOffsets     = 273
	tagOrientation      = 274
	tagSamplesPerPixel  = 277
	tagRowsPerStrip     = 278
	tagStripByteCounts  = 279
	tagXResolution      = 282
	tagYResolution      = 283
	tagPlanarConfig     = 284
	tagResolutionUnit   = 296
	tagPredictor        = 317
	tagColorMap         = 320
	tagTileWidth        = 322
	tagTileLength       = 323
	tagTileOffsets      = 324
	tagTileByteCounts   = 325
	tagSubIFDs          = 330
	tagExtraSamples     = 338
	tagSampleFormat     = 339
	tagImageDepth       = 32997
	tagModelPixelScale  = 33550
	tagModelTiepoint    = 33922
	tagModelTransform   = 34264
	tagGeoKeyDirectory  = 34735
	tagGeoDoubleParams  = 34736
	tagGeoASCIIParams   = 34737
	tagGDALMetadata     = 42112
	tagGDALNoData       = 42113
	maxRowsPerStripNone = math.MaxUint32
)

// Photometric interpretations read here.
const (
	photoWhiteIsZero = 0
	photoBlackIsZero = 1
	photoRGB         = 2
	photoPalette     = 3
)

// Compressions read here.
const (
	compressionNone       = 1
	compressionLZW        = 5
	compressionDeflate    = 8
	compressionDeflateOld = 32946
	compressionPackBits   = 32773
)

func compressionName(c uint16) string {
	switch c {
	case compressionNone:
		return "none"
	case 2:
		return "CCITT modified Huffman RLE"
	case 3:
		return "CCITT T.4"
	case 4:
		return "CCITT T.6"
	case compressionLZW:
		return "LZW"
	case 6:
		return "old-style JPEG"
	case 7:
		return "JPEG"
	case compressionDeflate:
		return "Deflate"
	case compressionDeflateOld:
		return "Deflate (old code)"
	case compressionPackBits:
		return "PackBits"
	case 32809:
		return "ThunderScan"
	case 34676:
		return "SGI LogLuv"
	case 34677:
		return "SGI LogLuv 24-bit"
	case 34712:
		return "JPEG 2000"
	case 34887:
		return "LERC"
	case 34925:
		return "LZMA"
	case 50000:
		return "Zstandard"
	case 50001:
		return "WebP"
	case 50002:
		return "JPEG XL"
	}
	return "unknown"
}

func photometricName(p uint16) string {
	switch p {
	case photoWhiteIsZero:
		return "WhiteIsZero"
	case photoBlackIsZero:
		return "BlackIsZero"
	case photoRGB:
		return "RGB"
	case photoPalette:
		return "Palette"
	case 4:
		return "transparency mask"
	case 5:
		return "Separated (CMYK)"
	case 6:
		return "YCbCr"
	case 8:
		return "CIELab"
	case 9:
		return "ICCLab"
	case 10:
		return "ITULab"
	case 32803:
		return "CFA"
	case 32844:
		return "LogL"
	case 32845:
		return "LogLuv"
	case 34892:
		return "LinearRaw"
	}
	return "unknown"
}

func orientationName(o uint16) string {
	switch o {
	case 1:
		return "top-left"
	case 2:
		return "top-right"
	case 3:
		return "bottom-right"
	case 4:
		return "bottom-left"
	case 5:
		return "left-top"
	case 6:
		return "right-top"
	case 7:
		return "right-bottom"
	case 8:
		return "left-bottom"
	}
	return "unknown"
}

func extraSampleName(e uint16) string {
	switch e {
	case 0:
		return "unspecified"
	case 1:
		return "associated alpha"
	case 2:
		return "unassociated alpha"
	}
	return "unknown"
}

// named is "what N (name)", the form a refusal names a coded value in.
func named(what string, code uint64, name string) string {
	return what + " " + itoa(code) + " (" + name + ")"
}

// dir is one directory: what the file states, and what this reader makes
// of it.
type dir struct {
	Directory
	entries map[uint16]entry
	// refusal is the first reason the directory cannot be read, or nil.
	refusal error
	// chunks is the number of strips or tiles the layout needs.
	chunks uint64
	// across and down are the chunks per row and per column of one plane.
	across, down int64
	// bytesPerSample is the stored size of one sample.
	bytesPerSample int
	nodata         string
	hasNodata      bool
	gdalMeta       string
}

func (inst *dir) refuse(err error) {
	if inst.refusal == nil {
		inst.refusal = err
	}
}

func (inst *dir) has(tag uint16) bool {
	_, ok := inst.entries[tag]
	return ok
}

// container is a parsed file: the header and the directory chain.
type container struct {
	f    *file
	dirs []*dir
}

// scan reads the header and every directory of the chain, describing each
// and classifying it for [Decode].
func scan(r io.ReaderAt, size int64) (c *container, err error) {
	if r == nil || size < 0 {
		return nil, malformed("no source")
	}
	f, off, err := parseHeader(r, size)
	if err != nil {
		return nil, err
	}
	if off == 0 {
		return nil, malformed("the file has no directory")
	}
	c = &container{f: f, dirs: make([]*dir, 0, 4)}
	visited := make(map[uint64]struct{}, 4)
	for off != 0 {
		if _, seen := visited[off]; seen {
			return nil, malformed("the directory chain is cyclic")
		}
		if len(visited) >= maxDirectories {
			return nil, limitErr("directories in the chain", maxDirectories)
		}
		visited[off] = struct{}{}
		entries, next, perr := f.parseIFD(off)
		if perr != nil {
			return nil, perr
		}
		c.dirs = append(c.dirs, describe(f, off, entries))
		off = next
	}
	c.classify()
	return c, nil
}

// describe reads what a directory states. Whatever keeps it from being
// read is recorded as its refusal, the first one found, and the rest of
// the description is still filled for [Inspect].
func describe(f *file, off uint64, entries []entry) (d *dir) {
	d = &dir{entries: make(map[uint16]entry, len(entries))}
	d.Offset = int64(off)
	for _, e := range entries {
		if _, dup := d.entries[e.tag]; !dup {
			d.entries[e.tag] = e
		}
	}
	scalar := func(tag uint16, def uint64) uint64 {
		e, ok := d.entries[tag]
		if !ok {
			return def
		}
		v, err := f.firstUint(e)
		if err != nil {
			d.refuse(err)
			return def
		}
		return v
	}
	width, height := scalar(tagImageWidth, 0), scalar(tagImageLength, 0)
	if !d.has(tagImageWidth) || !d.has(tagImageLength) {
		d.refuse(malformed("ImageWidth or ImageLength is missing"))
	}
	if width == 0 || height == 0 {
		d.refuse(malformed("the image is empty"))
	}
	if width > math.MaxInt32 || height > math.MaxInt32 {
		d.refuse(limitErr("image side over 2^31-1", math.MaxInt32))
	}
	d.Width, d.Height = int64(min(width, math.MaxInt64)), int64(min(height, math.MaxInt64))

	spp := scalar(tagSamplesPerPixel, 1)
	if spp == 0 || spp > math.MaxUint16 {
		d.refuse(malformed("SamplesPerPixel is 0 or over 65535"))
		spp = 1
	}
	d.SamplesPerPixel = int(spp)
	d.BitsPerSample = perSample(f, d, tagBitsPerSample, 1)
	d.SampleFormat = perSample(f, d, tagSampleFormat, 1)
	d.Compression = uint16(scalar(tagCompression, compressionNone))
	d.CompressionName = compressionName(d.Compression)
	if p, ok := d.entries[tagPhotometric]; ok {
		v, err := f.firstUint(p)
		if err != nil {
			d.refuse(err)
		}
		d.Photometric, d.HasPhotometric = uint16(v), err == nil
		d.PhotometricName = photometricName(d.Photometric)
	}
	d.Predictor = uint16(scalar(tagPredictor, 1))
	planar := scalar(tagPlanarConfig, 1)
	d.Planar = planar == 2
	d.Orientation = uint16(scalar(tagOrientation, 1))
	d.NewSubfileType = uint32(scalar(tagNewSubfileType, 0))
	switch scalar(tagSubfileType, 1) {
	case 2:
		d.NewSubfileType |= 1
	case 3:
		d.NewSubfileType |= 2
	}
	if e, ok := d.entries[tagExtraSamples]; ok {
		v, err := f.uints(e, 8*spp)
		if err != nil {
			d.refuse(err)
		}
		d.ExtraSamples = make([]uint16, len(v))
		for i, x := range v {
			d.ExtraSamples[i] = uint16(x)
		}
	}
	if e, ok := d.entries[tagSubIFDs]; ok {
		d.SubIFDs = int(min(e.count, math.MaxInt32))
	}
	if e, ok := d.entries[tagGDALNoData]; ok {
		s, err := f.ascii(e, maxSmallValue)
		if err == nil {
			d.nodata, d.hasNodata = strings.TrimSpace(s), true
		}
	}
	if e, ok := d.entries[tagGDALMetadata]; ok {
		if s, err := f.ascii(e, maxGDALMetadata); err == nil {
			d.gdalMeta = s
		}
	}

	// Each check below names what it refuses; the first that fails is the
	// directory's refusal.
	if planar != 1 && planar != 2 {
		d.refuse(malformed("PlanarConfiguration is neither 1 nor 2"))
	}
	d.Kind, d.bytesPerSample = sampleKind(d)
	checkCompression(d)
	checkPhotometric(d)
	checkPredictor(d)
	if d.Orientation != 1 {
		d.refuse(unsupported(named("orientation", uint64(d.Orientation), orientationName(d.Orientation))))
	}
	if depth := scalar(tagImageDepth, 1); depth > 1 {
		d.refuse(unsupported("ImageDepth " + itoa(depth) + " (volumetric images)"))
	}
	layout(d, scalar)
	return d
}

// perSample reads a tag with one value per sample. A single value stands
// for every sample, as several writers store it.
func perSample(f *file, d *dir, tag uint16, def uint16) []uint16 {
	out := make([]uint16, d.SamplesPerPixel)
	e, ok := d.entries[tag]
	if !ok {
		for i := range out {
			out[i] = def
		}
		return out
	}
	if e.count != 1 && e.count != uint64(d.SamplesPerPixel) {
		d.refuse(malformed("tag " + itoa(uint64(tag)) + " has neither one value nor one per sample"))
		return out
	}
	v, err := f.uints(e, 8*uint64(d.SamplesPerPixel))
	if err != nil {
		d.refuse(err)
		return out
	}
	for i := range out {
		out[i] = uint16(min(v[min(i, len(v)-1)], math.MaxUint16))
	}
	return out
}

// sampleKind is the kind the directory's samples read to, and the stored
// size of one sample; a refusal names the bit width and format otherwise.
func sampleKind(d *dir) (kind imaging.SampleKindE, size int) {
	bits, format := d.BitsPerSample[0], d.SampleFormat[0]
	for i := range d.BitsPerSample {
		if d.BitsPerSample[i] != bits {
			d.refuse(unsupported("samples of mixed bit widths"))
			return 0, 0
		}
		if d.SampleFormat[i] != format {
			d.refuse(unsupported("samples of mixed sample formats"))
			return 0, 0
		}
	}
	b := itoa(uint64(bits))
	switch format {
	case 1:
		switch bits {
		case 8:
			return imaging.SampleKindU8, 1
		case 16:
			return imaging.SampleKindU16, 2
		}
		d.refuse(unsupported("unsigned " + b + "-bit samples"))
	case 2:
		switch bits {
		case 16:
			return imaging.SampleKindI16, 2
		case 32:
			return imaging.SampleKindI32, 4
		}
		d.refuse(unsupported("signed " + b + "-bit samples"))
	case 3:
		switch bits {
		case 32:
			return imaging.SampleKindF32, 4
		case 64:
			return imaging.SampleKindF64, 8
		}
		d.refuse(unsupported(b + "-bit floating-point samples"))
	case 4:
		d.refuse(unsupported("sample format 4 (undefined)"))
	case 5:
		d.refuse(unsupported("sample format 5 (complex integer)"))
	case 6:
		d.refuse(unsupported("sample format 6 (complex floating point)"))
	default:
		d.refuse(unsupported("sample format " + itoa(uint64(format))))
	}
	return 0, 0
}

func checkCompression(d *dir) {
	switch d.Compression {
	case compressionNone, compressionLZW, compressionDeflate, compressionDeflateOld, compressionPackBits:
		return
	}
	d.refuse(unsupported(named("compression", uint64(d.Compression), d.CompressionName)))
}

// baseSamples is the number of samples the photometric interpretation
// itself names; the rest are extra samples.
func baseSamples(photometric uint16) int {
	if photometric == photoRGB {
		return 3
	}
	return 1
}

func checkPhotometric(d *dir) {
	if !d.HasPhotometric {
		d.refuse(malformed("PhotometricInterpretation is missing"))
		return
	}
	switch d.Photometric {
	case photoWhiteIsZero, photoBlackIsZero, photoRGB, photoPalette:
	default:
		d.refuse(unsupported(named("photometric interpretation", uint64(d.Photometric), d.PhotometricName)))
		return
	}
	if d.SamplesPerPixel < baseSamples(d.Photometric) {
		d.refuse(malformed(d.PhotometricName + " with fewer samples per pixel than it names"))
		return
	}
	if d.Photometric != photoPalette {
		return
	}
	if d.SamplesPerPixel != 1 {
		d.refuse(unsupported("a palette image with extra samples"))
		return
	}
	if d.Kind != imaging.SampleKindU8 && d.Kind != imaging.SampleKindU16 {
		if d.Kind != 0 {
			d.refuse(unsupported("palette indices of kind " + d.Kind.String()))
		}
		return
	}
	e, ok := d.entries[tagColorMap]
	if !ok || e.typ != typeShort || e.count != uint64(3)<<d.BitsPerSample[0] {
		d.refuse(malformed("a palette image without a colour map of 3·2^bits SHORT values"))
	}
}

func checkPredictor(d *dir) {
	switch d.Predictor {
	case 1:
	case 2:
		if d.Kind.IsFloat() {
			d.refuse(unsupported("predictor 2 (horizontal differencing) on floating-point samples"))
		}
	case 3:
		if d.Kind != 0 && !d.Kind.IsFloat() {
			d.refuse(unsupported("predictor 3 (floating point) on integer samples"))
		}
	default:
		d.refuse(unsupported("predictor " + itoa(uint64(d.Predictor))))
	}
}

// layout reads the strip or tile geometry and checks that the offset and
// byte-count tags count the chunks it needs. The arrays themselves are read
// by [Decode], after its limits.
func layout(d *dir, scalar func(uint16, uint64) uint64) {
	if d.Width <= 0 || d.Height <= 0 || d.Width > math.MaxInt32 || d.Height > math.MaxInt32 {
		return
	}
	var offTag, countTag uint16
	if d.has(tagTileWidth) || d.has(tagTileLength) || d.has(tagTileOffsets) {
		d.Tiled = true
		tw, th := scalar(tagTileWidth, 0), scalar(tagTileLength, 0)
		if tw == 0 || th == 0 || tw > math.MaxInt32 || th > math.MaxInt32 {
			d.refuse(malformed("TileWidth or TileLength is missing, 0 or over 2^31-1"))
			return
		}
		d.TileWidth, d.TileHeight = int64(tw), int64(th)
		d.across = (d.Width + d.TileWidth - 1) / d.TileWidth
		d.down = (d.Height + d.TileHeight - 1) / d.TileHeight
		offTag, countTag = tagTileOffsets, tagTileByteCounts
	} else {
		rps := scalar(tagRowsPerStrip, maxRowsPerStripNone)
		if rps == 0 {
			d.refuse(malformed("RowsPerStrip is 0"))
			return
		}
		d.RowsPerStrip = int64(min(rps, uint64(d.Height)))
		d.across = 1
		d.down = (d.Height + d.RowsPerStrip - 1) / d.RowsPerStrip
		offTag, countTag = tagStripOffsets, tagStripByteCounts
	}
	// across, down ≤ 2^31 and planes ≤ 2^16, so the product fits.
	d.chunks = uint64(d.across) * uint64(d.down)
	if d.Planar {
		d.chunks *= uint64(d.SamplesPerPixel)
	}
	offs, okO := d.entries[offTag]
	counts, okC := d.entries[countTag]
	switch {
	case !okO || !okC:
		d.refuse(malformed("the strip or tile offsets or byte counts are missing"))
	case offs.count != d.chunks || counts.count != d.chunks:
		d.refuse(malformed("the strip or tile offsets or byte counts do not count the chunks the layout needs"))
	}
}

// classify says for each directory what [Decode] does with it: the first
// is the image; a later one like it is a page; reduced-resolution images,
// masks and directories unlike the first are skipped.
func (inst *container) classify() {
	first := inst.dirs[0]
	if first.refusal != nil {
		first.Status, first.Reason = DirectoryStatusRefused, first.refusal.Error()
	} else {
		first.Status = DirectoryStatusRead
	}
	for _, d := range inst.dirs[1:] {
		switch {
		case d.NewSubfileType&1 != 0:
			d.Status, d.Reason = DirectoryStatusSkipped, "a reduced-resolution image"
		case d.NewSubfileType&4 != 0:
			d.Status, d.Reason = DirectoryStatusSkipped, "a transparency mask"
		default:
			if why := differs(first, d); why != "" {
				d.Status, d.Reason = DirectoryStatusSkipped, "differs from the first directory in "+why
				if d.refusal != nil {
					d.Reason += "; it would also be refused: " + d.refusal.Error()
				}
			} else if d.refusal != nil {
				d.Status, d.Reason = DirectoryStatusRefused, d.refusal.Error()
			} else {
				d.Status = DirectoryStatusRead
			}
		}
	}
}

// differs names what keeps d from being a page of first's stack, or is
// empty.
func differs(first, d *dir) string {
	switch {
	case d.Width != first.Width || d.Height != first.Height:
		return "size"
	case d.Kind != first.Kind || d.Kind == 0:
		return "sample kind"
	case d.SamplesPerPixel != first.SamplesPerPixel || d.Photometric != first.Photometric ||
		d.HasPhotometric != first.HasPhotometric || !slices.Equal(d.ExtraSamples, first.ExtraSamples):
		return "samples per pixel or their interpretation"
	case d.hasNodata != first.hasNodata || d.nodata != first.nodata:
		return "the invalid value (GDAL_NODATA)"
	case d.gdalMeta != first.gdalMeta:
		return "the band metadata (GDAL_METADATA)"
	}
	return ""
}

func (inst *container) info() (info Info) {
	refused := false
	info.Format = "tiff"
	if inst.f.big {
		info.Format = "bigtiff"
	}
	info.BigEndian = inst.f.order.Uint16([]byte{0, 1}) == 1
	info.Directories = make([]Directory, len(inst.dirs))
	for i, d := range inst.dirs {
		dd := d.Directory
		dd.BitsPerSample = slices.Clone(dd.BitsPerSample)
		dd.SampleFormat = slices.Clone(dd.SampleFormat)
		dd.ExtraSamples = slices.Clone(dd.ExtraSamples)
		info.Directories[i] = dd
		switch d.Status {
		case DirectoryStatusRead:
			info.Pages++
		case DirectoryStatusRefused:
			refused = true
		}
	}
	if refused {
		info.Pages = 0
	}
	return
}
