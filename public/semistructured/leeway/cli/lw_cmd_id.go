package cli

import (
	"os"
	"slices"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/urfave/cli/v2"
)

func NewCliCommandId() *cli.Command {
	var universal *cli2.UniversalCliFormatter
	{
		var err error
		universal, err = cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
		if err != nil {
			log.Panic().Err(err).Msg("unable to create universal formatter")
		}
	}
	universalFlags := universal.ToCliFlags()
	return &cli.Command{
		Name: "id",
		Subcommands: []*cli.Command{
			{
				Name: "tagvalue",
				Subcommands: []*cli.Command{
					{
						Name: "leadingzero",
						Flags: slices.Concat([]cli.Flag{
							&cli.UintFlag{
								Name:  "tagWidth",
								Value: 3,
								Action: func(context *cli.Context, u uint) error {
									if u < 2 || u > fibonacci.Uint32TagValueTagWidth {
										return eb.Build().Uint64("maxTagWidth", fibonacci.Uint32TagValueTagWidth).Errorf("tagWidth is out of range")
									}
									return nil
								},
							},
							&cli.UintFlag{
								Name:  "leadingZeros",
								Value: 1,
							},
						}, universalFlags),
						Action: func(context *cli.Context) error {
							tagWidth := context.Uint("tagWidth")
							leadingZeros := context.Uint("leadingZeros")
							if leadingZeros > tagWidth-2 {
								return eh.Errorf("leading zeros must be smaller or equal to tagWidth-2")
							}
							_, err := os.Stdout.WriteString("TagValue\tLeadingZeros\n")
							if err != nil {
								return err
							}
							var r struct {
								TagValue     identifier.TagValue
								LeadingZeros uint8
								IdTag        identifier.IdTag
								IdTagHex     string
								IdTagBin     string
							}
							for tv, u := range fibonacci.IterateTagValuesWithGivenMinNumberOfLeadingZeros(uint8(tagWidth), uint8(leadingZeros)) {
								idTag := tv.GetTag()
								r.TagValue = tv
								r.IdTag = idTag
								r.LeadingZeros = u
								r.IdTagBin = "0b" + strconv.FormatUint(uint64(idTag), 2)
								r.IdTagHex = "0x" + strconv.FormatUint(uint64(idTag), 16)
								err = universal.FormatValue(context, r)
								if err != nil {
									return err
								}
							}
							return nil
						},
					},
				},
			},
		},
	}
}
