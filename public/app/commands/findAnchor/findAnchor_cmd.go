package findAnchor

import (
	"bufio"
	"errors"
	"io"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/fec/anchor"
	"github.com/urfave/cli/v2"
)

var (
	NAnchorBytes = env.NewInt(env.Spec{
		Name:        "PEBBLE_N_ANCHOR_BYTES",
		Default:     "3",
		Description: "anchor length in bytes for the SkipPastAnchor* fec routines",
		Category:    env.CategoryE("anchor"),
		CliFlagName: "nAnchorBytes",
	})

	MaxHammingDistPerByteIncl = env.NewInt(env.Spec{
		Name:        "PEBBLE_MAX_HAMMING_DIST_PER_BYTE_INCL",
		Default:     "3",
		Description: "max Hamming distance per byte (inclusive) for anchor matching",
		Category:    env.CategoryE("anchor"),
		CliFlagName: "maxHammingDistPerByteIncl",
	})
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "findAnchor",
		Flags: []cli.Flag{
			NAnchorBytes.AsCliFlag(),
			MaxHammingDistPerByteIncl.AsCliFlag(),
		},
		Action: func(ctx *cli.Context) error {
			r := bufio.NewReader(os.Stdin)
			offset := uint64(0)
			nAnchorBytes := int(NAnchorBytes.Get())
			t := uint64(nAnchorBytes)
			maxDist := int(MaxHammingDistPerByteIncl.Get())
			nBytesRead, dist, err := anchor.SkipPastAnchorInitial(r, nAnchorBytes, maxDist)
			for {
				if err != nil {
					if errors.Is(err, io.EOF) {
						return nil
					}
					return err
				}
				offset += nBytesRead - t
				log.Info().Uint64("offset", offset).Int("hammingDist", dist).Msg("found anchor")
				offset += t
				nBytesRead, dist, err = anchor.SkipPastAnchorConsecutive(r, nAnchorBytes, maxDist)
			}
		},
		Usage: "find anchor",
	}
}
