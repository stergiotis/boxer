package cli

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/urfave/cli/v2"
)

// leeway sqlsurface (ADR-0171 §SD2): provision and inspect leeway's SQL read
// surface on a ClickHouse server.
//
// Before this existed the only ways to provision were Go code, play's
// startup reconcile, or piping `readback.HelperUDFsSQL()` — and that last
// one installs the functions without the version marker, leaving a server
// that works but cannot say what it carries, which is the exact failure the
// marker exists to prevent. `print` here emits the marker with the rest.
//
// The four subcommands map one-to-one onto the package API, including its
// central asymmetry: `install` drops this repository's own withdrawn
// spellings automatically, while `drop-undeclared` — a separate, explicitly
// named, explicitly confirmed command — is the only thing that removes a
// name nobody here declared.

// NewCliCommandSqlSurface builds the `leeway sqlsurface` command group.
func NewCliCommandSqlSurface() *cli.Command {
	return &cli.Command{
		Name:  "sqlsurface",
		Usage: "provision and inspect leeway's SQL read surface — the co/ragged pack, the read-back family and the identity UDFs (ADR-0171 §SD2)",
		Subcommands: []*cli.Command{
			newCliCommandSqlSurfacePrint(),
			newCliCommandSqlSurfaceInstall(),
			newCliCommandSqlSurfaceStatus(),
			newCliCommandSqlSurfaceDropUndeclared(),
		},
	}
}

// sqlSurfaceConnFlags are the connection flags every server-touching
// subcommand takes. Defaults come from the ClickHouse environment
// (ADR-0009's registered variables), so a configured shell needs none of
// them.
func sqlSurfaceConnFlags() []cli.Flag {
	def := chclient.Defaults()
	return []cli.Flag{
		&cli.StringFlag{Name: "url", Value: def.URL, Usage: "ClickHouse HTTP endpoint (defaults to the configured environment)"},
		&cli.StringFlag{Name: "user", Value: def.User, Usage: "ClickHouse user"},
		&cli.StringFlag{Name: "password", Usage: "ClickHouse password"},
		&cli.DurationFlag{Name: "timeout", Value: 2 * time.Minute, Usage: "bound on the whole operation; an install is several dozen statements"},
	}
}

// sqlSurfaceClient resolves the connection: the environment first, then any
// flag the caller actually set.
//
// IsSet is what makes that layering work — a flag left at its default must
// not clobber a value the environment supplied, and reading the flag value
// alone cannot tell "unset" from "set to the default".
func sqlSurfaceClient(cCtx *cli.Context) (client *chclient.Client, url string, ctx context.Context, cancel context.CancelFunc) {
	cfg := chclient.ConfigFromEnv()
	if cCtx.IsSet("url") {
		cfg.URL = cCtx.String("url")
	}
	if cCtx.IsSet("user") {
		cfg.User = cCtx.String("user")
	}
	if cCtx.IsSet("password") {
		cfg.Password = cCtx.String("password")
	}
	ctx, cancel = context.WithTimeout(cCtx.Context, cCtx.Duration("timeout"))
	client = chclient.New(cfg, nil)
	url = cfg.URL
	return
}

func newCliCommandSqlSurfacePrint() *cli.Command {
	return &cli.Command{
		Name:  "print",
		Usage: "print the CREATE FUNCTION statements for the whole surface, version marker included — for provisioning by hand or offline",
		Action: func(cCtx *cli.Context) error {
			var b strings.Builder
			for _, stmt := range lwsqlsurface.Statements() {
				b.WriteString(stmt)
				b.WriteString(";\n")
			}
			_, err := os.Stdout.WriteString(b.String())
			return err
		},
	}
}

func newCliCommandSqlSurfaceInstall() *cli.Command {
	return &cli.Command{
		Name:  "install",
		Usage: "install all three families and the version marker, verify it, and drop this repository's withdrawn spellings",
		Flags: sqlSurfaceConnFlags(),
		Action: func(cCtx *cli.Context) error {
			client, url, ctx, cancel := sqlSurfaceClient(cCtx)
			defer cancel()
			err := lwsqlsurface.Install(ctx, client)
			if err != nil {
				return err
			}
			_, err = os.Stdout.WriteString("installed surface v" + strconv.Itoa(lwsqlsurface.Version) +
				" — " + strconv.Itoa(len(lwsqlsurface.DeclaredFunctions())) + " function(s) on " + url + "\n")
			return err
		},
	}
}

func newCliCommandSqlSurfaceStatus() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "report what the server carries against what this build declares; changes nothing",
		Flags: append(sqlSurfaceConnFlags(),
			&cli.BoolFlag{Name: "fail-on-drift", Usage: "exit non-zero unless the server matches this build exactly"}),
		Action: func(cCtx *cli.Context) error {
			client, url, ctx, cancel := sqlSurfaceClient(cCtx)
			defer cancel()
			rep, err := lwsqlsurface.Reconcile(ctx, client, lwsqlsurface.ReconcileReport)
			if err != nil {
				return err
			}
			_, err = os.Stdout.WriteString(formatSurfaceStatus(rep, url))
			if err != nil {
				return err
			}
			if cCtx.Bool("fail-on-drift") && !rep.InSync() {
				return eh.Errorf("the endpoint does not match this build")
			}
			return nil
		},
	}
}

func newCliCommandSqlSurfaceDropUndeclared() *cli.Command {
	return &cli.Command{
		Name:  "drop-undeclared",
		Usage: "drop leeway-namespaced functions the server carries that NO build declares — they may be a fork's; run status first",
		Flags: append(sqlSurfaceConnFlags(),
			&cli.BoolFlag{Name: "confirm", Usage: "required: without it the command reports what it would drop and stops"}),
		Action: func(cCtx *cli.Context) error {
			client, url, ctx, cancel := sqlSurfaceClient(cCtx)
			defer cancel()

			// Always look before deleting, and print the list either way.
			// The confirm gate sits between the two calls rather than
			// before them, so the refusal names what it refused to do.
			rep, err := lwsqlsurface.Reconcile(ctx, client, lwsqlsurface.ReconcileReport)
			if err != nil {
				return err
			}
			if len(rep.Undeclared) == 0 {
				_, err = os.Stdout.WriteString("nothing to drop on " + url +
					": every leeway-namespaced function is declared by this build\n")
				return err
			}
			_, err = os.Stdout.WriteString("undeclared on " + url + ": " +
				strings.Join(rep.Undeclared, ", ") + "\n")
			if err != nil {
				return err
			}
			if !cCtx.Bool("confirm") {
				_, err = os.Stdout.WriteString("not dropping anything — pass --confirm to remove them\n")
				return err
			}
			rep, err = lwsqlsurface.Reconcile(ctx, client, lwsqlsurface.ReconcileDrop)
			// Report what went even when the loop stopped early: a DROP can
			// fail partway (an Executable or WASM UDF the statement cannot
			// remove), and returning the error alone would leave the caller
			// not knowing which names are already gone.
			if len(rep.Dropped) > 0 {
				_, wErr := os.Stdout.WriteString("dropped: " + strings.Join(rep.Dropped, ", ") + "\n")
				if wErr != nil && err == nil {
					err = wErr
				}
			}
			if err != nil {
				return err
			}
			if len(rep.Dropped) == 0 {
				_, err = os.Stdout.WriteString("nothing was dropped\n")
			}
			return err
		},
	}
}

// formatSurfaceStatus renders a report for a person. Separated from the
// urfave plumbing so the wording is testable without a server — the same
// split `leeway ddl compose` uses.
//
// The ordering is what a reader needs in the order they need it: the
// revision, then what is missing (the failure that returns wrong answers),
// then leftovers, and only then the all-clear.
func formatSurfaceStatus(rep lwsqlsurface.Report, url string) (out string) {
	var b strings.Builder
	b.WriteString("endpoint: " + url + "\n")

	switch {
	case rep.MarkerUnreadable:
		// The marker IS there; its body is not a revision. Saying "no
		// marker" would send someone to install over a server whose real
		// problem is that the function was edited.
		b.WriteString("version:  the marker is installed but its value is not a revision — it has been edited\n")
	case rep.PreSurface():
		// Not "unknown": this server works, it was just provisioned before
		// the three families shared a marker. Saying so is the difference
		// between "reinstall" and "investigate".
		b.WriteString("version:  no surface marker — provisioned by a pre-surface build; `install` moves it forward\n")
	case rep.ServerVersion < 0:
		b.WriteString("version:  no surface marker (this build: v" + strconv.Itoa(lwsqlsurface.Version) + ")\n")
	case rep.ServerVersion == lwsqlsurface.Version:
		b.WriteString("version:  v" + strconv.Itoa(rep.ServerVersion) + " — matches this build\n")
	default:
		b.WriteString("version:  v" + strconv.Itoa(rep.ServerVersion) + " (this build: v" +
			strconv.Itoa(lwsqlsurface.Version) + ") — names may resolve to older definitions\n")
	}

	if len(rep.Missing) > 0 {
		b.WriteString("missing:  " + strconv.Itoa(len(rep.Missing)) + " declared function(s) absent — run `install`\n")
		for _, n := range rep.Missing {
			b.WriteString("          " + n + "\n")
		}
	}
	if len(rep.Retired) > 0 {
		b.WriteString("retired:  " + strconv.Itoa(len(rep.Retired)) +
			" withdrawn spelling(s) still installed — `install` drops them\n")
		for _, n := range rep.Retired {
			b.WriteString("          " + n + "\n")
		}
	}
	if len(rep.Undeclared) > 0 {
		// Listed, but NOT part of the in-sync verdict: somebody else's
		// function on a shared server is not this build being wrong, and
		// making --fail-on-drift red for it would leave deleting their
		// function as the only way to go green.
		b.WriteString("extra:    " + strconv.Itoa(len(rep.Undeclared)) +
			" function(s) no build declares — not this build's, left alone; `drop-undeclared` removes them\n")
		for _, n := range rep.Undeclared {
			b.WriteString("          " + n + "\n")
		}
	}
	if rep.InSync() {
		b.WriteString("in sync:  " + strconv.Itoa(len(lwsqlsurface.DeclaredFunctions())) + " function(s) declared, all present\n")
	}
	out = b.String()
	return
}
