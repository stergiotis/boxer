package markdown

import (
	"encoding/json"
	"os"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// newExtractCommand is `boxer markdown extract`: the ingestor's reading of the
// given files as JSON, one object per file, no database involved. It is the
// way to see what `ingest` would store before storing it.
func newExtractCommand() *cli.Command {
	return &cli.Command{
		Name:      "extract",
		Usage:     "print the structured reading of markdown files as JSON",
		ArgsUsage: "<file | dir>...",
		Description: "Parses each file the way `ingest` does — frontmatter leaves, headings, " +
			"fenced code blocks, links, emphasis, tags — and writes a JSON array of " +
			"{File, Doc} objects to stdout. Directories are walked for .md files; " +
			"dot-directories are skipped.",
		Action: runExtract,
	}
}

func runExtract(c *cli.Context) (err error) {
	if c.NArg() == 0 {
		return eh.Errorf("at least one file or directory is required")
	}
	sources, err := walkSources(c.Args().Slice())
	if err != nil {
		return
	}
	type entry struct {
		File string
		Doc  *mdextract.Document
	}
	out := make([]entry, 0, len(sources))
	for _, s := range sources {
		var src []byte
		src, err = os.ReadFile(s.path)
		if err != nil {
			return eb.Build().Str("path", s.path).Errorf("read: %w", err)
		}
		out = append(out, entry{File: s.name, Doc: mdextract.Extract(src)})
	}
	enc := json.NewEncoder(c.App.Writer)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
