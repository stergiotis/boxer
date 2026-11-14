// Package markdown is the CLI in front of the markdown document facts: read
// Obsidian-flavoured markdown files into `boxer.facts` as document, heading,
// code-block, link, emphasis, tag and frontmatter rows (the mddocfacts store),
// or show what would be extracted without a database. It also holds the
// windowless steps of a sealed notebook (ADR-0371): collect, check, buffer.
package markdown

import (
	cli "github.com/urfave/cli/v3"
)

// NewCliCommand is the `markdown` command group.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "markdown",
		Usage: "markdown documents as facts: ingest a vault, or inspect an extraction; sealed notebooks",
		Commands: []*cli.Command{
			newIngestCommand(),
			newExtractCommand(),
			newNotebookCommand(),
		},
	}
}
