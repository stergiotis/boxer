package tracing

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/containers/co"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommands() []*cli.Command {
	var universal *cli2.UniversalCliFormatter
	{
		var err error
		universal, err = cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
		if err != nil {
			log.Panic().Err(err).Msg("unable to create universal formatter")
		}
	}
	universalFlags := universal.ToCliFlags()
	return []*cli.Command{
		{
			Name: "tracing",
			Subcommands: []*cli.Command{
				{
					Name: "codelocations",
					Flags: slices.Concat(universalFlags, []cli.Flag{
						&cli.BoolFlag{
							Name: "sort",
						},
						&cli.StringFlag{
							Name:  "relativePathBase",
							Value: "",
						},
						&cli.BoolFlag{
							Name: "relativePath",
						},
						&cli.Uint64Flag{
							Name:     "estimatedLOC",
							Value:    10_000,
							Category: "performance",
						},
					}),
					Action: func(context *cli.Context) error {
						estLoc := int(context.Uint64("estimatedLOC"))
						u := NewTraceUtils()
						files := make([]string, 0, estLoc)
						lines := make([]uint64, 0, estLoc)
						dedup := containers.NewHashSet[string](estLoc)
						for file, line := range u.IterateCodeLocations(os.Stdin, dedup) {
							files = append(files, file)
							lines = append(lines, line)
						}
						if context.Bool("sort") {
							co.SortUnstable(len(files),
								func(i, j int) bool {
									o := strings.Compare(files[i], files[j])
									if o == 0 {
										return lines[i] < lines[j]
									}
									return o < 0
								},
								func(i, j int) {
									files[j], files[i] = files[i], files[j]
									lines[j], lines[i] = lines[i], lines[j]
								},
							)
						}
						if context.Bool("relativePath") {
							basePath := context.String("relativePathBase")
							if basePath == "" {
								var err error
								basePath, err = os.Getwd()
								if err != nil {
									return eh.Errorf("unable to determine current work directory")
								}
							}
							for i, p := range files {
								t, err := filepath.Rel(basePath, p)
								if err != nil {
									return eb.Build().Str("path", p).Str("basePath", basePath).Errorf("unable to make path relative to supplied base path")
								}
								files[i] = t
							}
						}
						log.Info().Int("linesOfCode", len(lines)).Msg("code locations")
						return universal.FormatValue(context, struct {
							Files []string
							Lines []uint64
						}{
							Files: files,
							Lines: lines,
						})
					},
				},
			},
		},
	}
}
