//go:build llm_generated_gemini3pro

package llmuse

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "prompt",
		Usage: "Creates a prompt for an LLM from a stubbed go package hierarchy",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "inputDir",
				Value: ".",
			},
		},
		Action: func(context *cli.Context) error {
			_, err := fmt.Println("Below is the public API surface of the library. Function bodies are stubbed (bodies replaced with `panic(\"stub\")`) -- ignore this, it is an artifact of the export process. Your job is to write code that consumes this API.")
			if err != nil {
				return err
			}
			baseDir := context.String("inputDir")
			l := os.DirFS(baseDir)
			err = fs.WalkDir(l, ".", func(path string, d fs.DirEntry, err error) error {
				if !d.IsDir() && strings.HasSuffix(path, ".go") {
					content, e := fs.ReadFile(l, path)
					if e != nil {
						return eb.Build().Str("path", path).Errorf("unable to read file: %w", e)
					}
					_, e = fmt.Printf("\n--- FILE: %s ---\n```go\n", path)
					if e != nil {
						return e
					}
					_, e = fmt.Println(unsafeperf.UnsafeBytesToString(content))
					if e != nil {
						return e
					}
					_, e = fmt.Print("\n```\n")
					if e != nil {
						return e
					}
				}
				return nil
			})
			return err
		},
	}
}
