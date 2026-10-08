package tabhost

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
)

// The checks a module runs on its tab binary (ADR-0278 SD7, proposed): that
// it compiles for wasip1 — which compiles exactly the apps it links — and a
// report of what those apps declare that the tab does not serve. The report
// is information, not a failure: an app may degrade by design.

// Gap is one bus subject an app's manifest declares that no service in the
// tab answers.
type Gap struct {
	App       string `json:"app"`
	Pattern   string `json:"pattern"`
	Direction string `json:"direction"`
	Reason    string `json:"reason"`
}

// TabReport is what a tab binary links, what its apps would miss, and what
// the tab answers by other means than a service (Reason then says how).
type TabReport struct {
	Apps     []string `json:"apps"`
	Gaps     []Gap    `json:"gaps"`
	Answered []Gap    `json:"answered"`
}

// answeredInTab are subjects a tab makes unnecessary rather than serves: the
// code behind them does the work itself under wasip1.
var answeredInTab = map[string]string{
	basemap.ClientCaps("")[0].Pattern: "the basemap fetches its tiles itself in a tab (ADR-0262 Update 2026-10-03)",
}

// serves reports whether a service the options turn on answers pattern. No
// in-tab service exists yet (SD2), so nothing is served; each service added to
// Services claims its subjects here.
func (inst Services) serves(pattern string) (yes bool) {
	_ = pattern
	return false
}

// Report lists reg's apps and, per app, each declared subject the services do
// not serve, in app then pattern order.
func Report(reg *app.Registry, services Services) (rep TabReport) {
	for _, m := range reg.AllManifests() {
		rep.Apps = append(rep.Apps, string(m.Id))
		for _, f := range m.Caps {
			if services.serves(f.Pattern) {
				continue
			}
			if how, ok := answeredInTab[f.Pattern]; ok {
				rep.Answered = append(rep.Answered, Gap{App: string(m.Id), Pattern: f.Pattern, Direction: f.Direction.String(), Reason: how})
				continue
			}
			rep.Gaps = append(rep.Gaps, Gap{App: string(m.Id), Pattern: f.Pattern, Direction: f.Direction.String(), Reason: f.Reason})
		}
	}
	sort.Strings(rep.Apps)
	sort.SliceStable(rep.Gaps, func(i, j int) bool {
		if rep.Gaps[i].App != rep.Gaps[j].App {
			return rep.Gaps[i].App < rep.Gaps[j].App
		}
		return rep.Gaps[i].Pattern < rep.Gaps[j].Pattern
	})
	return
}

// WriteReport prints rep as a table: the linked apps, then the gaps.
func WriteReport(w *os.File, rep TabReport) {
	_, _ = fmt.Fprintf(w, "%d apps linked; %d declared subjects no tab service answers\n", len(rep.Apps), len(rep.Gaps))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "app\tsubject\tdirection\treason")
	seen := map[string]bool{}
	for _, g := range rep.Gaps {
		seen[g.App] = true
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", g.App, g.Pattern, g.Direction, g.Reason)
	}
	for _, a := range rep.Apps {
		if !seen[a] {
			_, _ = fmt.Fprintf(tw, "%s\t-\t\tneeds nothing beyond its own bus client\n", a)
		}
	}
	for _, g := range rep.Answered {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\tanswered in the tab: %s\n", g.App, g.Pattern, g.Direction, g.Reason)
	}
	_ = tw.Flush()
}

func reportCommand(inst *Program) (cmd *cli.Command) {
	return &cli.Command{
		Name:  "tabreport",
		Usage: "list the apps this tab binary links and the bus subjects they declare that no tab service answers",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "print the report as JSON"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			rep := Report(app.DefaultRegistry, inst.opts.Services)
			if !cmd.Bool("json") {
				WriteReport(os.Stdout, rep)
				return
			}
			if err = json.MarshalWrite(os.Stdout, rep); err != nil {
				return eh.Errorf("tabreport: %w", err)
			}
			_, _ = fmt.Fprintln(os.Stdout)
			return
		},
	}
}

// CheckCompiles builds the tab binary package pkg for wasip1 as bundle does,
// into a scratch directory. When it does not compile, failing names the
// packages the compiler reported (its `# <package>` lines) and output is what
// the go command printed.
func CheckCompiles(pkg string) (failing []string, output string, err error) {
	mainDir, err := packageModuleDir(pkg)
	if err != nil {
		return
	}
	dir, err := os.MkdirTemp("", "tabcheck-")
	if err != nil {
		return nil, "", eh.Errorf("tabcheck: scratch directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	var b bytes.Buffer
	err = buildGoModule(mainDir, pkg, filepath.Join(dir, "tab.wasm"), false, &b)
	output = b.String()
	if err != nil {
		for _, line := range strings.Split(output, "\n") {
			if p, ok := strings.CutPrefix(line, "# "); ok && !slices.Contains(failing, p) {
				failing = append(failing, p)
			}
		}
	}
	return
}

// ReportOf runs the tab binary package pkg natively and returns its report.
func ReportOf(pkg string) (rep TabReport, err error) {
	mainDir, err := packageModuleDir(pkg)
	if err != nil {
		return
	}
	o, err := extbin.Go.Output(context.Background(), extbin.Opts{Dir: mainDir}, "run", "-tags", readTags(mainDir), pkg, "tabreport", "--json")
	if err != nil {
		return rep, eb.Build().Str("pkg", pkg).Errorf("tabcheck: run tabreport: %w", err)
	}
	if err = json.Unmarshal(o, &rep); err != nil {
		return rep, eb.Build().Str("pkg", pkg).Errorf("tabcheck: read the report: %w", err)
	}
	return
}
