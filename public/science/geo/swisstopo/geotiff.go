package swisstopo

import (
	"bytes"
	"encoding/binary"

	"golang.org/x/image/tiff/lzw"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// swissALTI3D 2m COG tile constants
const (
	tiffTileWidth  int32 = 128
	tiffTileLength int32 = 128
	pixelWidth     int32 = 500
	pixelHeight    int32 = 500
	samplesPerPx   int32 = 1
	bytesPerSample int32 = 4 // float32
)

// tiffTagID constants for the tags we read
const (
	tagTileOffsets     uint16 = 324
	tagTileByteCounts  uint16 = 325
	tagStripOffsets    uint16 = 273
	tagStripByteCounts uint16 = 279
)

// readSwissALTI3DTile reads a swissALTI3D 2m COG tile and returns the decoded
// 500x500 float32 elevation grid. The returned slice has length 250000 and is
// indexed as pixels[row*500+col].
func readSwissALTI3DTile(path string) (pixels []float32, err error) {
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eh.Errorf("unable to read tile file %s: %w", path, err)
		return
	}

	// validate byte order (must be little-endian "II")
	if len(data) < 8 {
		err = eh.Errorf("file too small: %w", fmt.Errorf("expected at least 8 bytes"))
		return
	}
	if data[0] != 'I' || data[1] != 'I' {
		err = eh.Errorf("unsupported byte order %c%c, expected little-endian II: %w", data[0], data[1], fmt.Errorf("bad byte order"))
		return
	}

	// read IFD offset
	ifdOffset := binary.LittleEndian.Uint32(data[4:8])

	// parse IFD to find tile offsets and byte counts
	var tileOffsets []uint32
	var tileByteCounts []uint32
	tileOffsets, tileByteCounts, err = parseTIFFIFD(data, ifdOffset)
	if err != nil {
		err = eh.Errorf("unable to parse TIFF IFD: %w", err)
		return
	}

	// compute tile grid dimensions
	tilesAcross := (pixelWidth + tiffTileWidth - 1) / tiffTileWidth   // 4
	tilesDown := (pixelHeight + tiffTileLength - 1) / tiffTileLength   // 4
	expectedTiles := tilesAcross * tilesDown

	if int32(len(tileOffsets)) != expectedTiles {
		err = eh.Errorf("expected %d tiles, got %d offsets: %w", expectedTiles, len(tileOffsets), fmt.Errorf("tile count mismatch"))
		return
	}

	pixels = make([]float32, pixelWidth*pixelHeight)

	// decode each internal tile
	var tileIdx int32
	for ty := int32(0); ty < tilesDown; ty++ {
		for tx := int32(0); tx < tilesAcross; tx++ {
			var tilePixels []float32
			tilePixels, err = decodeLZWTile(data, tileOffsets[tileIdx], tileByteCounts[tileIdx], tiffTileWidth, tiffTileLength)
			if err != nil {
				err = eh.Errorf("unable to decode tile %d (tx=%d, ty=%d): %w", tileIdx, tx, ty, err)
				return
			}

			// copy tile pixels into the full image
			{ // blit tile into pixel buffer
				srcRowStart := ty * tiffTileLength
				srcColStart := tx * tiffTileWidth
				for row := int32(0); row < tiffTileLength; row++ {
					dstRow := srcRowStart + row
					if dstRow >= pixelHeight {
						break
					}
					for col := int32(0); col < tiffTileWidth; col++ {
						dstCol := srcColStart + col
						if dstCol >= pixelWidth {
							break
						}
						pixels[dstRow*pixelWidth+dstCol] = tilePixels[row*tiffTileWidth+col]
					}
				}
			}

			tileIdx++
		}
	}

	return
}

// parseTIFFIFD reads the first IFD and extracts tile offsets and byte counts.
func parseTIFFIFD(data []byte, ifdOffset uint32) (tileOffsets []uint32, tileByteCounts []uint32, err error) {
	if int(ifdOffset)+2 > len(data) {
		err = eh.Errorf("IFD offset out of range: %w", fmt.Errorf("offset %d exceeds file size %d", ifdOffset, len(data)))
		return
	}

	numEntries := binary.LittleEndian.Uint16(data[ifdOffset : ifdOffset+2])
	pos := ifdOffset + 2

	for i := uint16(0); i < numEntries; i++ {
		if int(pos)+12 > len(data) {
			err = eh.Errorf("IFD entry %d out of range: %w", i, fmt.Errorf("truncated"))
			return
		}

		tag := binary.LittleEndian.Uint16(data[pos : pos+2])
		fieldType := binary.LittleEndian.Uint16(data[pos+2 : pos+4])
		count := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		valueOffset := binary.LittleEndian.Uint32(data[pos+8 : pos+12])

		switch tag {
		case tagTileOffsets, tagStripOffsets:
			tileOffsets, err = readUint32Array(data, fieldType, count, valueOffset)
			if err != nil {
				err = eh.Errorf("unable to read tile offsets: %w", err)
				return
			}
		case tagTileByteCounts, tagStripByteCounts:
			tileByteCounts, err = readUint32Array(data, fieldType, count, valueOffset)
			if err != nil {
				err = eh.Errorf("unable to read tile byte counts: %w", err)
				return
			}
		}

		pos += 12
	}

	if len(tileOffsets) == 0 {
		err = eh.Errorf("no tile/strip offsets found in IFD: %w", fmt.Errorf("missing tag"))
		return
	}
	if len(tileByteCounts) == 0 {
		err = eh.Errorf("no tile/strip byte counts found in IFD: %w", fmt.Errorf("missing tag"))
		return
	}
	return
}

// readUint32Array reads an array of uint32 values from a TIFF IFD entry.
// Handles both SHORT (type 3) and LONG (type 4) field types.
// In TIFF, if the total value data fits in 4 bytes, it is stored directly
// in the value/offset field; otherwise the field contains an offset into the file.
func readUint32Array(data []byte, fieldType uint16, count uint32, valueOffset uint32) (values []uint32, err error) {
	values = make([]uint32, count)

	switch fieldType {
	case 3: // SHORT (uint16), 2 bytes each
		if count <= 2 {
			// inline: values packed into the 4-byte value/offset field
			var buf [4]byte
			binary.LittleEndian.PutUint32(buf[:], valueOffset)
			for i := uint32(0); i < count; i++ {
				values[i] = uint32(binary.LittleEndian.Uint16(buf[i*2 : i*2+2]))
			}
		} else {
			// stored at file offset
			off := valueOffset
			for i := uint32(0); i < count; i++ {
				if int(off)+2 > len(data) {
					err = eh.Errorf("SHORT array out of range at index %d: %w", i, fmt.Errorf("truncated"))
					return
				}
				values[i] = uint32(binary.LittleEndian.Uint16(data[off : off+2]))
				off += 2
			}
		}
	case 4: // LONG (uint32), 4 bytes each
		if count <= 1 {
			// inline: single value in the value/offset field
			values[0] = valueOffset
		} else {
			// stored at file offset
			off := valueOffset
			for i := uint32(0); i < count; i++ {
				if int(off)+4 > len(data) {
					err = eh.Errorf("LONG array out of range at index %d: %w", i, fmt.Errorf("truncated"))
					return
				}
				values[i] = binary.LittleEndian.Uint32(data[off : off+4])
				off += 4
			}
		}
	default:
		err = eh.Errorf("unsupported field type %d for uint32 array: %w", fieldType, fmt.Errorf("bad type"))
		return
	}
	return
}

// decodeLZWTile decompresses an LZW-compressed TIFF tile with horizontal
// differencing predictor for float32 data.
func decodeLZWTile(data []byte, offset uint32, byteCount uint32, tileW int32, tileH int32) (pixels []float32, err error) {
	if int(offset)+int(byteCount) > len(data) {
		err = eh.Errorf("tile data out of range: offset=%d count=%d filesize=%d: %w", offset, byteCount, len(data), fmt.Errorf("truncated"))
		return
	}

	compressedData := data[offset : offset+byteCount]
	reader := lzw.NewReader(bytes.NewReader(compressedData), lzw.MSB, 8)
	defer func() {
		_ = reader.Close()
	}()

	expectedBytes := tileW * tileH * bytesPerSample * samplesPerPx
	var decoded []byte
	decoded, err = io.ReadAll(reader)
	if err != nil {
		err = eh.Errorf("LZW decompression failed: %w", err)
		return
	}

	if int32(len(decoded)) < expectedBytes {
		err = eh.Errorf("decoded size %d < expected %d bytes: %w", len(decoded), expectedBytes, fmt.Errorf("short read"))
		return
	}

	// swissALTI3D tiles use Predictor=1 (no prediction), so no
	// post-decompression differencing step is needed.

	// interpret as little-endian float32
	pixelCount := tileW * tileH
	pixels = make([]float32, pixelCount)
	for i := int32(0); i < pixelCount; i++ {
		off := i * bytesPerSample
		bits := binary.LittleEndian.Uint32(decoded[off : off+bytesPerSample])
		pixels[i] = math.Float32frombits(bits)
	}

	return
}
