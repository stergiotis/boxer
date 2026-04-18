//go:build llm_generated_gemini3pro

package stubber

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	cmd := &cli.Command{
		Name:  "stub",
		Usage: "Filter private elements from Go packages",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "dir",
				Value: ".",
				Usage: "Root directory",
			},
			&cli.StringFlag{
				Name:     "outputBaseDir",
				Value:    "",
				Usage:    "Output directory",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "excludeFilenameRegex",
				Value: "",
			},
			&cli.StringFlag{
				Name:  "excludePathRegex",
				Value: "",
			},
			&cli.StringFlag{
				Name:  "buildTag",
				Value: "",
				Usage: "Build tag to merge into //go:build directives of generated files",
			},
			&cli.BoolFlag{
				Name:  "deletePrivate",
				Value: false,
				Usage: "Delete private top-level funcs, types and vars/consts (including their doc comments) unless referenced from surviving public signatures, type definitions or var/const initializers",
			},
		},
		Action: func(c *cli.Context) error {
			pattern := "./..."
			outputBaseDir := filepath.Clean(c.String("outputBaseDir"))

			inst := &TreeProcessor{Filter: NewGoFilter(c.String("buildTag"), c.Bool("deletePrivate"))}

			// Use os.DirFS for reading current directory
			srcFS := os.DirFS(c.String("dir"))

			// Writer function saves to OS
			writerFn := func(relPath string) (w io.WriteCloser, err error) {
				fullPath := filepath.Join(outputBaseDir, relPath)
				err = os.MkdirAll(filepath.Dir(fullPath), 0755)
				if err != nil {
					return
				}
				w, err = os.OpenFile(fullPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
				return
			}
			var excludeFilenameRegex, excludePathRegex *regexp.Regexp
			excludeFilenameRegexStr := c.String("excludeFilenameRegex")
			if excludeFilenameRegexStr != "" {
				var err error
				excludeFilenameRegex, err = regexp.Compile(excludeFilenameRegexStr)
				if err != nil {
					return eh.Errorf("unable to compile excludeFilenameRegex: %w", err)
				}
			}
			excludePathRegexStr := c.String("excludePathRegex")
			if excludePathRegexStr != "" {
				var err error
				excludePathRegex, err = regexp.Compile(excludePathRegexStr)
				if err != nil {
					return eh.Errorf("unable to compile excludePathRegex: %w", err)
				}
			}

			if err := inst.ProcessTree(context.Background(), srcFS, pattern, excludeFilenameRegex, excludePathRegex, writerFn, func(fpath string) (process bool) {
				return !strings.HasPrefix(fpath, outputBaseDir)
			}, nil); err != nil {
				return eh.Errorf("process tree failed: %w", err)
			}
			return nil
		},
	}
	return cmd
}
