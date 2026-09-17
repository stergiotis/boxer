// Package viewerfixture prepares and checks the media fixtures
// scripts/ci/viewer_wine_smoke.sh drives the packaged Windows viewer with
// (ADR-0243).
//
// The smoke test encodes one frame per supported codec with ffmpeg, feeds it
// to the viewer over a websocket, and captures what the viewer drew. Two steps
// of that are container arithmetic rather than media work: ffmpeg writes VP9
// and AV1 into IVF but the viewer is fed a bare bitstream, and a capture that
// decoded nothing is a valid BMP of one flat colour, which no exit status
// reports.
package viewerfixture

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// The IVF container: a 32-byte file header signed "DKIF", then per frame a
// 4-byte payload length and an 8-byte timestamp. So the first frame's payload
// begins at 44.
const (
	ivfSignature   = "DKIF"
	ivfFileHeader  = 32
	ivfFrameHeader = 12
	ivfFirstFrame  = ivfFileHeader + ivfFrameHeader
)

// The BMP header carries the offset of the pixel array at byte 10; 54 is the
// smallest header pair (file + BITMAPINFOHEADER) that offset can follow.
const (
	bmpSignature    = "BM"
	bmpMinHeader    = 54
	bmpPixelsOffset = 10
)

// defaultMinDistinctBytes is how many distinct byte values the pixel array has
// to carry before a capture counts as an image. A blank or nearly uniform
// frame is what a viewer that decoded nothing draws.
const defaultMinDistinctBytes = 33

// NewCliCommand returns the `viewer-fixture` subcommand. Mounted under boxer's
// `dev` parent by public/app/main.go.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "viewer-fixture",
		Usage: "prepare and check the imzero2-viewer smoke test's media fixtures (ADR-0243)",
		Subcommands: []*cli.Command{
			newIvfExtractCommand(),
			newBmpCheckCommand(),
		},
	}
}

func newIvfExtractCommand() *cli.Command {
	return &cli.Command{
		Name:      "ivf-extract",
		Usage:     "write the first frame's bare bitstream out of an IVF file",
		ArgsUsage: "IN.ivf OUT",
		Action: func(ctx *cli.Context) (err error) {
			if ctx.NArg() != 2 {
				return cli.Exit("usage: "+ctx.Command.HelpName+" IN.ivf OUT", 2)
			}
			in, out := ctx.Args().Get(0), ctx.Args().Get(1)
			b, err := os.ReadFile(in)
			if err != nil {
				return eb.Build().Str("path", in).Errorf("read the IVF fixture: %w", err)
			}
			frame, err := IvfFirstFrame(b)
			if err != nil {
				return eb.Build().Str("path", in).Errorf("take the first frame: %w", err)
			}
			if err = os.WriteFile(out, frame, 0o644); err != nil {
				return eb.Build().Str("path", out).Errorf("write the bitstream: %w", err)
			}
			return
		},
	}
}

func newBmpCheckCommand() *cli.Command {
	return &cli.Command{
		Name:      "bmp-check",
		Usage:     "fail when a captured BMP is blank or nearly uniform",
		ArgsUsage: "CAPTURE.bmp",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "min-distinct-bytes",
				Value: defaultMinDistinctBytes,
				Usage: "distinct byte values the pixel array must carry",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			if ctx.NArg() != 1 {
				return cli.Exit("usage: "+ctx.Command.HelpName+" CAPTURE.bmp", 2)
			}
			path := ctx.Args().Get(0)
			b, err := os.ReadFile(path)
			if err != nil {
				return eb.Build().Str("path", path).Errorf("read the capture: %w", err)
			}
			distinct, err := BmpDistinctPixelBytes(b)
			if err != nil {
				return eb.Build().Str("path", path).Errorf("inspect the capture: %w", err)
			}
			minimum := ctx.Int("min-distinct-bytes")
			if distinct < minimum {
				fmt.Printf("capture is blank or nearly uniform: %d distinct pixel bytes, need %d\n", distinct, minimum)
				return cli.Exit("", 1)
			}
			return
		},
	}
}

// IvfFirstFrame returns the payload of the first frame of an IVF file.
func IvfFirstFrame(b []byte) (frame []byte, err error) {
	if len(b) < ivfFirstFrame || string(b[:len(ivfSignature)]) != ivfSignature {
		err = eb.Build().Int("len", len(b)).Errorf("not an IVF file")
		return
	}
	n := int(binary.LittleEndian.Uint32(b[ivfFileHeader : ivfFileHeader+4]))
	if n <= 0 || n > len(b)-ivfFirstFrame {
		err = eb.Build().Int("frameLen", n).Int("available", len(b)-ivfFirstFrame).
			Errorf("the first frame's length does not fit the file")
		return
	}
	frame = b[ivfFirstFrame : ivfFirstFrame+n]
	return
}

// BmpDistinctPixelBytes counts the distinct byte values in a BMP's pixel
// array. It is a proxy for "something was drawn", not an image metric: the
// smoke test only has to tell a decoded frame from the flat fill a viewer that
// decoded nothing leaves behind.
func BmpDistinctPixelBytes(b []byte) (distinct int, err error) {
	if len(b) <= bmpMinHeader || string(b[:len(bmpSignature)]) != bmpSignature {
		err = eb.Build().Int("len", len(b)).Errorf("not a BMP file")
		return
	}
	off := int(binary.LittleEndian.Uint32(b[bmpPixelsOffset : bmpPixelsOffset+4]))
	if off < bmpMinHeader || off >= len(b) {
		err = eb.Build().Int("pixelOffset", off).Int("len", len(b)).
			Errorf("the pixel array starts outside the file")
		return
	}
	var seen [256]bool
	for _, v := range b[off:] {
		if !seen[v] {
			seen[v] = true
			distinct++
		}
	}
	return
}
