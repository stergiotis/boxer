package fs

import (
	"os"
	"slices"
	"strings"

	"io/fs"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

func NewCommand() *cli.Command {
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
		Name: "fs",
		Flags: slices.Concat([]cli.Flag{
			&cli.StringFlag{
				Name:  "dir",
				Value: ".",
			},
			&cli.StringFlag{
				Name:  "suffix",
				Value: "",
			},
		}, universalFlags),
		Action: func(context *cli.Context) error {
			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return eh.Errorf("unable to create watcher: %w", err)
			}
			dir := context.String("dir")
			suffix := context.String("suffix")
			err = fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
				if d.IsDir() {
					log.Debug().Str("dir", path).Str("suffix", suffix).Msg("watching directory")
					e := watcher.Add(path)
					if e != nil {
						log.Warn().Err(e).Msg("unable to watch directory, skipping")
					}
				}
				return nil
			})
			if err != nil {
				return eh.Errorf("unable to walk director: %w", err)
			}
			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						log.Panic().Str("dir", dir).Msg("unable to watch fs")
						return nil
					}
					if strings.HasSuffix(event.Name, suffix) {
						op := event.Op
						err = universal.FormatValue(context, struct {
							Event     string
							EventName string
							Operation struct {
								Create bool
								Write  bool
								Remove bool
								Rename bool
								Chmod  bool
							}
						}{
							Event:     event.String(),
							EventName: event.Name,
							Operation: struct {
								Create bool
								Write  bool
								Remove bool
								Rename bool
								Chmod  bool
							}{
								Create: op.Has(fsnotify.Create),
								Write:  op.Has(fsnotify.Write),
								Remove: op.Has(fsnotify.Remove),
								Rename: op.Has(fsnotify.Rename),
								Chmod:  op.Has(fsnotify.Chmod),
							},
						})
						if err != nil {
							log.Warn().Err(err).Msg("unable to serialize event, skipping")
							err = nil
						}
					}
				case e, ok := <-watcher.Errors:
					if !ok {
						log.Panic().Str("dir", dir).Msg("unable to watch fs")
						return nil
					}
					log.Warn().Err(e).Msg("fs watch error")
				}
			}
		},
	}
}
