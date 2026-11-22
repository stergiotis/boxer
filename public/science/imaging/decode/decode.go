package decode

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// FormatE is a file format this package reads. The zero value is a format it
// does not.
type FormatE uint8

const (
	FormatPNG FormatE = iota + 1
	FormatJPEG
)

func (inst FormatE) String() string {
	switch inst {
	case FormatPNG:
		return "png"
	case FormatJPEG:
		return "jpeg"
	}
	return "unknown"
}

// DefaultMaxPixels bounds a decode at 64 megapixels: an 8192 × 8192 image,
// which as four 16-bit planes is half a gibibyte of samples. It is also the
// default of [Limits.MaxPixels], a bound on one plane of a stack.
const DefaultMaxPixels int64 = 64 << 20

// Options configures a decode. The zero value is usable.
type Options struct {
	// MaxPixels refuses a file with more pixels than this before decoding
	// it; 0 means [DefaultMaxPixels].
	MaxPixels int64
}

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// Sniff tells the format of a file from its first bytes.
func Sniff(data []byte) FormatE {
	switch {
	case bytes.HasPrefix(data, pngSignature):
		return FormatPNG
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return FormatJPEG
	}
	return 0
}

// Decode reads a PNG or JPEG file into an image. sourceName is recorded in
// the provenance and appears in errors; it is a name for a person, not a path
// this function opens.
func Decode(data []byte, sourceName string, opts Options) (img imaging.Image, err error) {
	maxPixels := opts.MaxPixels
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	format := Sniff(data)
	var cfg image.Config
	switch format {
	case FormatPNG:
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case FormatJPEG:
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	default:
		return img, eb.Build().Str("source", sourceName).Errorf("not a PNG or a JPEG file")
	}
	if err != nil {
		return img, eb.Build().Str("source", sourceName).Stringer("format", format).Errorf("read image header: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return img, eb.Build().Str("source", sourceName).Int("width", cfg.Width).Int("height", cfg.Height).
			Int64("maxPixels", maxPixels).Errorf("image has more pixels than the decode limit")
	}
	if format == FormatPNG {
		img, err = decodePNG(data, sourceName)
	} else {
		img, err = decodeJPEG(data, sourceName)
	}
	return
}

// planesOf turns a decoded picture into the planes of the given model and
// kind. Known concrete types are read from their pixel arrays; anything else
// goes through the colour model, pixel by pixel, which is slow and right for
// every type including one whose alpha is premultiplied.
func planesOf(src image.Image, model imaging.ColorModelE, kind imaging.SampleKindE) (planes []imaging.Plane, err error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	n := w * h
	count := model.PlaneCount()
	if w <= 0 || h <= 0 || count == 0 {
		return nil, eb.Build().Int("width", w).Int("height", h).Stringer("model", model).Errorf("decoded image is empty")
	}
	var p8 [4][]uint8
	var p16 [4][]uint16
	for c := range count {
		if kind == imaging.SampleKindU16 {
			p16[c] = make([]uint16, n)
		} else {
			p8[c] = make([]uint8, n)
		}
	}
	// channel[c] is which of red, green, blue, alpha feeds plane c.
	var channel [4]int
	switch model {
	case imaging.ColorModelGray:
		channel = [4]int{0}
	case imaging.ColorModelGrayAlpha:
		channel = [4]int{0, 3}
	default:
		channel = [4]int{0, 1, 2, 3}
	}
	done := false
	switch s := src.(type) {
	case *image.Gray:
		if kind == imaging.SampleKindU8 && model == imaging.ColorModelGray {
			for y := range h {
				copy(p8[0][y*w:(y+1)*w], s.Pix[y*s.Stride:y*s.Stride+w])
			}
			done = true
		}
	case *image.Gray16:
		if kind == imaging.SampleKindU16 && model == imaging.ColorModelGray {
			for y := range h {
				row := s.Pix[y*s.Stride : y*s.Stride+2*w]
				for x := range w {
					p16[0][y*w+x] = uint16(row[2*x])<<8 | uint16(row[2*x+1])
				}
			}
			done = true
		}
	case *image.NRGBA:
		if kind == imaging.SampleKindU8 {
			for y := range h {
				row := s.Pix[y*s.Stride : y*s.Stride+4*w]
				for c := range count {
					dst, ch := p8[c][y*w:(y+1)*w], channel[c]
					for x := range dst {
						dst[x] = row[4*x+ch]
					}
				}
			}
			done = true
		}
	case *image.RGBA:
		// Premultiplied, so only an image known to be opaque is read here.
		if kind == imaging.SampleKindU8 && !model.HasAlpha() {
			for y := range h {
				row := s.Pix[y*s.Stride : y*s.Stride+4*w]
				for c := range count {
					dst, ch := p8[c][y*w:(y+1)*w], channel[c]
					for x := range dst {
						dst[x] = row[4*x+ch]
					}
				}
			}
			done = true
		}
	case *image.NRGBA64:
		if kind == imaging.SampleKindU16 {
			readWide(s.Pix, s.Stride, w, h, count, channel, &p16)
			done = true
		}
	case *image.RGBA64:
		if kind == imaging.SampleKindU16 && !model.HasAlpha() {
			readWide(s.Pix, s.Stride, w, h, count, channel, &p16)
			done = true
		}
	case *image.Paletted:
		if kind == imaging.SampleKindU8 {
			var lut [256][4]uint8
			for i, col := range s.Palette {
				if i >= len(lut) {
					break
				}
				nc := color.NRGBAModel.Convert(col).(color.NRGBA)
				lut[i] = [4]uint8{nc.R, nc.G, nc.B, nc.A}
			}
			for y := range h {
				row := s.Pix[y*s.Stride : y*s.Stride+w]
				for c := range count {
					dst, ch := p8[c][y*w:(y+1)*w], channel[c]
					for x, idx := range row {
						dst[x] = lut[idx][ch]
					}
				}
			}
			done = true
		}
	}
	if !done {
		for y := range h {
			for x := range w {
				nc := color.NRGBA64Model.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA64)
				v := [4]uint16{nc.R, nc.G, nc.B, nc.A}
				for c := range count {
					if kind == imaging.SampleKindU16 {
						p16[c][y*w+x] = v[channel[c]]
					} else {
						p8[c][y*w+x] = uint8(v[channel[c]] >> 8)
					}
				}
			}
		}
	}
	planes = make([]imaging.Plane, count)
	for c := range count {
		if kind == imaging.SampleKindU16 {
			planes[c], err = imaging.NewPlaneU16(w, h, p16[c])
		} else {
			planes[c], err = imaging.NewPlaneU8(w, h, p8[c])
		}
		if err != nil {
			return nil, err
		}
	}
	return planes, nil
}

// readWide de-interleaves big-endian 16-bit RGBA rows into planes.
func readWide(pix []uint8, stride, w, h, count int, channel [4]int, planes *[4][]uint16) {
	for y := range h {
		row := pix[y*stride : y*stride+8*w]
		for c := range count {
			dst, ch := planes[c][y*w:(y+1)*w], channel[c]
			for x := range dst {
				dst[x] = uint16(row[8*x+2*ch])<<8 | uint16(row[8*x+2*ch+1])
			}
		}
	}
}
