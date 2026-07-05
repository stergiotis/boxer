// Package keelsoncodec exposes the keelson Go <-> leeway codec generator
// (ADR-0042) as a boxer subcommand, folding the former standalone keelsoncodec
// main into public/app per the entry-point standard.
//
// For each input DTO `.go` source it writes a sibling `<name>.out.go`.
// --target selects the wrapper around leeway/marshallgen's schema-agnostic core:
//
//   - facts (default): factswrapper.FactsWrapper — vdd-resolved kind ids,
//     dml_cbor pool, ActiveSections / ActiveFields hints, Marshal / Unmarshal,
//     buscodec.CodecI bridge.
//   - anchor: marshallgen.NoOpWrapper — schema-agnostic surface only.
package keelsoncodec

import (
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/factswrapper"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "keelsoncodec",
		Usage:     "generate keelson Go<->leeway codecs from DTO sources (ADR-0042)",
		ArgsUsage: "<dto1.go> [<dto2.go> ...]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "target",
				Value: "facts",
				Usage: "schema family target (facts|anchor)",
			},
		},
		Action: func(c *cli.Context) (err error) {
			target := c.String("target")
			inputs := c.Args().Slice()
			if len(inputs) < 1 {
				return eh.Errorf("at least one input .go DTO source is required (usage: keelsoncodec [--target=facts|anchor] <dto1.go> ...)")
			}
			var failed int
			for _, inputPath := range inputs {
				if !strings.HasSuffix(inputPath, ".go") || strings.HasSuffix(inputPath, ".out.go") {
					log.Error().Str("input", inputPath).Msg("keelsoncodec: input must be a .go file (not .out.go)")
					failed++
					continue
				}
				outputPath := strings.TrimSuffix(inputPath, ".go") + ".out.go"
				var gerr error
				switch target {
				case "", "facts":
					_, gerr = factswrapper.FactsWrapper{}.Generate(inputPath, outputPath)
				case "anchor":
					_, gerr = marshallgen.Generate(inputPath, outputPath, marshallgen.NoOpWrapper{}, marshallgen.EmitOpts{})
				default:
					return eh.Errorf("unknown target %q (want facts|anchor)", target)
				}
				if gerr != nil {
					log.Error().Err(gerr).Str("input", inputPath).Msg("keelsoncodec: generation failed")
					failed++
					continue
				}
				log.Info().Str("output", outputPath).Str("target", target).Msg("keelsoncodec: wrote codec")
			}
			if failed > 0 {
				return eh.Errorf("keelsoncodec: %d of %d input(s) failed", failed, len(inputs))
			}
			return
		},
	}
}
