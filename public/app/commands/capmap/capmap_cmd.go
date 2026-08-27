// Package capmap is the `boxer capmap` command: it reads a business-capability
// vault and writes it into `boxer.facts` as competence and relation rows
// (ADR-0168).
//
// The vault says *capability*, boxer says *competence* — "capability" belongs
// to the runtime's security capabilities here (§SD6). This command speaks
// boxer's side of that boundary.
//
// Four verbs, two of which need no database:
//
//   - `parse` reads the vault and reports what is in it — counts, the files
//     that were not competences, and the links that did not resolve. No
//     ClickHouse, so it works in a checkout with nothing running.
//   - `similar` ranks the competences by the compression distance of their
//     prose and writes each one's nearest neighbours into its note as
//     `similar:` frontmatter — the one derived fact the vault carries, and
//     the one edit this command makes to it (capmap_cmd_similar.go).
//   - `load` reads the vault and writes the rows.
//   - `dump` is `load` backwards: it reads the rows and writes a vault.
//
// `load` and `dump` are named after the pair they are, which is also what the
// prototype this was ported from called them. The vault stays authoritative
// (ADR-0168 §SD3), so `dump` is not a second editing surface — it is how a
// corpus that has been through the store comes back into the form a person
// edits and git diffs, and it is what makes the store a safe place to keep one.
//
// The corpus model lives in
// [github.com/stergiotis/boxer/public/gov/capmapcorpus] and the row encoding in
// [github.com/stergiotis/boxer/public/gov/capmapfacts]; this package is only
// the CLI surface over them.
package capmap

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapfacts"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// defaultEngine is empty on purpose: an empty clause makes the store apply the
// same MergeTree clause the runtime's own facts table is created with, so the
// two cannot drift.
//
// Do not be tempted to write something readable like `ORDER BY (id)` here.
// There is no column called `id` — leeway spells it `id:id:u64:47::0:` — and
// the clause is passed through to ClickHouse verbatim, so the mistake surfaces
// only when a table is actually created, not when the DDL is composed.
const defaultEngine = ""

// maxReported bounds the per-finding lists `parse` prints. A vault with
// thousands of unresolved citations should report the count and a sample, not
// scroll the terminal.
const maxReported = 20

func vaultFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "vault",
		Usage: "competence vault directory; empty resolves " + capmapcorpus.EnvVaultDirName + " or the nearest doc/competences",
	}
}

// storeFlags are the connection and placement flags the two database verbs
// share, so `load` and `dump` cannot drift into naming the same table
// differently.
func storeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "clickhouse-url", Value: chclient.Defaults().URL, Usage: "ClickHouse HTTP endpoint"},
		&cli.StringFlag{Name: "user", Value: chclient.Defaults().User, Usage: "ClickHouse user"},
		&cli.StringFlag{Name: "password", Usage: "ClickHouse password"},
		&cli.StringFlag{Name: "database", Value: factsschema.DatabaseName, Usage: "target database"},
		&cli.StringFlag{Name: "table", Value: factsschema.TableName, Usage: "target table"},
	}
}

// NewCliCommand wires `boxer capmap` and its subcommands.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "capmap",
		Usage: "read a business-capability vault; report it, rank its competences by resemblance, load it into boxer.facts as competence and relation rows, or dump it back out",
		Subcommands: []*cli.Command{
			{
				Name:   "parse",
				Usage:  "read the vault and report counts, skipped files and unresolved links (no database)",
				Flags:  []cli.Flag{vaultFlag()},
				Action: actionParse,
			},
			similarCommand(),
			{
				Name: "load",
				// `ingest` was this verb's only name until `dump` arrived and
				// made the pair worth naming as one. Kept as an alias rather
				// than removed: it is in ADR-0168, in the applet book's prose
				// and in whatever anybody scripted.
				Aliases: []string{"ingest"},
				Usage:   "read the vault and write it into boxer.facts",
				Flags: append(append([]cli.Flag{vaultFlag()}, storeFlags()...),
					&cli.BoolFlag{Name: "setup-table", Usage: "create the database and table first if they are absent"},
					&cli.StringFlag{Name: "engine", Value: defaultEngine, Usage: "engine clause used by --setup-table; empty uses the store's own default"},
				),
				Action: actionLoad,
			},
			{
				Name:  "dump",
				Usage: "read the competences in boxer.facts and write them back out as a vault",
				Flags: append([]cli.Flag{
					&cli.StringFlag{Name: "out", Required: true, Usage: "directory to write the vault into"},
					&cli.BoolFlag{Name: "force", Usage: "write into a directory that already holds files"},
				}, storeFlags()...),
				Action: actionDump,
			},
		},
	}
}

// readVault resolves the vault and parses it, preferring an explicit --vault
// over the environment. Unlike capmapcorpus.Load, a CLI wants the reason it
// failed rather than an empty corpus.
func readVault(c *cli.Context) (corpus capmapcorpus.Corpus, dir string, err error) {
	if dir = c.String("vault"); dir == "" {
		if dir, err = capmapcorpus.ResolveVault(); err != nil {
			return corpus, "", err
		}
	}
	if corpus, err = capmapcorpus.ParseDir(dir); err != nil {
		return corpus, dir, eh.Errorf("unable to read vault %q: %w", dir, err)
	}
	return corpus, dir, nil
}

func actionParse(c *cli.Context) (err error) {
	var (
		corpus capmapcorpus.Corpus
		dir    string
	)
	if corpus, dir, err = readVault(c); err != nil {
		return err
	}
	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(out, "vault\t%s\n", dir)
	fmt.Fprintf(out, "competences\t%d\n", len(corpus.Competences))
	fmt.Fprintf(out, "relations\t%d\n", len(corpus.Relations))

	byResolution := map[capmapcorpus.ResolutionE]int{}
	for _, r := range corpus.Relations {
		byResolution[r.Resolution]++
	}
	for _, r := range []capmapcorpus.ResolutionE{
		capmapcorpus.ResolutionDirect, capmapcorpus.ResolutionDirRef,
		capmapcorpus.ResolutionExternal, capmapcorpus.ResolutionUnresolved,
	} {
		fmt.Fprintf(out, "  %s\t%d\n", r, byResolution[r])
	}
	fmt.Fprintf(out, "skipped files\t%d\n", len(corpus.Skipped))
	_ = out.Flush()

	// The two lists worth acting on. Citations and dirrefs are deliberately
	// absent: the first are not defects and the second are a mechanical
	// rewrite, so mixing either in here would dilute what is left.
	reportSkipped(corpus.Skipped)
	reportUnresolved(capmapcorpus.UnresolvedRelations(corpus.Relations))
	return nil
}

func reportSkipped(skipped []capmapcorpus.SkippedFile) {
	if len(skipped) == 0 {
		return
	}
	fmt.Printf("\nskipped (not competences), showing %d of %d:\n", min(len(skipped), maxReported), len(skipped))
	for i, s := range skipped {
		if i >= maxReported {
			break
		}
		fmt.Printf("  %s — %s\n", s.Path, s.Reason)
	}
}

func reportUnresolved(broken []capmapcorpus.Relation) {
	if len(broken) == 0 {
		fmt.Println("\nno unresolved links")
		return
	}
	sort.Slice(broken, func(a, b int) bool {
		if broken[a].SourceSlug != broken[b].SourceSlug {
			return broken[a].SourceSlug < broken[b].SourceSlug
		}
		return broken[a].Target < broken[b].Target
	})
	fmt.Printf("\nunresolved links, showing %d of %d:\n", min(len(broken), maxReported), len(broken))
	for i, r := range broken {
		if i >= maxReported {
			break
		}
		where := string(r.Kind)
		if r.Section != "" {
			where += "/" + r.Section
		}
		fmt.Printf("  %s [%s] -> %s\n", r.SourceSlug, where, r.Target)
	}
}

func actionLoad(c *cli.Context) (err error) {
	var (
		corpus capmapcorpus.Corpus
		dir    string
	)
	if corpus, dir, err = readVault(c); err != nil {
		return err
	}
	if len(corpus.Competences) == 0 {
		return eh.Errorf("vault %q holds no competences; refusing to ingest an empty corpus", dir)
	}

	ctx := context.Background()
	database, table := c.String("database"), c.String("table")
	if c.Bool("setup-table") {
		store, sErr := chstore.New(chstore.Config{
			URL:      c.String("clickhouse-url"),
			User:     c.String("user"),
			Password: c.String("password"),
			Database: database,
			Table:    table,
		})
		if sErr != nil {
			return eh.Errorf("unable to build the store for --setup-table: %w", sErr)
		}
		if err = store.SetupTable(ctx, c.String("engine")); err != nil {
			return eh.Errorf("unable to set up %s.%s: %w", database, table, err)
		}
	}

	sink := chclient.New(chclient.Config{
		URL:      c.String("clickhouse-url"),
		User:     c.String("user"),
		Password: c.String("password"),
	}, nil)

	var stats capmapfacts.Stats
	if stats, err = capmapfacts.Ingest(ctx, corpus, sink, database+"."+table, time.Now().UTC()); err != nil {
		return err
	}
	fmt.Printf("loaded %d rows into %s.%s from %s (%d competences, %d relations)\n",
		stats.Rows, database, table, dir, stats.Competences, stats.Relations)
	return nil
}

func actionDump(c *cli.Context) (err error) {
	out := c.String("out")
	if err = checkOutputDir(out, c.Bool("force")); err != nil {
		return err
	}
	client := chclient.New(chclient.Config{
		URL:      c.String("clickhouse-url"),
		User:     c.String("user"),
		Password: c.String("password"),
	}, nil)
	database, table := c.String("database"), c.String("table")
	qualified := database + "." + table

	ctx := context.Background()
	corpus, err := capmapfacts.ReadCorpus(ctx, client, qualified)
	if err != nil {
		return err
	}
	if len(corpus.Competences) == 0 {
		// Writing an empty vault over a directory the operator named is the
		// one outcome nobody wants from a typo'd --table.
		return eh.Errorf("%s holds no competences; refusing to write an empty vault to %q", qualified, out)
	}
	stats, err := capmapcorpus.WriteVault(corpus, out)
	if err != nil {
		return err
	}
	fmt.Printf("dumped %d competences and %d relations from %s into %s (%d directory-backed)\n",
		len(corpus.Competences), len(corpus.Relations), qualified, out, stats.Directories)
	return nil
}

// checkOutputDir refuses to write into a directory that already holds
// something, unless the operator says so.
//
// A dump writes and never deletes, so pointing it at a populated vault would
// leave files from both — the previous corpus's competences that this one no
// longer has, silently mixed in with the new ones. Naming an empty directory is
// the honest default; --force is for the operator who knows what is in theirs.
func checkOutputDir(dir string, force bool) (err error) {
	if dir == "" {
		return eh.Errorf("no output directory")
	}
	entries, rErr := os.ReadDir(dir)
	if rErr != nil {
		if os.IsNotExist(rErr) {
			return nil
		}
		return eh.Errorf("unable to inspect %q: %w", dir, rErr)
	}
	if len(entries) > 0 && !force {
		return eh.Errorf("%q is not empty; a dump adds files and removes none, so its contents would mix with the corpus being written — empty it, name another, or pass --force", dir)
	}
	return nil
}
