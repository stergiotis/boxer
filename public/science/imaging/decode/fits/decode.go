package fits

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// DecoderName is what [imaging.Provenance.Decoder] says of a stack this
// package read.
const DecoderName = "boxer FITS reader"

// Decode reads one image HDU of a FITS file into a stack (ADR-0398 §SD7):
// NAXIS1 as columns, NAXIS2 as rows with the file's bottom row first turned
// into row 0 at the top, and every further axis a selector axis. sourceName
// is recorded in the provenance; it is a name for a person, not a path this
// function opens. Limits are checked from the header before any sample is
// read.
func Decode(ctx context.Context, r io.ReaderAt, size int64, sourceName string, opts Options) (stack imaging.Stack, err error) {
	maxPixels, maxBytes, maxPlanes := opts.limits()
	r, size, gzipped, err := unwrap(ctx, r, size, maxBytes+gzipHeaderAllowance)
	if err != nil {
		return stack, eb.Build().Str("source", sourceName).Errorf("open FITS file: %w", err)
	}
	scans, err := scan(ctx, r, size)
	if err != nil {
		return stack, eb.Build().Str("source", sourceName).Errorf("read FITS headers: %w", err)
	}
	chosen, err := pick(scans, opts)
	if err != nil {
		return stack, eb.Build().Str("source", sourceName).Errorf("choose an HDU: %w", err)
	}
	s := scans[chosen]
	h, d := s.hdr, s.hdu

	width, height := d.Axes[0], d.Axes[1]
	if width > math.MaxInt32 || height > math.MaxInt32 || width*height > maxPixels {
		return stack, eb.Build().Str("source", sourceName).Int64("width", width).Int64("height", height).Int64("maxPixels", maxPixels).
			Errorf("check the plane size: %w", refuse(ErrLimit, "a plane has more pixels than the pixel limit"))
	}
	planeCount := int64(1)
	for _, a := range d.Axes[2:] {
		planeCount *= a // d.DataBytes did not overflow, so neither does this
	}
	if planeCount > int64(maxPlanes) {
		return stack, eb.Build().Str("source", sourceName).Int64("planes", planeCount).Int("maxPlanes", maxPlanes).
			Errorf("check the plane count: %w", refuse(ErrLimit, "the image has more planes than the plane limit"))
	}
	if d.DataBytes > maxBytes {
		return stack, eb.Build().Str("source", sourceName).Int64("bytes", d.DataBytes).Int64("maxBytes", maxBytes).
			Errorf("check the sample bytes: %w", refuse(ErrLimit, "the image's samples take more bytes than the byte limit"))
	}

	prov := imaging.Provenance{
		SourceName:      sourceName,
		Format:          "fits",
		Decoder:         DecoderName,
		StoredBitDepth:  uint8(abs(d.BitPix)),
		StoredColorType: "BITPIX " + strconv.Itoa(d.BitPix) + ", " + strconv.Itoa(len(d.Axes)) + " axes",
		Derivations:     imaging.DerivationRowsFlipped,
	}
	if gzipped {
		prov.Notes = append(prov.Notes, "the file is gzip-compressed and was decompressed in memory")
	}
	if len(scans) > 1 {
		label := "HDU " + strconv.Itoa(chosen)
		if d.ExtName != "" {
			label += " (" + d.ExtName + ")"
		}
		prov.Notes = append(prov.Notes, label+" of "+strconv.Itoa(len(scans))+" was read")
	}
	if t := tableNote(scans, chosen); t != "" {
		prov.Notes = append(prov.Notes, t)
	}

	transform := imaging.IdentityTransform
	if v, present, valid := h.real("BSCALE"); present {
		if !valid {
			return stack, eb.Build().Str("source", sourceName).Errorf("read BSCALE: %w", refuse(ErrMalformed, "BSCALE is not a finite number"))
		}
		transform.Scale = v
	}
	if v, present, valid := h.real("BZERO"); present {
		if !valid {
			return stack, eb.Build().Str("source", sourceName).Errorf("read BZERO: %w", refuse(ErrMalformed, "BZERO is not a finite number"))
		}
		transform.Offset = v
	}
	if transform.Scale == 0 {
		return stack, eb.Build().Str("source", sourceName).Errorf("read BSCALE: %w", refuse(ErrMalformed, "BSCALE = 0"))
	}
	invalid, hasInvalid := 0.0, false
	if c, present := h.card("BLANK"); present {
		v, _, valid := h.integer("BLANK")
		switch {
		case d.BitPix < 0:
			prov.Notes = append(prov.Notes, "BLANK was ignored: it applies to integer samples only, and NaN marks an undefined float sample")
		case !valid:
			prov.Notes = append(prov.Notes, "BLANK = "+c.Value+" is not an integer and was ignored")
		case d.BitPix == 8 && (v < 0 || v > math.MaxUint8),
			d.BitPix == 16 && (v < math.MinInt16 || v > math.MaxInt16),
			d.BitPix == 32 && (v < math.MinInt32 || v > math.MaxInt32):
			prov.Notes = append(prov.Notes, "BLANK = "+c.Value+" is not a value BITPIX "+strconv.Itoa(d.BitPix)+" can store and was ignored")
		default:
			invalid, hasInvalid = float64(v), true
		}
	}
	if u, ok := h.str("BUNIT"); ok && strings.TrimSpace(u) != "" {
		prov.Notes = append(prov.Notes, "values are in "+strings.TrimSpace(u))
	}

	wcs := readWCS(h, d.Axes)
	prov.Notes = append(prov.Notes, wcs.notes...)

	// The stack's axes: FITS axes NAXIS … 3, slowest first, so that the
	// planes are in the file's order.
	axes := make([]imaging.Axis, 0, len(d.Axes)-2)
	names := make(map[string]int, len(d.Axes))
	for k := 3; k <= len(d.Axes); k++ {
		s, _ := h.str(axisKw("CTYPE", k))
		names[strings.TrimSpace(s)]++
	}
	for k := len(d.Axes); k >= 3; k-- {
		s, _ := h.str(axisKw("CTYPE", k))
		name := strings.TrimSpace(s)
		if name == "" || names[name] > 1 || strings.HasPrefix(name, "axis") {
			name = "axis" + strconv.Itoa(k)
		}
		axes = append(axes, imaging.Axis{
			Name:   name,
			Role:   imaging.AxisRoleSelector,
			Size:   int(d.Axes[k-1]),
			Unit:   wcs.units[k],
			Coords: wcs.coords[k],
		})
	}

	w, hh, np := int(width), int(height), int(planeCount)
	planes := make([]imaging.Plane, np)
	var perr error
	switch d.BitPix {
	case 8:
		bufs := allocate[uint8](np, w*hh)
		if err = readSamples(ctx, r, d.DataOffset, bufs, w, hh, 1, func(src []byte, dst []uint8) { copy(dst, src) }); err == nil {
			for i, b := range bufs {
				planes[i], perr = imaging.NewPlaneU8(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case 16:
		bufs := allocate[int16](np, w*hh)
		if err = readSamples(ctx, r, d.DataOffset, bufs, w, hh, 2, func(src []byte, dst []int16) {
			for i := range dst {
				dst[i] = int16(binary.BigEndian.Uint16(src[2*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				planes[i], perr = imaging.NewPlaneI16(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case 32:
		bufs := allocate[int32](np, w*hh)
		if err = readSamples(ctx, r, d.DataOffset, bufs, w, hh, 4, func(src []byte, dst []int32) {
			for i := range dst {
				dst[i] = int32(binary.BigEndian.Uint32(src[4*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				planes[i], perr = imaging.NewPlaneI32(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case -32:
		bufs := allocate[float32](np, w*hh)
		if err = readSamples(ctx, r, d.DataOffset, bufs, w, hh, 4, func(src []byte, dst []float32) {
			for i := range dst {
				dst[i] = math.Float32frombits(binary.BigEndian.Uint32(src[4*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				planes[i], perr = imaging.NewPlaneF32(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	case -64:
		bufs := allocate[float64](np, w*hh)
		if err = readSamples(ctx, r, d.DataOffset, bufs, w, hh, 8, func(src []byte, dst []float64) {
			for i := range dst {
				dst[i] = math.Float64frombits(binary.BigEndian.Uint64(src[8*i:]))
			}
		}); err == nil {
			for i, b := range bufs {
				planes[i], perr = imaging.NewPlaneF64(w, hh, b)
				err = errors.Join(err, perr)
			}
		}
	}
	if err != nil {
		return stack, eb.Build().Str("source", sourceName).Int("hdu", chosen).Errorf("read samples: %w", err)
	}
	for i := range planes {
		if !transform.IsIdentity() {
			if planes[i], err = planes[i].WithTransform(transform); err != nil {
				return stack, eb.Build().Str("source", sourceName).Errorf("apply BSCALE and BZERO: %w", errors.Join(ErrMalformed, err))
			}
		}
		if hasInvalid {
			if planes[i], err = planes[i].WithInvalid(invalid); err != nil {
				return stack, eb.Build().Str("source", sourceName).Errorf("apply BLANK: %w", errors.Join(ErrMalformed, err))
			}
		}
	}
	stack, err = imaging.NewStack(axes, planes, 0, wcs.placement, prov)
	if err != nil {
		return stack, eb.Build().Str("source", sourceName).Errorf("assemble the stack: %w", errors.Join(ErrMalformed, err))
	}
	return
}

func allocate[T any](planes, samples int) [][]T {
	out := make([][]T, planes)
	for i := range out {
		out[i] = make([]T, samples)
	}
	return out
}

// readChunk is how many bytes of samples are read at once; ctx is checked
// between chunks.
const readChunk = 1 << 20

// readSamples reads planes·w·h samples of bpp bytes from off, NAXIS1
// fastest, and stores each plane's rows bottom row first, which is the
// file's order, as top row first.
func readSamples[T any](ctx context.Context, r io.ReaderAt, off int64, planes [][]T, w, h, bpp int, conv func(src []byte, dst []T)) error {
	total := int64(len(planes)) * int64(w) * int64(h)
	buf := make([]byte, min(int64(readChunk), total*int64(bpp)))
	per := int64(w) * int64(h)
	var g int64 // samples read so far
	for g < total {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(int64(len(buf)/bpp), total-g)
		chunk := buf[:n*int64(bpp)]
		if _, err := r.ReadAt(chunk, off+g*int64(bpp)); err != nil {
			return eb.Build().Int64("offset", off+g*int64(bpp)).Errorf("read data: %w", errors.Join(ErrMalformed, err))
		}
		for len(chunk) > 0 {
			p := g / per
			within := g % per
			row, col := within/int64(w), within%int64(w)
			run := min(int64(w)-col, int64(len(chunk)/bpp))
			dst := (int64(h)-1-row)*int64(w) + col
			conv(chunk[:run*int64(bpp)], planes[p][dst:dst+run])
			chunk = chunk[run*int64(bpp):]
			g += run
		}
	}
	return nil
}
