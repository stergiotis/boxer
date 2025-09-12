package cli

import (
	"os"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/urfave/cli/v2"
)

func NewCliCommandDml() *cli.Command {
	return &cli.Command{
		Name: "dml",
		Subcommands: []*cli.Command{
			{Name: "table",
				Subcommands: []*cli.Command{
					{
						Name: "generate",
						Subcommands: []*cli.Command{
							{Name: "go",
								Flags: []cli.Flag{
									&cli.StringFlag{
										Name:     "tableName",
										Required: true,
									},
									&cli.StringFlag{
										Name:     "packageName",
										Required: true,
									},
								},
								Action: func(context *cli.Context) error {
									marshaller, err := common.NewTableMarshaller()
									if err != nil {
										return eh.Errorf("unable to create table marshaller: %w", err)
									}
									var dto common.TableDescDto
									err = marshaller.DecodeDtoCbor(os.Stdin, &dto)
									if err != nil {
										return eh.Errorf("unable to decode table description dto encoded in CBOR: %w", err)
									}
									var tblDesc common.TableDesc
									err = tblDesc.LoadFrom(&dto)
									if err != nil {
										return eh.Errorf("unable to load table from dto: %w", err)
									}

									var conv *ddl.HumanReadableNamingConvention
									conv, err = ddl.NewHumanReadableNamingConvention(":")
									chTech := clickhouse.NewTechnologySpecificCodeGenerator()
									driver := dml.NewGoCodeGeneratorDriver(conv, chTech)

									tableRowConfig := common.TableRowConfigMultiAttributesPerRow
									tableName := context.String("tableName")
									packageName := context.String("packageName")
									var wellFormed bool
									var sourceCode []byte
									namingStyle := gocodegen.NewDefaultGoClassNamer()
									sourceCode, wellFormed, err = driver.GenerateGoClasses(packageName, naming.MustBeValidStylableName(tableName), tblDesc, tableRowConfig, namingStyle)
									if err != nil {
										return eh.Errorf("unable to generate go classes: %w", err)
									}
									if !wellFormed {
										log.Warn().Msg("output is not well-formed go code")
									}

									_, err = os.Stdout.Write(sourceCode)
									if err != nil {
										return eh.Errorf("unable to write to stdout: %w", err)
									}
									return nil
								},
							},
						},
					},
				},
			},
		},
	}
}
