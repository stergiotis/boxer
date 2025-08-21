package cli

import (
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	ddl2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	arrow2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/urfave/cli/v2"
)

func NewCliCommandDdl() *cli.Command {
	rndFlags, rndFunc := BuildRndFlag()
	marshaller, err := common.NewTableMarshaller()
	if err != nil {
		log.Panic().Err(err).Msg("unable to create table marshaller")
	}
	namingStyleFlag, namingStyleFunc := cli2.BuildEnumStringFlag(naming.AllNamingStyles, naming.DefaultNamingStyle, "namingStyle")
	var universal *cli2.UniversalCliFormatter
	{
		universal, err = cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
		if err != nil {
			log.Panic().Err(err).Msg("unable to create universal formatter")
		}
	}
	universalFlags := universal.ToCliFlags()
	techs := []common.TechnologySpecificGeneratorI{
		clickhouse.NewTechnologySpecificCodeGenerator(),
		arrow2.NewTechnologySpecificCodeGenerator(),
		golang.NewTechnologySpecificCodeGenerator(),
	}
	techIds := make([]string, 0, len(techs))
	for _, t := range techs {
		techIds = append(techIds, t.GetTechnology().Id)
	}
	tableRowConfigFlag, tableRowConfigGetter := cli2.BuildEnumStringFlag(common.AllTableRowConfigs, common.TableRowConfigMultiAttributesPerRow, "tableRowConfig")
	return &cli.Command{
		Name: "ddl",
		Subcommands: []*cli.Command{
			{
				Name: "table",
				Subcommands: []*cli.Command{
					{
						Name: "generate",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "separator",
								Value: ":",
							},
							&cli.StringFlag{
								Name:  "technology",
								Value: techIds[0],
								Usage: fmt.Sprintf("possible values: %q", techIds),
							},
							tableRowConfigFlag,
						},
						Action: func(context *cli.Context) error {
							var dto common.TableDescDto
							err = marshaller.DecodeDtoCbor(os.Stdin, &dto)
							if err != nil {
								return eh.Errorf("unable to decode table description dto encoded in CBOR: %w", err)
							}
							var tech common.TechnologySpecificGeneratorI
							{
								techIdx := slices.Index(techIds, context.String("technology"))
								if techIdx >= 0 {
									tech = techs[techIdx]
								}
							}

							if tech == nil {
								return eb.Build().Str("given", context.String("technology")).Strs("possible", techIds).Errorf("unable to resolve technology")
							}
							var conv common.NamingConventionI
							conv, err = ddl2.NewHumanReadableNamingConvention(context.String("separator"))
							if err != nil {
								return eh.Errorf("unable to create human readable name convention object: %w", err)
							}
							b := &strings.Builder{}
							tech.SetCodeBuilder(b)

							var table common.TableDesc
							err = table.LoadFrom(&dto)
							if err != nil {
								return eh.Errorf("unable to load table from dto: %w", err)
							}
							generator := ddl2.NewGeneratorDriver()
							ir := common.NewIntermediateTableRepresentation()
							err = ir.LoadFromTable(&table, tech)
							if err != nil {
								return eh.Errorf("unable to create intermediate table representation: %w", err)
							}
							tableRowConfig := tableRowConfigGetter(context)

							tech.ResetCodeBuilder()
							err = generator.GenerateColumnsCode(ir.IterateColumnProps(), tableRowConfig, conv, tech, func(hint encodingaspects.AspectE) (ok bool, msg string) {
								return true, ""
							})
							if err != nil {
								return eh.Errorf("unable to compose columns from table: %w", err)
							}
							_, err = os.Stdout.WriteString(b.String())
							if err != nil {
								return eh.Errorf("unable to write to stdout: %w", err)
							}
							return nil
						},
					},
					{
						Name:  "normalize",
						Flags: slices.Concat([]cli.Flag{namingStyleFlag}, universalFlags),
						Action: func(context *cli.Context) error {
							namingStyle := namingStyleFunc(context)
							normalizer := common.NewTableNormalizer(namingStyle)

							var table common.TableDesc
							err = marshaller.DecodeTableCbor(os.Stdin, &table)
							if err != nil {
								return eh.Errorf("unable to decode table description encoded in CBOR: %w", err)
							}

							var namesChanged, reorderedPlain, reorderedTagged bool
							namesChanged, reorderedPlain, reorderedTagged, err = normalizer.Normalize(&table)
							if err != nil {
								return eh.Errorf("unable to normalize table: %w", err)
							}
							log.Info().Bool("namesChanged", namesChanged).Bool("reorderedPlain", reorderedPlain).Bool("reorderedTagged", reorderedTagged).Msg("normalized table")
							var dto common.TableDescDto
							err = table.LoadTo(&dto)
							if err != nil {
								return eh.Errorf("unable to convert to dto: %w", err)
							}
							return universal.FormatValue(context, dto)
						},
					},
					{
						Name: "scramble",
						Flags: slices.Concat([]cli.Flag{
							namingStyleFlag,
						},
							rndFlags,
							universalFlags),
						Action: func(context *cli.Context) error {
							namingStyle := namingStyleFunc(context)
							normalizer := common.NewTableNormalizer(namingStyle)

							var table common.TableDesc
							err = marshaller.DecodeTableCbor(os.Stdin, &table)
							if err != nil {
								return eh.Errorf("unable to decode table description encoded in CBOR: %w", err)
							}

							rnd := rand.New(rndFunc(context))
							normalizer.Scramble(&table, rnd)
							var dto common.TableDescDto
							err = table.LoadTo(&dto)
							if err != nil {
								return eh.Errorf("unable to convert to dto: %w", err)
							}
							return universal.FormatValue(context, dto)
						},
					},
					{
						Name: "validate",
						Action: func(context *cli.Context) error {
							var table common.TableDesc
							err = marshaller.DecodeTableCbor(os.Stdin, &table)
							if err != nil {
								return eh.Errorf("unable to decode table description encoded in CBOR: %w", err)
							}
							validator := common.NewTableValidator()
							err = validator.ValidateTable(&table)
							if err != nil {
								return eh.Errorf("validation failed: %w", err)
							}
							log.Info().Msg("table is valid")
							return nil
						},
					},
				},
			},
		},
	}
}
