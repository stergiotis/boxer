package decode

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// pngInfo is what a PNG file says about itself in the chunks before its
// pixel data.
type pngInfo struct {
	bitDepth   uint8
	colorType  uint8
	hasTRNS    bool
	animated   bool
	colourDecl []string
}

const (
	pngColorGray      = 0
	pngColorRGB       = 2
	pngColorPalette   = 3
	pngColorGrayAlpha = 4
	pngColorRGBA      = 6
)

// scanPNG walks the chunks up to the first IDAT. It reads lengths and types
// only; the decoder verifies the checksums.
func scanPNG(data []byte) (info pngInfo, err error) {
	pos := len(pngSignature)
	seenHeader := false
	for pos+8 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[pos:]))
		kind := string(data[pos+4 : pos+8])
		body := pos + 8
		if length < 0 || body+length+4 > len(data) {
			break
		}
		chunk := data[body : body+length]
		switch kind {
		case "IHDR":
			if length < 13 {
				return info, eb.Build().Int("length", length).Errorf("PNG header chunk is too short")
			}
			info.bitDepth, info.colorType = chunk[8], chunk[9]
			seenHeader = true
		case "tRNS":
			info.hasTRNS = true
		case "acTL":
			info.animated = true
		case "sRGB":
			info.colourDecl = append(info.colourDecl, "sRGB")
		case "gAMA":
			if length >= 4 {
				g := float64(binary.BigEndian.Uint32(chunk)) / 100000
				info.colourDecl = append(info.colourDecl, "gamma "+strconv.FormatFloat(g, 'g', 6, 64))
			}
		case "iCCP":
			name, _, _ := bytes.Cut(chunk, []byte{0})
			info.colourDecl = append(info.colourDecl, "ICC profile "+strconv.Quote(string(name)))
		case "cHRM":
			info.colourDecl = append(info.colourDecl, "chromaticities")
		case "cICP":
			info.colourDecl = append(info.colourDecl, "coding-independent code points")
		case "IDAT", "IEND":
			pos = len(data)
			continue
		}
		pos = body + length + 4
	}
	if !seenHeader {
		return info, eb.Build().Errorf("PNG file has no header chunk")
	}
	return info, nil
}

func decodePNG(data []byte, sourceName string) (img imaging.Image, err error) {
	info, err := scanPNG(data)
	if err != nil {
		return img, eb.Build().Str("source", sourceName).Errorf("read PNG chunks: %w", err)
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return img, eb.Build().Str("source", sourceName).Uint8("bitDepth", info.bitDepth).Uint8("colorType", info.colorType).
			Errorf("the PNG decoder does not read this file: %w", err)
	}
	prov := imaging.Provenance{
		SourceName:       sourceName,
		Format:           FormatPNG.String(),
		Decoder:          "go image/png",
		StoredBitDepth:   info.bitDepth,
		ColorDeclaration: strings.Join(info.colourDecl, "; "),
	}
	var model imaging.ColorModelE
	switch info.colorType {
	case pngColorGray:
		model, prov.StoredColorType = imaging.ColorModelGray, "gray"
	case pngColorGrayAlpha:
		model, prov.StoredColorType = imaging.ColorModelGrayAlpha, "gray+alpha"
	case pngColorRGB:
		model, prov.StoredColorType = imaging.ColorModelRGB, "rgb"
	case pngColorRGBA:
		model, prov.StoredColorType = imaging.ColorModelRGBA, "rgba"
	case pngColorPalette:
		model, prov.StoredColorType = imaging.ColorModelRGB, "palette"
		prov.Derivations |= imaging.DerivationPaletteExpanded
	default:
		return img, eb.Build().Str("source", sourceName).Uint8("colorType", info.colorType).Errorf("PNG colour type is not one the format defines")
	}
	if info.hasTRNS {
		switch model {
		case imaging.ColorModelGray:
			model = imaging.ColorModelGrayAlpha
			prov.Derivations |= imaging.DerivationColorKeyAlpha
		case imaging.ColorModelRGB:
			model = imaging.ColorModelRGBA
			if info.colorType != pngColorPalette {
				prov.Derivations |= imaging.DerivationColorKeyAlpha
			}
		}
	}
	kind := imaging.SampleKindU8
	if info.bitDepth == 16 {
		kind = imaging.SampleKindU16
	}
	if info.bitDepth < 8 && info.colorType != pngColorPalette {
		prov.Derivations |= imaging.DerivationDepthExpanded
	}
	if info.animated {
		prov.Derivations |= imaging.DerivationDefaultFrameOnly
	}
	planes, err := planesOf(src, model, kind)
	if err != nil {
		return img, eb.Build().Str("source", sourceName).Errorf("PNG pixels to planes: %w", err)
	}
	return imaging.NewImage(model, planes, prov)
}
