package vendortidy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v3"
	"golang.org/x/mod/modfile"
)

// NewCliCommand returns the `vendortidy` subcommand (registered under
// `golang`): the ADR-0384 offline tidy of a vendored workspace's member.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "vendortidy",
		Usage:     "Tidy a workspace member's go.mod and go.sum against the workspace's vendor tree, offline (ADR-0384)",
		ArgsUsage: "[module-dir]",
		Description: "Computes the requirements `go mod tidy` would give the module from its imports,\n" +
			"walked through the workspace's used modules and vendor/, at the versions the vendor\n" +
			"tree builds, and writes go.mod and go.sum. Needs no network and no module cache.\n" +
			"go.mod comes out as tidy writes it; go.sum may keep lines a connected tidy drops.\n" +
			"An import the workspace does not provide stops the run: vendor it on a connected host.\n" +
			"With --check nothing is written and drift exits non-zero.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "work", Usage: "path to go.work; empty asks the go command (go env GOWORK)"},
			&cli.StringFlag{Name: "goroot", Usage: "GOROOT deciding which imports are standard library; empty asks the go command (go env GOROOT)"},
			&cli.BoolFlag{Name: "check", Usage: "report drift and exit non-zero instead of writing"},
		},
		Action: runVendorTidy,
	}
}

func runVendorTidy(ctx context.Context, cmd *cli.Command) (err error) {
	moduleDir := "."
	if cmd.Args().Len() > 1 {
		return eb.Build().Int("args", cmd.Args().Len()).Errorf("at most one module directory")
	}
	if cmd.Args().Len() == 1 {
		moduleDir = cmd.Args().First()
	}
	moduleDir, err = filepath.Abs(moduleDir)
	if err != nil {
		return eb.Build().Str("dir", moduleDir).Errorf("resolve module directory: %w", err)
	}

	work, goRoot := cmd.String("work"), cmd.String("goroot")
	if work == "" || goRoot == "" {
		var envWork, envRoot string
		envWork, envRoot, err = goEnv(ctx, moduleDir)
		if err != nil {
			return err
		}
		if work == "" {
			work = envWork
		}
		if goRoot == "" {
			goRoot = envRoot
		}
	}
	if work == "" || work == "off" {
		return eb.Build().Str("dir", moduleDir).Errorf("no workspace: set GOWORK or pass --work")
	}

	var ws *Workspace
	ws, err = LoadWorkspace(work, goRoot)
	if err != nil {
		return err
	}
	var res Result
	res, err = ws.Tidy(moduleDir)
	if err != nil {
		return err
	}
	out := cmd.Root().Writer
	if out == nil {
		out = os.Stdout
	}
	reportProblems(out, res)
	if res.HasUnresolved() {
		return eb.Build().Str("module", res.Module).Errorf("imports the workspace does not provide; vendor them on a connected host")
	}

	goModPath := filepath.Join(moduleDir, "go.mod")
	goSumPath := filepath.Join(moduleDir, "go.sum")
	var oldMod, oldSum []byte
	oldMod, err = os.ReadFile(goModPath)
	if err != nil {
		return eb.Build().Str("path", goModPath).Errorf("read go.mod: %w", err)
	}
	oldSum, err = os.ReadFile(goSumPath)
	if err != nil && !os.IsNotExist(err) {
		return eb.Build().Str("path", goSumPath).Errorf("read go.sum: %w", err)
	}
	err = nil

	var newMod []byte
	newMod, err = FormatGoMod(oldMod, res)
	if err != nil {
		return err
	}
	var changes []VersionChange
	changes, err = CompareRequires(oldMod, res)
	if err != nil {
		return err
	}
	var idx SumIndex
	idx, err = ws.LoadSums()
	if err != nil {
		return err
	}
	reportChanges(out, res, changes)
	if oldGo := goLine(oldMod); oldGo != res.GoVersion {
		_, _ = fmt.Fprintf(out, "go       %s => %s (the highest go version a required module declares)\n", oldGo, res.GoVersion)
	}

	if cmd.Bool("check") {
		missing := MissingSums(oldSum, res, idx)
		for _, l := range missing {
			_, _ = fmt.Fprintf(out, "go.sum lacks: %s\n", l)
		}
		if !bytes.Equal(oldMod, newMod) || len(missing) > 0 {
			return eb.Build().Str("module", res.Module).Int("requireChanges", len(changes)).Int("missingSums", len(missing)).Errorf("go.mod or go.sum differs from the vendored workspace")
		}
		_, _ = fmt.Fprintf(out, "%s matches the vendored workspace\n", res.Module)
		return nil
	}

	newSum, missingSums, merr := MergeGoSum(oldSum, oldMod, res, idx)
	if merr != nil {
		return merr
	}
	for _, mv := range missingSums {
		_, _ = fmt.Fprintf(out, "warning: the workspace carries no checksum for %s@%s; go.sum will need it on a connected host\n", mv.Path, mv.Version)
	}
	err = writeKeepingMode(goModPath, newMod)
	if err != nil {
		return err
	}
	if len(newSum) > 0 || len(oldSum) > 0 {
		err = writeKeepingMode(goSumPath, newSum)
	}
	return
}

// goEnv asks the go command for GOWORK and GOROOT as seen from dir.
func goEnv(ctx context.Context, dir string) (work string, goRoot string, err error) {
	var b []byte
	b, err = extbin.Go.Output(ctx, extbin.Opts{Dir: dir}, "env", "GOWORK", "GOROOT")
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("go env GOWORK GOROOT: %w", err)
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		err = eb.Build().Str("output", string(b)).Errorf("unexpected go env output")
		return
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
}

func goLine(goMod []byte) string {
	mf, err := modfile.ParseLax("go.mod", goMod, nil)
	if err != nil || mf.Go == nil {
		return ""
	}
	return mf.Go.Version
}

func writeKeepingMode(p string, b []byte) (err error) {
	mode := os.FileMode(0o644)
	if st, serr := os.Stat(p); serr == nil {
		mode = st.Mode().Perm()
	}
	err = os.WriteFile(p, b, mode)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("write: %w", err)
	}
	return
}

func reportProblems(w io.Writer, res Result) {
	for _, p := range res.Problems {
		switch p.Kind {
		case ProblemKindUnresolvedImport:
			_, _ = fmt.Fprintf(w, "unresolved import %s (imported by %s)\n", p.Path, p.Importer)
		case ProblemKindWorkspaceOnly:
			_, _ = fmt.Fprintf(w, "not required: %s is used through the workspace and has no version in vendor/modules.txt\n", p.Path)
		}
	}
}

func reportChanges(w io.Writer, res Result, changes []VersionChange) {
	for _, c := range changes {
		switch c.Kind {
		case VersionChangeKindAdded:
			_, _ = fmt.Fprintf(w, "add      %s %s\n", c.Path, c.NewVersion)
		case VersionChangeKindRemoved:
			_, _ = fmt.Fprintf(w, "drop     %s %s\n", c.Path, c.OldVersion)
		case VersionChangeKindUpgraded:
			_, _ = fmt.Fprintf(w, "upgrade  %s %s => %s\n", c.Path, c.OldVersion, c.NewVersion)
		case VersionChangeKindDowngraded:
			_, _ = fmt.Fprintf(w, "DOWNGRADE %s %s => %s (the workspace builds the older version)\n", c.Path, c.OldVersion, c.NewVersion)
		case VersionChangeKindMarking:
			marking := "direct"
			if c.Indirect {
				marking = "indirect"
			}
			_, _ = fmt.Fprintf(w, "mark     %s %s as %s\n", c.Path, c.NewVersion, marking)
		}
	}
	for _, r := range res.Requires {
		if r.Kept {
			_, _ = fmt.Fprintf(w, "kept     %s %s (provides no vendored package; cannot be judged offline)\n", r.Path, r.Version)
		}
		if r.Replacement != "" {
			_, _ = fmt.Fprintf(w, "replace  %s => %s is how the workspace builds it; go.mod needs the same replace\n", r.Path, r.Replacement)
		}
	}
}
