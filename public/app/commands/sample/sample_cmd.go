package sample

import (
	"math/rand/v2"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/sample"
	cli3 "github.com/stergiotis/boxer/public/semistructured/leeway/cli"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/urfave/cli/v2"
)

func getNumber(context *cli.Context, randFunc func(context *cli.Context) *rand.Rand) (n uint64) {
	if context.Bool("random") {
		return randFunc(context).Uint64()
	}
	return context.Uint64("n")
}

func NewCliCommand() *cli.Command {
	rndFlags, rndFunc := cli3.BuildRndFlag()
	universal, err := cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create universal formatter")
	}
	flags := slices.Concat([]cli.Flag{
		&cli.Uint64Flag{
			Name:  "n",
			Value: 0,
		},
		&cli.BoolFlag{
			Name: "random",
		},
	}, rndFlags, universal.ToCliFlags())
	return &cli.Command{
		Name: "sample",
		Subcommands: []*cli.Command{
			{
				Name: "canonicaltype",
				Subcommands: []*cli.Command{
					{
						Name:  "machineNumeric",
						Flags: flags,
						Action: func(context *cli.Context) error {
							return universal.FormatValue(context, sample.GenerateSampleMachineNumericType(getNumber(context, rndFunc)))
						},
					},
					{
						Name:  "string",
						Flags: flags,
						Action: func(context *cli.Context) error {
							return universal.FormatValue(context, sample.GenerateSampleStringType(getNumber(context, rndFunc)))
						},
					},
					{
						Name:  "temporal",
						Flags: flags,
						Action: func(context *cli.Context) error {
							return universal.FormatValue(context, sample.GenerateSampleTemporalType(getNumber(context, rndFunc)))
						},
					},
				},
			},
			{
				Name: "table",
				Flags: slices.Concat(flags, []cli.Flag{
					&cli.BoolFlag{
						Name:  "clickHouseCompatible",
						Value: false,
					},
				}),
				Action: func(context *cli.Context) error {
					var e error
					var acceptCanonicalType func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string)
					var acceptEncodingAspect func(hint encodingaspects.AspectE) (ok bool, msg string)
					if context.Bool("clickHouseCompatible") {
						tech := clickhouse.NewTechnologySpecificCodeGenerator()
						acceptCanonicalType = func(ct canonicaltypes.PrimitiveAstNodeI) (ok bool, msg string) {
							ok, msg = tech.CheckTypeCompatibility(ct)
							if !ok {
								log.Debug().Stringer("canonicalType", ct).Str("msg", msg).Str("technology", tech.GetTechnology().Name).Msg("rejecting canonical type not implemented in code generator")
							}
							return
						}
						acceptEncodingAspect = ddl.EncodingAspectFilterFuncFromTechnology(tech, common.ImplementationStatusPartial)
					}
					rnd := rndFunc(context)
					var dto common.TableDescDto
					dto, e = common.GenerateSampleTableDescDto(rnd, acceptCanonicalType, acceptEncodingAspect)
					if e != nil {
						return e
					}
					return universal.FormatValue(context, dto)
				},
			},
		},
	}
}
