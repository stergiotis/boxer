package tiff

import (
	"context"
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// decoderName is what [imaging.Provenance.Decoder] records.
const decoderName = "boxer imaging/decode/tiff"

// decode reads the directories classified for reading into a stack.
func (inst *container) decode(ctx context.Context, sourceName string, opts Options) (stack imaging.Stack, err error) {
	maxPixels, maxBytes := opts.limits()
	first := inst.dirs[0]
	pages := make([]*dir, 0, len(inst.dirs))
	for _, d := range inst.dirs {
		switch d.Status {
		case DirectoryStatusRefused:
			return stack, d.refusal
		case DirectoryStatusRead:
			pages = append(pages, d)
		}
	}
	palette := first.Photometric == photoPalette
	bands := first.SamplesPerPixel
	outKind := first.Kind
	if palette {
		bands, outKind = 3, imaging.SampleKindU16
	}

	// The limits, from the directories alone.
	pixels := uint64(first.Width) * uint64(first.Height)
	if pixels > uint64(maxPixels) {
		return stack, limitErr("pixels per plane over MaxPixels", uint64(maxPixels))
	}
	total := mulSat(mulSat(mulSat(pixels, uint64(bands)), uint64(len(pages))), uint64(outKind.Bytes()))
	if palette {
		// The page of indices read before it is expanded.
		total += pixels * 2
	}
	if total > uint64(maxBytes) {
		return stack, limitErr("sample bytes over MaxBytes", uint64(maxBytes))
	}
	for _, d := range pages {
		if d.Tiled && uint64(d.TileWidth)*uint64(d.TileHeight) > uint64(maxPixels) {
			return stack, limitErr("pixels per tile over MaxPixels", uint64(maxPixels))
		}
		if d.maxChunkBytes() > uint64(maxBytes) {
			return stack, limitErr("bytes per strip or tile over MaxBytes", uint64(maxBytes))
		}
	}

	cr := &chunkReader{f: inst.f}
	planes := make([]imaging.Plane, 0, len(pages)*bands)
	for _, d := range pages {
		offsets, counts, cerr := inst.chunkTable(d)
		if cerr != nil {
			return stack, cerr
		}
		var got []imaging.Plane
		if palette {
			got, err = readPalette(ctx, cr, d, offsets, counts)
		} else {
			got, err = readPage(ctx, cr, d, offsets, counts)
		}
		if err != nil {
			return stack, err
		}
		planes = append(planes, got...)
	}
	return inst.assemble(sourceName, pages, planes)
}

// chunkTable reads a directory's strip or tile offsets and byte counts and
// checks that every chunk lies inside the file.
func (inst *container) chunkTable(d *dir) (offsets, counts []uint64, err error) {
	offTag, countTag := uint16(tagStripOffsets), uint16(tagStripByteCounts)
	if d.Tiled {
		offTag, countTag = tagTileOffsets, tagTileByteCounts
	}
	if offsets, err = inst.f.uints(d.entries[offTag], d.chunks*8); err != nil {
		return
	}
	if counts, err = inst.f.uints(d.entries[countTag], d.chunks*8); err != nil {
		return
	}
	for i, off := range offsets {
		n := counts[i]
		switch {
		case n == 0 && off == 0:
			return nil, nil, unsupported("a sparse file: a strip or tile with no bytes and no offset")
		case n == 0:
			return nil, nil, malformed("a strip or tile has a byte count of 0")
		case !inst.f.span(off, n):
			return nil, nil, malformed("a strip or tile lies outside the file")
		}
	}
	return
}

func readPage(ctx context.Context, cr *chunkReader, d *dir, offsets, counts []uint64) (planes []imaging.Plane, err error) {
	switch d.Kind {
	case imaging.SampleKindU8:
		return readPlanes(ctx, cr, d, offsets, counts, getU8, imaging.NewPlaneU8)
	case imaging.SampleKindU16:
		return readPlanes(ctx, cr, d, offsets, counts, getU16, imaging.NewPlaneU16)
	case imaging.SampleKindI16:
		return readPlanes(ctx, cr, d, offsets, counts, getI16, imaging.NewPlaneI16)
	case imaging.SampleKindI32:
		return readPlanes(ctx, cr, d, offsets, counts, getI32, imaging.NewPlaneI32)
	case imaging.SampleKindF32:
		return readPlanes(ctx, cr, d, offsets, counts, getF32, imaging.NewPlaneF32)
	case imaging.SampleKindF64:
		return readPlanes(ctx, cr, d, offsets, counts, getF64, imaging.NewPlaneF64)
	}
	return nil, unsupported("samples of kind " + d.Kind.String())
}

func readPlanes[T sampleI](ctx context.Context, cr *chunkReader, d *dir, offsets, counts []uint64,
	get func(order binary.ByteOrder, b []byte) T, mk func(int, int, []T) (imaging.Plane, error)) (planes []imaging.Plane, err error) {
	w, h := int(d.Width), int(d.Height)
	dst := make([][]T, d.SamplesPerPixel)
	for i := range dst {
		dst[i] = make([]T, w*h)
	}
	if err = decodeInto(ctx, cr, d, offsets, counts, dst, get); err != nil {
		return nil, err
	}
	planes = make([]imaging.Plane, len(dst))
	for i, s := range dst {
		if planes[i], err = mk(w, h, s); err != nil {
			return nil, err
		}
	}
	return planes, nil
}

// readPalette reads a page of palette indices and expands them through the
// colour map into red, green and blue planes of 16 bits, the colour map's
// own precision (TIFF 6.0 §5).
func readPalette(ctx context.Context, cr *chunkReader, d *dir, offsets, counts []uint64) (planes []imaging.Plane, err error) {
	w, h := int(d.Width), int(d.Height)
	idx := [][]uint16{make([]uint16, w*h)}
	get := getU16
	if d.Kind == imaging.SampleKindU8 {
		get = getIndex8
	}
	if err = decodeInto(ctx, cr, d, offsets, counts, idx, get); err != nil {
		return nil, err
	}
	cmap, err := cr.f.uints(d.entries[tagColorMap], maxSmallValue)
	if err != nil {
		return nil, err
	}
	n := len(cmap) / 3
	planes = make([]imaging.Plane, 3)
	for c := range 3 {
		lut := cmap[c*n : (c+1)*n]
		out := make([]uint16, w*h)
		for i, v := range idx[0] {
			// The map has 2^bits entries, so every index of the
			// directory's width is inside it.
			out[i] = uint16(lut[v])
		}
		if planes[c], err = imaging.NewPlaneU16(w, h, out); err != nil {
			return nil, err
		}
	}
	return planes, nil
}

// assemble makes the stack: the layout's axes and colour model, GDAL's
// labels, value transforms and invalid value, the placement and the
// provenance.
func (inst *container) assemble(sourceName string, pages []*dir, planes []imaging.Plane) (stack imaging.Stack, err error) {
	first := pages[0]
	notes := make([]string, 0, 8)
	prov := imaging.Provenance{
		SourceName:     sourceName,
		Format:         "tiff",
		Decoder:        decoderName,
		StoredBitDepth: uint8(first.BitsPerSample[0]),
	}
	if inst.f.big {
		prov.Format = "bigtiff"
	}
	model, labels, layoutNote := colourLayout(first)
	if layoutNote != "" {
		notes = append(notes, layoutNote)
	}
	prov.StoredColorType = storedColorType(first)
	switch first.Photometric {
	case photoWhiteIsZero:
		notes = append(notes, "The file is WhiteIsZero (MinIsWhite): 0 is white. The samples are as stored; nothing was inverted.")
	case photoPalette:
		prov.Derivations |= imaging.DerivationPaletteExpanded
	}
	bands := len(labels)

	// GDAL's metadata: band names, scales and offsets.
	transforms := make([]imaging.ValueTransform, bands)
	for i := range transforms {
		transforms[i] = imaging.IdentityTransform
	}
	if e, ok := first.entries[tagGDALMetadata]; ok {
		meta, merr := readGDALMetadata(inst.f, e)
		switch {
		case merr != nil:
			notes = append(notes, "GDAL_METADATA could not be read and was not applied: "+merr.Error()+".")
		case first.Photometric == photoPalette:
			notes = append(notes, "GDAL_METADATA describes the palette indices, not the expanded colours; it was not applied.")
		default:
			notes = append(notes, meta.apply(labels, transforms)...)
		}
	}

	// GDAL's invalid value.
	invalid, hasInvalid, invalidNote := nodataOf(first, model)
	if invalidNote != "" {
		notes = append(notes, invalidNote)
	}
	for i := range planes {
		b := i % bands
		if !transforms[b].IsIdentity() {
			if planes[i], err = planes[i].WithTransform(transforms[b]); err != nil {
				return stack, err
			}
		}
		if hasInvalid {
			if planes[i], err = planes[i].WithInvalid(invalid); err != nil {
				return stack, err
			}
		}
	}

	placement, placeNotes := placementOf(inst.f, first)
	notes = append(notes, placeNotes...)
	notes = append(notes, inst.skippedNotes()...)
	prov.Notes = notes

	axes := make([]imaging.Axis, 0, 2)
	if len(pages) > 1 {
		axes = append(axes, imaging.Axis{Name: "page", Role: imaging.AxisRoleSelector, Size: len(pages)})
	}
	axes = append(axes, imaging.Axis{Name: "band", Role: imaging.AxisRoleBand, Size: bands, Labels: labels})
	return imaging.NewStack(axes, planes, model, placement, prov)
}

// colourLayout is the colour model the samples are, the band labels, and a
// note when the samples are read as bands without a model (ADR-0398 §SD6).
func colourLayout(d *dir) (model imaging.ColorModelE, labels []string, note string) {
	if d.Photometric == photoPalette {
		return imaging.ColorModelRGB, []string{"red", "green", "blue"}, ""
	}
	base := baseSamples(d.Photometric)
	extra := d.SamplesPerPixel - base
	switch {
	case extra == 0 && base == 1:
		model = imaging.ColorModelGray
	case extra == 0:
		model = imaging.ColorModelRGB
	case extra == 1 && len(d.ExtraSamples) == 1 && d.ExtraSamples[0] == 2 && base == 1:
		model = imaging.ColorModelGrayAlpha
	case extra == 1 && len(d.ExtraSamples) == 1 && d.ExtraSamples[0] == 2:
		model = imaging.ColorModelRGBA
	}
	if model != 0 {
		return model, model.PlaneNames(), ""
	}
	labels = make([]string, d.SamplesPerPixel)
	for i := range labels {
		labels[i] = strconv.Itoa(i + 1)
	}
	var why string
	switch {
	case len(d.ExtraSamples) != extra:
		why = strconv.Itoa(extra) + " extra samples, of which ExtraSamples describes " + strconv.Itoa(len(d.ExtraSamples))
	case extra == 1:
		why = "its extra sample is " + extraSampleName(d.ExtraSamples[0])
		if d.ExtraSamples[0] == 1 {
			why += ", which is not divided out"
		}
	default:
		kinds := make([]string, len(d.ExtraSamples))
		for i, e := range d.ExtraSamples {
			kinds[i] = extraSampleName(e)
		}
		why = strconv.Itoa(extra) + " extra samples (" + strings.Join(kinds, ", ") + ")"
	}
	return 0, labels, "Read as " + strconv.Itoa(d.SamplesPerPixel) + " bands without a colour model: the file is " +
		d.PhotometricName + " with " + why + "."
}

// storedColorType names the photometric interpretation and the layout in
// the file's terms.
func storedColorType(d *dir) string {
	var b strings.Builder
	b.WriteString(d.PhotometricName)
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(d.SamplesPerPixel))
	if d.SamplesPerPixel == 1 {
		b.WriteString(" sample")
	} else {
		b.WriteString(" samples")
	}
	if len(d.ExtraSamples) > 0 {
		kinds := make([]string, len(d.ExtraSamples))
		for i, e := range d.ExtraSamples {
			kinds[i] = extraSampleName(e)
		}
		b.WriteString(" (extra: " + strings.Join(kinds, ", ") + ")")
	}
	if d.Planar {
		b.WriteString(", planar")
	} else {
		b.WriteString(", chunky")
	}
	if d.Tiled {
		b.WriteString(", tiles")
	} else {
		b.WriteString(", strips")
	}
	return b.String()
}

// skippedNotes counts the directories not read, by why.
func (inst *container) skippedNotes() (notes []string) {
	var reduced, masks, differ, subIFDs int
	for _, d := range inst.dirs {
		subIFDs += d.SubIFDs
		if d.Status != DirectoryStatusSkipped {
			continue
		}
		switch {
		case d.NewSubfileType&1 != 0:
			reduced++
		case d.NewSubfileType&4 != 0:
			masks++
		default:
			differ++
		}
	}
	count := func(n int, one, many string) {
		if n == 1 {
			notes = append(notes, "1 "+one)
		} else if n > 1 {
			notes = append(notes, strconv.Itoa(n)+" "+many)
		}
	}
	count(reduced, "reduced-resolution directory was not read.", "reduced-resolution directories were not read.")
	count(masks, "mask directory was not read.", "mask directories were not read.")
	count(differ, "directory unlike the first in size, sample kind or layout was not read.",
		"directories unlike the first in size, sample kind or layout were not read.")
	count(subIFDs, "SubIFD was not read.", "SubIFDs were not read.")
	return
}
