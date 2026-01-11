//go:build llm_generated_gemini3pro

package filename

import (
	"context"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type Renamer struct {
	RenameOp RenameOpE
}

type RenameOpE uint8

const (
	RenameOpDryRun RenameOpE = 0
	RenameOpRename RenameOpE = 1
)

func (inst *Renamer) Run(ctx context.Context, root string) (err error) {
	for path, errIter := range inst.walkGoFiles(root) {
		if errIter != nil {
			err = eh.Errorf("error during walk: %w", errIter)
			return
		}

		err = inst.processPath(path)
		if err != nil {
			err = eb.Build().
				Str("path", path).
				Errorf("failed to process file: %w", err)
			return
		}
	}

	return
}

func (inst *Renamer) walkGoFiles(root string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			isGo := strings.HasSuffix(d.Name(), ".go")
			if isGo {
				if !yield(path, nil) {
					return fs.SkipAll
				}
			} else {
				// Ignore non-go files
			}
			return nil
		})

		if err != nil {
			log.Warn().Err(err).Msg("error while processing path, skipping")
			err = nil
		}
	}
}

func (inst *Renamer) processPath(oldPath string) (err error) {
	var dir string
	var filename string
	var newFilename string
	var newPath string

	dir, filename = filepath.Split(oldPath)

	prefix, suffix, dot := strings.Cut(filename, ".")
	if dot {
		suffix = "." + suffix
	}

	var prefix2 naming.StylableName
	prefix2, err = naming.MakeStylableName(prefix)
	if err != nil {
		err = eb.Build().Str("filename", filename).Errorf("found non-convertable file name")
		return
	}
	newFilename = prefix2.Convert(naming.LowerSnakeCase).String() + suffix

	if filename == newFilename {
		return
	}

	if strings.Contains(suffix, ".out.") || strings.Contains(suffix, ".gen.") {
		log.Info().Str("old", filename).Str("new", newFilename).Msg("skipping generated file")
		return
	}

	newPath = filepath.Join(dir, newFilename)
	switch inst.RenameOp {
	case RenameOpRename:
		log.Info().
			Str("old", filename).
			Str("new", newFilename).
			Msg("renaming file")
		err = os.Rename(oldPath, newPath)
		if err != nil {
			err = eh.Errorf("os rename failed: %w", err)
			return
		}
		break
	case RenameOpDryRun:
		log.Info().
			Str("old", filename).
			Str("new", newFilename).
			Msg("found file to rename (dry run)")
	}

	return
}

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "filenames",
		Usage: "Renames all .go files in a directory tree to snake_case",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "print changes without applying them",
			},
			&cli.StringFlag{
				Name:  "root",
				Usage: "root directory",
				Value: ".",
			},
		},
		// Convention: Context is mandatory.
		Action: func(c *cli.Context) error {
			renamer := &Renamer{
				RenameOp: RenameOpRename,
			}
			if c.Bool("dry-run") {
				renamer.RenameOp = RenameOpDryRun
			}

			return renamer.Run(context.Background(), c.String("root"))
		},
	}
}
