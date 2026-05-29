package cli

import (
	"fmt"
	"os"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	arrow2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/arrow"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/golang"
	"github.com/urfave/cli/v2"
)

func NewCliCommandIr() *cli.Command {
	var universal *cli2.UniversalCliFormatter
	{
		var err error
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
	return &cli.Command{
		Name: "ir",
		Subcommands: []*cli.Command{
			{
				Name: "load",
				Flags: slices.Concat([]cli.Flag{
					&cli.StringFlag{
						Name:  "technology",
						Value: techIds[0],
						Usage: fmt.Sprintf("possible values: %q", techIds),
					},
				}, universalFlags),
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
					var table common.TableDesc
					err = table.LoadFrom(&dto)
					if err != nil {
						return eh.Errorf("unable to load table from dto: %w", err)
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
					ir := common.NewIntermediateTableRepresentation()
					err = ir.LoadFromTable(&table, tech)
					if err != nil {
						return eh.Errorf("unable to load table into intermediate representation: %w", err)
					}

					{
						allocator := memory.DefaultAllocator
						entity := common.NewInEntitySystemTableColumns(allocator, 128)

						err = common.PopulateSchemaTable(entity, ir, table.DictionaryEntry.Name, table.DictionaryEntry.Comment)
						if err != nil {
							return eh.Errorf("unable to populate schema table: %w", err)
						}

						var records []arrow.RecordBatch
						records, err = entity.TransferRecords(nil)
						if err != nil {
							return eh.Errorf("unable to transfer records: %w", err)
						}

						schema := entity.GetSchema()
						var w *ipc.FileWriter
						w, err = ipc.NewFileWriter(os.Stdout, ipc.WithZstd(), ipc.WithAllocator(allocator), ipc.WithSchema(schema))
						if w != nil {
							defer w.Close()
						}
						if err != nil {
							return eh.Errorf("unable to create file writer: %w", err)
						}
						for _, rec := range records {
							err = w.Write(rec)
							if err != nil {
								return eh.Errorf("unable to write record batch: %w", err)
							}
						}
					}
					/*err = universal.FormatValue(context, ir)
					if err != nil {
						return eh.Errorf("unable to format output: %w", err)
					}*/
					return nil
				},
			},
		},
	}
}
