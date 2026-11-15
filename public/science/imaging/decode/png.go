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
	width, height int
	bitDepth      uint8
	colorType     uint8
	compression   uint8
	filter        uint8
	interlace     uint8
	hasTRNS       bool
	animated      bool
	colourDecl    []string
	palette       []byte // PLTE's body: three bytes an entry
	trns          []byte // tRNS's body
	// idatAt is the offset of the first IDAT chunk's length field; 0 when
	// the scan found none.
	idatAt int
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
			if seenHeader {
				// The first header is the one the standard decoder reads;
				// a second is refused by the row decoder.
				break
			}
			if length < 13 {
				return info, eb.Build().Int("length", length).Errorf("PNG header chunk is too short")
			}
			info.width, info.height = int(binary.BigEndian.Uint32(chunk[0:4])), int(binary.BigEndian.Uint32(chunk[4:8]))
			info.bitDepth, info.colorType = chunk[8], chunk[9]
			info.compression, info.filter, info.interlace = chunk[10], chunk[11], chunk[12]
			seenHeader = true
		case "PLTE":
			info.palette = chunk
		case "tRNS":
			info.hasTRNS = true
			info.trns = chunk
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
			if kind == "IDAT" {
				info.idatAt = pos
			}
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
	model, kind, prov, err := pngLayout(info, sourceName)
	if err != nil {
		return img, err
	}
	planes, err := planesOf(src, model, kind)
	if err != nil {
		return img, eb.Build().Str("source", sourceName).Errorf("PNG pixels to planes: %w", err)
	}
	return imaging.NewImage(model, planes, prov)
}

// pngLayout is what a PNG's header and chunks make of it: the colour model
// and sample kind of its planes, and the provenance that records what the
// reader did. Both the whole decode and the row decoder use it, so the two
// give the same image.
func pngLayout(info pngInfo, sourceName string) (model imaging.ColorModelE, kind imaging.SampleKindE, prov imaging.Provenance, err error) {
	prov = imaging.Provenance{
		SourceName:       sourceName,
		Format:           FormatPNG.String(),
		Decoder:          "go image/png",
		StoredBitDepth:   info.bitDepth,
		ColorDeclaration: strings.Join(info.colourDecl, "; "),
	}
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
		return model, kind, prov, eb.Build().Str("source", sourceName).Uint8("colorType", info.colorType).Errorf("PNG colour type is not one the format defines")
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
	kind = imaging.SampleKindU8
	if info.bitDepth == 16 {
		kind = imaging.SampleKindU16
	}
	if info.bitDepth < 8 && info.colorType != pngColorPalette {
		prov.Derivations |= imaging.DerivationDepthExpanded
	}
	if info.animated {
		prov.Derivations |= imaging.DerivationDefaultFrameOnly
	}
	return model, kind, prov, nil
}

// pngConfig is the standard decoder's reading of a PNG header.
var pngConfig = png.DecodeConfig
