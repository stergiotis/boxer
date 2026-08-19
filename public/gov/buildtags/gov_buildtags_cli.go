package buildtags

import (
	"fmt"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "buildtags",
		Usage:     "verify ./tags against the build-tag contract boxer publishes via its module pin",
		ArgsUsage: "   (no arguments; reads --file)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "file",
				Value: "tags",
				Usage: "path to the repository's tags file",
			},
			&cli.BoolFlag{
				Name:  "print-env",
				Value: false,
				Usage: "print the derived GOFLAGS assignment instead of checking",
			},
			&cli.BoolFlag{
				Name:  "list",
				Value: false,
				Usage: "print the required, optional and retired sets instead of checking",
			},
		},
		Action: buildtagsAction,
	}
}

func buildtagsAction(ctx *cli.Context) (err error) {
	file := ctx.String("file")

	// --list reads nothing: it prints the contract this binary publishes, and
	// the caller most likely to ask is a repository that has no tags file yet.
	// That became the normal case with ADR-0199 — the required set is empty, so
	// a consumer needs no tags file at all, and answering "what does boxer
	// require?" with "open tags: no such file or directory" is the wrong
	// answer to the right question.
	if ctx.Bool("list") {
		printContract()
		return
	}

	var raw []byte
	raw, err = os.ReadFile(file)
	if err != nil {
		err = eb.Build().Str("file", file).Errorf("read tags file: %w", err)
		return
	}
	tags := ParseTags(string(raw))

	if ctx.Bool("print-env") {
		fmt.Println(GoFlags(tags))
		return
	}

	var n uint32
	for f := range Check(tags) {
		fmt.Fprintf(os.Stdout, "%s:  %s\n", file, f.Message())
		n++
	}
	if n > 0 {
		err = eb.Build().
			Str("file", file).
			Uint32("findings", n).
			Errorf("tags file violates the build-tag contract")
		return
	}
	fmt.Fprintf(os.Stdout, "%s: ok (%d tags, none retired)\n", file, len(tags))
	return
}

func printContract() {
	fmt.Println("required (a consumer must set these):")
	if len(RequiredTags) == 0 {
		fmt.Println("  (none — boxer compiles without build tags since ADR-0199)")
	}
	for _, t := range RequiredTags {
		fmt.Printf("  %s\n", t)
	}
	fmt.Println("optional (recognised opt-ins):")
	for _, t := range OptionalTags {
		fmt.Printf("  %s\n", t)
	}
	fmt.Println("retired (must not be present):")
	for _, r := range RetiredTags {
		fmt.Printf("  %-24s %s  %s\n", r.Pattern, r.Adr, r.Retired)
	}
}
