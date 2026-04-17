//go:build llm_generated_gemini3pro
package stubber

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type TreeProcessor struct {
	Filter *GoFilter
}

// ProcessTree walks the srcFS matching the Go package pattern and writes filtered files to writerFn.
// Pattern examples: "./...", "pkg/foo", ".".
func (tp *TreeProcessor) ProcessTree(ctx context.Context, srcFS fs.FS, pattern string, excludeFilename *regexp.Regexp, excludePath *regexp.Regexp, writerFn func(path string) (w io.WriteCloser, err error), shouldProcessDir func(fpath string) (process bool), shouldProcessFile func(fpath string) (process bool)) error {
	// 1. Determine base path and recursive mode
	basePath := "."
	recursive := false

	if pattern == "./..." {
		recursive = true
	} else if strings.HasSuffix(pattern, "/...") {
		basePath = strings.TrimSuffix(pattern, "/...")
		recursive = true
	} else {
		basePath = pattern
	}

	// 2. Walk the filesystem
	return fs.WalkDir(srcFS, basePath, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip directories that shouldn't be recursed if not in recursive mode
		if d.IsDir() {
			if !recursive && fpath != basePath && fpath != "." {
				return fs.SkipDir
			}
			// Standard Go ignore rules: .git, _vendor, etc.
			if strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
				if d.Name() != "." { // Allow root
					return fs.SkipDir
				}
			}
			return nil
		}

		// Only process .go files, ignore tests
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		if (excludeFilename != nil && excludeFilename.MatchString(d.Name())) ||
			(excludePath != nil && excludePath.MatchString(fpath)) {
			log.Debug().Str("path",fpath).Str("name",d.Name()).Msg("excluding file")
			return nil
		}

		if d.IsDir() {
			if shouldProcessDir != nil && !shouldProcessDir(fpath) {
				return fs.SkipDir
			}
		} else {
			if shouldProcessFile != nil && !shouldProcessFile(fpath) {
				return nil
			}
		}
		log.Info().Str("path", fpath).Msg("processing")

		// Process file
		fileIn, err := srcFS.Open(fpath)
		if err != nil {
			return eb.Build().Str("path", fpath).Errorf("failed to open input file: %w", err)
		}
		defer fileIn.Close()

		var buf bytes.Buffer
		if err := tp.Filter.Process(ctx, fpath, fileIn, &buf); err != nil {
			return eb.Build().Str("fpath", fpath).Errorf("failed to process file: %w", err)
		}

		var w io.WriteCloser
		w, err = writerFn(fpath)
		if err != nil {
			return eb.Build().Str("path", fpath).Errorf("failed to write output: %w", err)
		}
		defer w.Close()
		_, err = buf.WriteTo(w)
		if err != nil {
			return eh.Errorf("unable to use writer: %w", err)
		}

		return nil
	})
}
