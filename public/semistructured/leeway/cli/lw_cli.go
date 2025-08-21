package cli

import (
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/urfave/cli/v2"
)

func BuildRndFlag() (flags []cli.Flag, f func(context *cli.Context) *rand.Rand) {
	flags = []cli.Flag{
		&cli.Uint64Flag{
			Name: "seed1",
		},
		&cli.Uint64Flag{
			Name: "seed2",
		},
	}
	f = func(context *cli.Context) *rand.Rand {
		var seed1, seed2 uint64
		if context.IsSet("seed1") {
			seed1 = context.Uint64("seed1")
		} else {
			seed1 = rand.Uint64()
		}
		if context.IsSet("seed2") {
			seed2 = context.Uint64("seed2")
		} else {
			seed2 = rand.Uint64()
		}
		return rand.New(rand.NewPCG(seed1, seed2))
	}
	return
}

type aspectI interface {
	fmt.Stringer
	Value() uint8
}

func newAspectCliCommands[E aspectI](name string, allAspects []E, f *cli2.UniversalCliFormatter) []*cli.Command {
	return []*cli.Command{
		{
			Name: name,
			Subcommands: []*cli.Command{
				{
					Name:  "list",
					Flags: f.ToCliFlags(),
					Action: func(context *cli.Context) error {
						strs := make([]string, 0, len(allAspects))
						values := make([]uint8, 0, len(allAspects))
						for _, a := range allAspects {
							strs = append(strs, a.String())
							values = append(values, a.Value())
						}
						return f.FormatValue(context, struct {
							Names  []string
							Values []uint8
						}{Names: strs, Values: values})
					},
				},
			},
		},
	}
}

func NewCliCommand() *cli.Command {
	f, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create cli universal formatter")
	}
	return &cli.Command{
		Name: "leeway",
		Subcommands: slices.Concat([]*cli.Command{
			NewCliCommandCanonicalTypes(),
		},
			newAspectCliCommands("encodinghints", encodingaspects.AllAspects, f),
			newAspectCliCommands("useaspects", useaspects.AllAspects, f),
			newAspectCliCommands("valuesemantics", valueaspects.AllAspects, f),
			[]*cli.Command{
				NewCliCommandDdl(),
				NewCliCommandDml(),
			},
		),
	}
}
