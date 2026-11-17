// Package fontscmd provides the `fonts` subcommand: the font files the
// imzero2 render head would be started with on this host.
package fontscmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/fontresolve"
	cli "github.com/urfave/cli/v3"
)

// faces are the names --face takes and the shell variables --shell writes, in
// output order.
var faces = []struct{ name, variable string }{
	{"main", "MAIN_FONT"},
	{"bold", "BOLD_FONT"},
	{"mono", "MONO_FONT"},
	{"fallback", "FALLBACK_FONT"},
}

// NewCommand builds the `fonts` subcommand.
func NewCommand() *cli.Command {
	return &cli.Command{
		Name:  "fonts",
		Usage: "print the font files the render head gets on this host when its font flags are left empty",
		Description: "The main (Noto Sans), bold, mono (a face covering the box-drawing block) and fallback\n" +
			"(Noto Sans CJK) faces, found through fontconfig. A face it cannot find prints empty and\n" +
			"is reported on stderr.\n\n" +
			"   imzero2 fonts                 # a table\n" +
			"   imzero2 fonts --face main     # one path, for a build that stages the file\n" +
			"   eval \"$(imzero2 fonts --shell)\"  # MAIN_FONT=… BOLD_FONT=… MONO_FONT=… FALLBACK_FONT=…",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "face", Usage: "print only this face's path: main, bold, mono or fallback; non-zero exit when it is not found"},
			&cli.BoolFlag{Name: "shell", Usage: "print shell assignments, single-quoted"},
		},
		Action: run,
	}
}

func run(_ context.Context, cmd *cli.Command) (err error) {
	found, notes := fontresolve.Resolver{Fontconfig: fontresolve.Fontconfig{}}.Resolve()
	paths := map[string]string{"main": found.Main, "bold": found.Bold, "mono": found.Mono, "fallback": found.Fallback}
	w := cmd.Root().Writer
	if w == nil {
		w = os.Stdout
	}
	for _, n := range notes {
		_, _ = fmt.Fprintln(os.Stderr, n)
	}
	if face := cmd.String("face"); face != "" {
		p, ok := paths[face]
		if !ok {
			return eb.Build().Str("face", face).Errorf("unknown face; want main, bold, mono or fallback")
		}
		if p == "" {
			return eb.Build().Str("face", face).Errorf("no font found for the face")
		}
		_, _ = fmt.Fprintln(w, p)
		return
	}
	for _, f := range faces {
		if cmd.Bool("shell") {
			_, _ = fmt.Fprintf(w, "%s='%s'\n", f.variable, strings.ReplaceAll(paths[f.name], "'", `'\''`))
		} else {
			_, _ = fmt.Fprintf(w, "%-8s  %s\n", f.name, paths[f.name])
		}
	}
	return
}
