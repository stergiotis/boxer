package decode

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// jpegInfo is what a JPEG file says about itself in the segments before its
// first scan.
type jpegInfo struct {
	frame      uint8 // the start-of-frame marker, 0xc0…0xcf; 0 when none was seen
	precision  uint8
	components uint8
	hasICC     bool
}

// frameName names a start-of-frame marker the way the format's
// specification does.
func (inst jpegInfo) frameName() string {
	switch inst.frame {
	case 0xc0:
		return "baseline"
	case 0xc1:
		return "extended sequential"
	case 0xc2:
		return "progressive"
	case 0xc3, 0xc7, 0xcb, 0xcf:
		return "lossless"
	case 0xc9, 0xca, 0xcd, 0xce:
		return "arithmetic-coded"
	case 0xc5, 0xc6:
		return "hierarchical"
	case 0:
		return "unknown"
	}
	return "other"
}

// scanJPEG walks the marker segments up to the first scan.
func scanJPEG(data []byte) (info jpegInfo) {
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xff {
			return
		}
		marker := data[pos+1]
		if marker == 0xff {
			pos++
			continue
		}
		if marker == 0xda || marker == 0xd9 {
			return
		}
		length := int(data[pos+2])<<8 | int(data[pos+3])
		if length < 2 || pos+2+length > len(data) {
			return
		}
		body := data[pos+4 : pos+2+length]
		isFrame := marker >= 0xc0 && marker <= 0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc
		switch {
		case isFrame && len(body) >= 6:
			info.frame, info.precision, info.components = marker, body[0], body[5]
		case marker == 0xe2 && bytes.HasPrefix(body, []byte("ICC_PROFILE\x00")):
			info.hasICC = true
		}
		pos += 2 + length
	}
	return
}

func decodeJPEG(data []byte, sourceName string) (img imaging.Image, err error) {
	info := scanJPEG(data)
	src, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return img, eb.Build().Str("source", sourceName).Str("frame", info.frameName()).
			Uint8("bitsPerSample", info.precision).Uint8("components", info.components).
			Errorf("the JPEG decoder does not read this file: %w", err)
	}
	prov := imaging.Provenance{
		SourceName:     sourceName,
		Format:         FormatJPEG.String(),
		Decoder:        "go image/jpeg",
		StoredBitDepth: info.precision,
		Lossy:          true,
	}
	if info.hasICC {
		prov.ColorDeclaration = "ICC profile"
	}
	model := imaging.ColorModelRGB
	switch s := src.(type) {
	case *image.Gray:
		model, prov.StoredColorType = imaging.ColorModelGray, "gray"
	case *image.YCbCr:
		prov.StoredColorType = "ycbcr"
		prov.Derivations |= imaging.DerivationYCbCrConverted
		src = rgbOfYCbCr(s)
	case *image.CMYK:
		prov.StoredColorType = "cmyk"
		prov.Derivations |= imaging.DerivationCMYKConverted
		src = rgbOfCMYK(s)
	default:
		prov.StoredColorType = "rgb"
	}
	planes, err := planesOf(src, model, imaging.SampleKindU8)
	if err != nil {
		return img, eb.Build().Str("source", sourceName).Errorf("JPEG pixels to planes: %w", err)
	}
	return imaging.NewImage(model, planes, prov)
}

// rgbOfYCbCr converts luma and chroma to red, green and blue with the
// standard library's matrix, taking each pixel's chroma from the sample that
// covers it. The result is opaque.
func rgbOfYCbCr(s *image.YCbCr) *image.NRGBA {
	b := s.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := dst.Pix[(y-b.Min.Y)*dst.Stride:]
		for x := b.Min.X; x < b.Max.X; x++ {
			yi, ci := s.YOffset(x, y), s.COffset(x, y)
			r, g, bl := color.YCbCrToRGB(s.Y[yi], s.Cb[ci], s.Cr[ci])
			o := 4 * (x - b.Min.X)
			row[o], row[o+1], row[o+2], row[o+3] = r, g, bl, 0xff
		}
	}
	return dst
}

// rgbOfCMYK converts ink amounts to red, green and blue with the standard
// library's formula, which assumes no profile. The result is opaque.
func rgbOfCMYK(s *image.CMYK) *image.NRGBA {
	b := s.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		in := s.Pix[y*s.Stride : y*s.Stride+4*b.Dx()]
		out := dst.Pix[y*dst.Stride:]
		for x := range b.Dx() {
			r, g, bl := color.CMYKToRGB(in[4*x], in[4*x+1], in[4*x+2], in[4*x+3])
			out[4*x], out[4*x+1], out[4*x+2], out[4*x+3] = r, g, bl, 0xff
		}
	}
	return dst
}
