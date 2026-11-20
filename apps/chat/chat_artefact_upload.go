package chat

// An image file the person adds to the artefact's set (ADR-0385 §SD1,
// §SD2). The set keeps a re-encoding of the file's pixels, turned the way
// its orientation tag says, and nothing else of the file: a photo's
// location, time and device, a comment, an embedded thumbnail and a colour
// profile all stay behind. The chat names the image; the file's own name is
// the person's to read in the panel.

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SampleImageSeed adds a generated image to a new window's artefact, as a
// file the person added (ADR-0009 seed variable): the host's file dialog
// starts in the home directory and takes no typed path, so a scene cannot
// reach a fixture through it.
var SampleImageSeed = env.NewBool(env.Spec{
	Name:        "BOXER_CHAT_SAMPLE_IMAGE",
	Default:     "false",
	Description: "add a generated sample image to the artefact of a new chat window, as a file the person added would be (ADR-0385); needs BOXER_CHAT_ARTEFACT; for scenes and demos",
	Category:    env.CategoryE("boxer-chat"),
})

// sampleImage is the seed's image: bands of colour over a ramp, as a PNG.
func sampleImage() []byte {
	const w, h = 320, 200
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	bands := [][3]uint8{{0x2a, 0x6f, 0x97}, {0xe9, 0xc4, 0x6a}, {0xe7, 0x6f, 0x51}, {0x2a, 0x9d, 0x8f}}
	for y := range h {
		for x := range w {
			b := bands[x*len(bands)/w]
			shade := uint8(255 - y*96/h)
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(uint16(b[0])*uint16(shade)/255), uint8(uint16(b[1])*uint16(shade)/255), uint8(uint16(b[2])*uint16(shade)/255), 255
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// uploadJPEGQuality is the quality a JPEG is encoded again at: one
// generation of loss the eye does not find, at a size near the file's.
const uploadJPEGQuality = 90

// reencodeUpload is a PNG or JPEG file's pixels as the set keeps them: in
// the file's format, upright, without its metadata. The size is checked
// from the header before anything is decoded.
func reencodeUpload(data []byte, maxPixels int64) (out []byte, ext string, err error) {
	if _, _, ext, err = imageSize(data, maxPixels); err != nil {
		return nil, "", err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", eb.Build().Errorf("decode: %w", err)
	}
	if o := fileOrientation(data, ext); o != orientUpright {
		src = upright(src, o)
	}
	var buf bytes.Buffer
	if ext == extJPEG {
		err = jpeg.Encode(&buf, src, &jpeg.Options{Quality: uploadJPEGQuality})
	} else {
		err = png.Encode(&buf, src)
	}
	if err != nil {
		return nil, "", eb.Build().Errorf("encode: %w", err)
	}
	return buf.Bytes(), ext, nil
}

// orientUpright is the orientation tag's value for pixels stored as they
// are shown; the tag runs from it to 8 (Exif 2.32 §4.6.4, tag 0x0112).
const orientUpright = 1

// fileOrientation is the orientation tag of a JPEG's or PNG's Exif block,
// upright when the file has none or it cannot be read.
func fileOrientation(data []byte, ext string) int {
	if ext == extJPEG {
		return tiffOrientation(jpegExif(data))
	}
	return tiffOrientation(pngExif(data))
}

// jpegExif is the TIFF block of a JPEG's Exif segment, nil when there is
// none before the scan starts.
func jpegExif(data []byte) []byte {
	const header = "Exif\x00\x00"
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return nil
		}
		marker := data[i+1]
		// A fill byte before a marker, and the markers without a length.
		if marker == 0xff {
			i++
			continue
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			i += 2
			continue
		}
		if marker == 0xda || marker == 0xd9 {
			return nil
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		if n < 2 || i+2+n > len(data) {
			return nil
		}
		seg := data[i+4 : i+2+n]
		if marker == 0xe1 && len(seg) > len(header) && string(seg[:len(header)]) == header {
			return seg[len(header):]
		}
		i += 2 + n
	}
	return nil
}

// pngExif is the TIFF block of a PNG's eXIf chunk, nil when there is none.
func pngExif(data []byte) []byte {
	for i := 8; i+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[i:]))
		if n < 0 || i+12+n > len(data) {
			return nil
		}
		if string(data[i+4:i+8]) == "eXIf" {
			return data[i+8 : i+8+n]
		}
		i += 12 + n
	}
	return nil
}

// tiffOrientation is the orientation tag of a TIFF block's first directory,
// upright when the block is not one or does not carry the tag.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return orientUpright
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return orientUpright
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return orientUpright
	}
	dir := int64(bo.Uint32(tiff[4:]))
	if dir+2 > int64(len(tiff)) {
		return orientUpright
	}
	entries := int64(bo.Uint16(tiff[dir:]))
	for k := int64(0); k < entries; k++ {
		e := dir + 2 + 12*k
		if e+12 > int64(len(tiff)) {
			return orientUpright
		}
		// The tag, as one SHORT held in the entry itself.
		if bo.Uint16(tiff[e:]) != 0x0112 || bo.Uint16(tiff[e+2:]) != 3 || bo.Uint32(tiff[e+4:]) != 1 {
			continue
		}
		if o := int(bo.Uint16(tiff[e+8:])); o >= 1 && o <= 8 {
			return o
		}
		return orientUpright
	}
	return orientUpright
}

// upright is src's pixels as the orientation tag o says they are shown.
func upright(src image.Image, o int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	// One format to index, whatever the decoder returned.
	in := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(in, in.Bounds(), src, b.Min, draw.Src)
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2: // mirrored left to right
				sx, sy = w-1-x, y
			case 3: // turned half round
				sx, sy = w-1-x, h-1-y
			case 4: // mirrored top to bottom
				sx, sy = x, h-1-y
			case 5: // mirrored over the diagonal from the top left
				sx, sy = y, x
			case 6: // a quarter turn clockwise shows it
				sx, sy = y, h-1-x
			case 7: // mirrored over the diagonal from the top right
				sx, sy = w-1-y, h-1-x
			case 8: // a quarter turn anticlockwise shows it
				sx, sy = w-1-y, x
			default:
				sx, sy = x, y
			}
			copy(out.Pix[out.PixOffset(x, y):out.PixOffset(x, y)+4], in.Pix[in.PixOffset(sx, sy):in.PixOffset(sx, sy)+4])
		}
	}
	return out
}

// addUpload is the person adding an image file in the panel, as a revision
// of its own (ADR-0385 §SD1): fileName is the file's name, kept for the
// panel alone. Like the person's removal it waits for no proposal: one that
// is waiting is decided first. A file that is neither format, one past the
// pixel ceiling and one past either budget are refused and change nothing.
func (inst *artefact) addUpload(data []byte, fileName string) (name string, n int, err error) {
	blocked := func() error {
		if inst.proposal != nil {
			return eb.Build().Errorf("a proposed change is waiting: accept or reject it first")
		}
		if set := inst.headImagesLocked(); len(set) >= inst.store.limits.count {
			return errImageBudget{what: "names", used: int64(len(set) + 1), limit: int64(inst.store.limits.count)}
		}
		return nil
	}
	inst.mu.Lock()
	err = blocked()
	inst.mu.Unlock()
	if err != nil {
		return
	}
	kept, ext, err := reencodeUpload(data, inst.store.limits.pixels)
	if err != nil {
		return
	}
	hash, w, h, err := inst.store.put(kept)
	if err != nil {
		return
	}
	// Sealed and not yet named by a revision: kept from a collection that
	// a revert in between runs.
	unpin := inst.store.pin(hash)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if err = blocked(); err != nil {
		// Refused after all: what was sealed for it goes.
		unpin()
		inst.gcLocked()
		return "", 0, err
	}
	defer unpin()
	set := inst.headImagesLocked()
	name, _ = imageName("", set, stemUpload, ext)
	next := append(append([]artImage(nil), set...), artImage{name: name, hash: hash, w: w, h: h, bytes: int64(len(kept)),
		source: imageSourceUpload, root: hash, uploaded: true, fileName: fileName})
	_, text := inst.headLocked()
	n = inst.nextLocked()
	inst.revs = append(inst.revs, artRevision{n: n, text: text, source: revSourcePerson, atMs: time.Now().UnixMilli(),
		images: next, ownImages: true, imageNote: "+ " + name})
	inst.gcLocked()
	return
}
