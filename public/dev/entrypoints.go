package dev

import (
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
	"golang.org/x/tools/go/packages"
)

const (
	urfaveCliV2ImportPath = "github.com/urfave/cli/v2"
	// loggingApplyFQN is referenced by every main that wires
	// `Before: logging.Apply` on its cli.App. The logging package
	// installs eh.MarshalError via init(), so no explicit
	// setup-call is needed; what mains must do instead is hook
	// Apply into the cli lifecycle so the writer/level/startup
	// record actually engage.
	loggingApplyFQN     = "github.com/stergiotis/boxer/public/observability/logging.Apply"
	buildVersionInfoFQN = "github.com/stergiotis/boxer/public/observability/vcs.BuildVersionInfo"
)

func newEntryPointsSubcommand() *cli.Command {
	return &cli.Command{
		Name:  "entry-points",
		Usage: "audit package main entry points against CODINGSTANDARDS.md 'Entry Points'",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "root",
				Value: ".",
				Usage: "directory to load packages from (passed as packages.Config.Dir)",
			},
			&cli.StringFlag{
				Name:  "tags",
				Value: "",
				Usage: "comma-separated build tags forwarded to packages.Load",
			},
			&cli.StringFlag{
				Name:  "baseline",
				Value: "",
				Usage: "path to a baseline file (one grandfathered package import path per line; # comments and blank lines allowed); listed packages are reported but do not trigger --strict failure",
			},
			&cli.BoolFlag{
				Name:  "strict",
				Value: false,
				Usage: "exit non-zero if any entry point fails any of the three checks (after baseline subtraction)",
			},
		},
		Action: entryPointsAction,
	}
}

// EntryPointsConfig parameterises [AuditEntryPointsE].
//
// Root is the directory packages are loaded from; BaselinePath, when non-empty,
// names a file of grandfathered package import paths (see [loadBaseline]).
type EntryPointsConfig struct {
	Root         string
	BaselinePath string
	Tags         []string
}

// EntryPointAudit is one `package main` measured against the three checks
// CODINGSTANDARDS.md "Entry Points" requires.
//
// Baselined records whether the package is grandfathered, not whether it
// passes; a baselined package with all three checks true is simply Conformant.
type EntryPointAudit struct {
	PkgPath   string
	CliOK     bool
	LoggingOK bool
	VcsOK     bool
	Baselined bool
}

func (inst EntryPointAudit) Conformant() (ok bool) {
	return inst.CliOK && inst.LoggingOK && inst.VcsOK
}

// Failing reports whether this entry point should fail a strict audit — not
// conformant and not grandfathered.
func (inst EntryPointAudit) Failing() (bad bool) {
	return !inst.Conformant() && !inst.Baselined
}

// AuditEntryPointsE loads every package under cfg.Root and measures each
// `package main` against the entry-point standard.
//
// Results are sorted by import path so callers render a stable table. The
// audit is reported, not enforced: deciding what a failure costs belongs to
// the caller, which is what lets the CLI, the composite gate and a consuming
// repository share one implementation.
func AuditEntryPointsE(ctx context.Context, cfg EntryPointsConfig) (audits []EntryPointAudit, err error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}

	baseline := make(map[string]struct{}, 0)
	if cfg.BaselinePath != "" {
		baseline, err = loadBaseline(cfg.BaselinePath)
		if err != nil {
			return
		}
	}

	pcfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports,
		Context: ctx,
		Dir:     root,
	}
	if len(cfg.Tags) > 0 {
		pcfg.BuildFlags = []string{"-tags=" + strings.Join(cfg.Tags, ",")}
	}

	var pkgs []*packages.Package
	pkgs, err = packages.Load(pcfg, "./...")
	if err != nil {
		err = eb.Build().Str("root", root).Strs("tags", cfg.Tags).Errorf("unable to load packages: %w", err)
		return
	}

	audits = make([]EntryPointAudit, 0, 8)
	for _, p := range pkgs {
		if p.Name != "main" {
			continue
		}
		if len(p.Errors) > 0 {
			log.Warn().
				Str("pkg", p.PkgPath).
				Int("errCount", len(p.Errors)).
				Str("firstErr", p.Errors[0].Msg).
				Msg("package has load errors; audit may be incomplete")
		}
		_, cliOK := p.Imports[urfaveCliV2ImportPath]
		_, isBaselined := baseline[p.PkgPath]
		audits = append(audits, EntryPointAudit{
			PkgPath:   p.PkgPath,
			CliOK:     cliOK,
			LoggingOK: packageReferencesFunc(p, loggingApplyFQN),
			VcsOK:     packageCallsFunc(p, buildVersionInfoFQN),
			Baselined: isBaselined,
		})
	}
	sort.Slice(audits, func(i, j int) bool { return audits[i].PkgPath < audits[j].PkgPath })
	return
}

// WriteEntryPointsTable renders audits as the aligned table the CLI and the
// composite gate both print.
func WriteEntryPointsTable(w io.Writer, audits []EntryPointAudit) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ENTRY POINT\tCLI/V2\tLOGGING.APPLY\tBUILDVERSIONINFO\tSTATUS")
	for _, a := range audits {
		var status string
		switch {
		case a.Conformant():
			status = "ok"
		case a.Baselined:
			status = "baselined"
		default:
			status = "fail"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", a.PkgPath, mark(a.CliOK), mark(a.LoggingOK), mark(a.VcsOK), status)
	}
	_ = tw.Flush()
}

func entryPointsAction(ctx *cli.Context) (err error) {
	root := ctx.String("root")
	strict := ctx.Bool("strict")

	tags := make([]string, 0, 8)
	if t := ctx.String("tags"); t != "" {
		for x := range strings.SplitSeq(t, ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				tags = append(tags, x)
			}
		}
	}

	var audits []EntryPointAudit
	audits, err = AuditEntryPointsE(ctx.Context, EntryPointsConfig{
		Root:         root,
		BaselinePath: ctx.String("baseline"),
		Tags:         tags,
	})
	if err != nil {
		return
	}

	if len(audits) == 0 {
		log.Info().Str("root", root).Strs("tags", tags).Msg("no main packages discovered")
		return
	}

	WriteEntryPointsTable(os.Stdout, audits)

	failCount := uint64(0)
	baselinedCount := uint64(0)
	for _, a := range audits {
		if a.Failing() {
			failCount++
		} else if a.Baselined && !a.Conformant() {
			baselinedCount++
		}
	}

	log.Info().
		Int("mains", len(audits)).
		Uint64("fail", failCount).
		Uint64("baselined", baselinedCount).
		Bool("strict", strict).
		Msg("entry-points audit complete")

	if strict && failCount > 0 {
		err = eb.Build().Uint64("fail", failCount).Uint64("baselined", baselinedCount).Errorf("entry-points: one or more entry points fail conformance check (not in baseline)")
	}
	return
}

// loadBaseline reads a list of grandfathered package import paths from
// path. Each non-empty, non-comment line is a fully-qualified package
// import path. '#' starts a line comment. Whitespace is trimmed. Unknown
// paths in the file are not an error here; the entry-points action
// merely looks them up by string equality against discovered mains.
func loadBaseline(path string) (out map[string]struct{}, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("read baseline: %w", err)
		return
	}
	out = make(map[string]struct{}, 16)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = struct{}{}
	}
	return
}

func packageCallsFunc(p *packages.Package, fullName string) (found bool) {
	if p == nil || p.TypesInfo == nil {
		return
	}
	for _, file := range p.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var ident *ast.Ident
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				ident = fn.Sel
			case *ast.Ident:
				ident = fn
			default:
				return true
			}
			tf, ok := p.TypesInfo.Uses[ident].(*types.Func)
			if !ok {
				return true
			}
			if tf.FullName() == fullName {
				found = true
				return false
			}
			return true
		})
		if found {
			return
		}
	}
	return
}

// packageReferencesFunc reports whether p contains any reference to the
// function identified by fullName. Unlike packageCallsFunc this matches
// value uses (e.g. `Before: logging.Apply` — assigning the function
// value to a struct field, not invoking it), which is the shape mains
// must use to hook logging.Apply into cli.App.Before.
func packageReferencesFunc(p *packages.Package, fullName string) (found bool) {
	if p == nil || p.TypesInfo == nil {
		return
	}
	for ident, obj := range p.TypesInfo.Uses {
		if found {
			break
		}
		tf, ok := obj.(*types.Func)
		if !ok {
			continue
		}
		if tf.FullName() == fullName {
			found = true
			_ = ident
		}
	}
	return
}

func mark(ok bool) string {
	if ok {
		return "ok"
	}
	return "miss"
}
