// Package jackstay is the CLI of the guided ClickHouse-to-ClickHouse sync of
// ADR-0259: one subcommand per step, each reading or writing the plan file
// (§SD7), all through the engine's workflow functions:
//
//	boxer jackstay discover  --source host:8123 --target other:8123
//	boxer jackstay structure --source … --target … --plan plan.json [--database db] [--map db=newdb] [--filter db.t='expr']
//	boxer jackstay export    --source … --pack DIR [--database db] [--filter db.t='expr'] [--sample 1/100]
//	boxer jackstay structure --pack DIR --target … --plan plan.json
//	boxer jackstay apply-ddl --plan plan.json [--confirm]
//	boxer jackstay diff      --plan plan.json [--table db.t] [--final]
//	boxer jackstay sync      --plan plan.json --mode full|repair|sample [--sample 1/100] [--existing refuse|append|replace] [--dry-run]
//	boxer jackstay status    --plan plan.json [--disk]
//
// diff compares content by primary key and moves no rows. sync relays chunks
// from source to target, verifies each by digest before it enters the journal
// beside the plan, and resumes from that journal when run again; it shows a
// pre-flight of the target's free space, a live bar of rows landed, and waits
// at a free-space floor between chunks.
//
// A --filter restricts a table to the slice of rows it selects, on both
// servers (ADR-0271 §SD1). export writes the selected tables into a pack
// directory, and a structure step given --pack in place of --source makes the
// pack the plan's source, so the later steps run on a host that cannot reach
// the source server (§SD2, §SD3).
//
// Passwords come from the environment only (CLICKHOUSE_PASSWORD for the
// source, BOXER_JACKSTAY_TARGET_PASSWORD for the target) and are never written
// to a plan.
package jackstay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/hmi/progressbar"
	"github.com/stergiotis/boxer/public/hmi/progressest"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "jackstay",
		Usage: "guided sync of tables from one ClickHouse server to another (ADR-0259)",
		Subcommands: []*cli.Command{
			newDiscoverCommand(),
			newStructureCommand(),
			newExportCommand(),
			newApplyDDLCommand(),
			newDiffCommand(),
			newSyncCommand(),
			newStatusCommand(),
		},
	}
}

func endpointFlags() []cli.Flag {
	return []cli.Flag{
		clickhouseenv.Endpoint.AsCliFlag(env.WithCliFlagName("source")),
		clickhouseenv.User.AsCliFlag(env.WithCliFlagName("source-user")),
		jk.TargetEndpointEnv.AsCliFlag(),
		jk.TargetUserEnv.AsCliFlag(),
	}
}

func planFlag(required bool) cli.Flag {
	return &cli.PathFlag{Name: "plan", Usage: "plan document (JSON)", Required: required}
}

func newDiscoverCommand() *cli.Command {
	return &cli.Command{
		Name:  "discover",
		Usage: "list the databases the source (and target, when given) hold, from their system tables",
		Flags: endpointFlags(),
		Action: func(c *cli.Context) (err error) {
			ctx := c.Context
			w := os.Stdout
			src := jk.SourceEndpoint()
			err = discoverAndPrint(ctx, w, "source", jk.SourceClientConfig(src))
			if err != nil {
				return
			}
			dst, ok := jk.TargetEndpoint()
			if !ok {
				return
			}
			_, _ = fmt.Fprintln(w)
			return discoverAndPrint(ctx, w, "target", jk.TargetClientConfig(dst))
		},
	}
}

func discover(ctx context.Context, role string, cfg chclient.Config) (inv jk.Inventory, err error) {
	inv, err = jk.Discover(ctx, chclient.New(cfg, nil))
	if err != nil {
		err = eb.Build().Str("role", role).Str("url", cfg.URL).Errorf("unable to discover server: %w", err)
	}
	return
}

func discoverAndPrint(ctx context.Context, w io.Writer, role string, cfg chclient.Config) (err error) {
	var inv jk.Inventory
	inv, err = discover(ctx, role, cfg)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%s %s (user %s): ClickHouse %s, up %s, timezone %s\n", role, cfg.URL, cfg.User,
		inv.Server.Version, progressest.FormatDuration(time.Duration(inv.Server.UptimeSeconds)*time.Second), inv.Server.Timezone)
	type dbStats struct {
		tables, leeway int
		rows, bytes    uint64
	}
	stats := make(map[string]*dbStats, len(inv.Databases))
	for _, db := range inv.Databases {
		stats[db.Name] = &dbStats{}
	}
	for i := range inv.Tables {
		t := &inv.Tables[i]
		s := stats[t.Ref.Database]
		if s == nil {
			continue
		}
		s.tables++
		s.rows += t.TotalRows
		s.bytes += t.TotalBytes
		if datacatalog.Classify(t.ColumnNames()).Kind == datacatalog.KindLeeway {
			s.leeway++
		}
	}
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  DATABASE\tENGINE\tTABLES\tLEEWAY\tROWS\tBYTES")
	for _, db := range inv.Databases {
		if datacatalog.IsSystemDatabase(db.Name) {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t(system)\t\t\t\n", db.Name, db.Engine)
			continue
		}
		s := stats[db.Name]
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d\t%s\n", db.Name, db.Engine, s.tables, s.leeway, s.rows, progressest.FormatBytes(int64(s.bytes)))
	}
	return tw.Flush()
}

func newStructureCommand() *cli.Command {
	return &cli.Command{
		Name:  "structure",
		Usage: "judge every selected source table against the target and write the plan (no DDL is run)",
		Flags: append(endpointFlags(),
			planFlag(true),
			&cli.StringSliceFlag{Name: "database", Usage: "source database to include (repeatable); default: every non-system database"},
			&cli.StringSliceFlag{Name: "map", Usage: "rename a database on the target, as source=target (repeatable)"},
			&cli.BoolFlag{Name: "leeway-only", Usage: "plan only the tables that classify as leeway (ADR-0170)"},
			filterFlag(),
			&cli.PathFlag{Name: "pack", Usage: "read the source from this pack directory (written by export) instead of a server"},
		),
		Action: func(c *cli.Context) (err error) {
			ctx := c.Context
			sel := jk.Selection{Databases: c.StringSlice("database"), LeewayOnly: c.Bool("leeway-only")}
			sel.DatabaseMap, err = parseMap(c.StringSlice("map"))
			if err != nil {
				return
			}
			sel.Filters, err = parseFilters(filterValues(c))
			if err != nil {
				return
			}
			srcEp := jk.SourceEndpoint()
			src := jk.ServerSource(metaClient(jk.SourceClientConfig(srcEp)))
			if dir := c.Path("pack"); dir != "" {
				var p *jk.Pack
				p, err = jk.OpenPack(dir)
				if err != nil {
					return
				}
				src, srcEp = p, p.Endpoint()
			}
			dstEp, ok := jk.TargetEndpoint()
			if !ok {
				return eh.Errorf("--target is required: %w", jk.ErrNoTarget)
			}
			path := c.Path("plan")
			var old *jk.Plan
			if o, loadErr := jk.LoadPlan(path); loadErr == nil {
				old = &o
			} else if !errors.Is(loadErr, fs.ErrNotExist) {
				// A plan that exists and cannot be read is not overwritten
				// unread.
				return loadErr
			}
			var plan jk.Plan
			plan, err = jk.PlanStructure(ctx, src, metaClient(jk.TargetClientConfig(dstEp)), srcEp, dstEp, sel, old, time.Now())
			if err != nil {
				return
			}
			err = plan.Save(path)
			if err != nil {
				return
			}
			printPlan(os.Stdout, &plan)
			_, _ = fmt.Fprintf(os.Stdout, "\nplan written to %s\n", path)
			if plan.HasPendingDDL() {
				_, _ = fmt.Fprintf(os.Stdout, "review the DDL above, then: boxer jackstay apply-ddl --plan %s --confirm\n", path)
			}
			return
		},
	}
}

func metaClient(cfg chclient.Config) (q *chclient.Client) {
	return chclient.New(cfg, nil)
}

// openSource is the source a plan names: its pack, or its server through a
// metadata or a scan client.
func openSource(ep jk.Endpoint, scan bool) (src jk.SourceI, err error) {
	if ep.Pack != "" {
		var p *jk.Pack
		p, err = jk.OpenPackFor(ep)
		if err != nil {
			return
		}
		return p, nil
	}
	if scan {
		return jk.ServerSource(scanClient(jk.SourceClientConfig(ep))), nil
	}
	return jk.ServerSource(metaClient(jk.SourceClientConfig(ep))), nil
}

func filterFlag() cli.Flag {
	return &cli.GenericFlag{Name: "filter", Value: &filterEntries{}, Usage: "sync only the rows of a table that satisfy a ClickHouse boolean expression over its columns, as database.table=expr (repeatable)"}
}

// filterEntries collects --filter values whole: a StringSliceFlag splits each
// value on commas, and an expression holds them (IN lists, function arguments).
type filterEntries []string

func (inst *filterEntries) Set(v string) error {
	*inst = append(*inst, v)
	return nil
}

func (inst *filterEntries) String() string {
	return strings.Join(*inst, " ")
}

// filterValues is what --filter collected.
func filterValues(c *cli.Context) (entries []string) {
	if f, ok := c.Generic("filter").(*filterEntries); ok && f != nil {
		entries = *f
	}
	return
}

// parseFilters reads database.table=expr entries; the first '=' separates,
// since an expression may hold more.
func parseFilters(entries []string) (m map[string]string, err error) {
	if len(entries) == 0 {
		return
	}
	m = make(map[string]string, len(entries))
	for _, e := range entries {
		ref, expr, found := strings.Cut(e, "=")
		ref, expr = strings.TrimSpace(ref), strings.TrimSpace(expr)
		if !found || !strings.Contains(ref, ".") || expr == "" {
			err = eb.Build().Str("filter", e).Errorf("expected database.table=expr")
			return
		}
		if _, dup := m[ref]; dup {
			err = eb.Build().Str("table", ref).Errorf("a table takes one filter; combine them with AND")
			return
		}
		m[ref] = expr
	}
	return
}

// scanClient is for queries that read whole tables: no client timeout, the
// context cancels.
func scanClient(cfg chclient.Config) (q *chclient.Client) {
	return chclient.New(cfg, &http.Client{})
}

// parseRefs reads database.name references; the first dot separates, since a
// table name may hold dots and a database name rarely does. A reference
// without both parts names no table, so it is refused rather than matching
// nothing.
func parseRefs(entries []string) (refs []datacatalog.TableRef, err error) {
	for _, e := range entries {
		db, name, found := strings.Cut(strings.TrimSpace(e), ".")
		if !found || db == "" || name == "" {
			err = eb.Build().Str("table", e).Errorf("expected database.name")
			return
		}
		refs = append(refs, datacatalog.TableRef{Database: db, Name: name})
	}
	return
}

func refuseStale(w io.Writer, path string, stale []string) (err error) {
	_, _ = fmt.Fprintln(w, "the plan no longer matches the servers:")
	for _, s := range stale {
		_, _ = fmt.Fprintf(w, "  %s\n", s)
	}
	_, _ = fmt.Fprintf(w, "re-run `boxer jackstay structure` to write a fresh plan to %s\n", path)
	return eb.Build().Int("stale", len(stale)).Errorf("plan is stale: %w", jk.ErrStale)
}

func parseMap(entries []string) (m map[string]string, err error) {
	if len(entries) == 0 {
		return
	}
	m = make(map[string]string, len(entries))
	for _, e := range entries {
		from, to, found := strings.Cut(e, "=")
		if !found || from == "" || to == "" {
			err = eb.Build().Str("map", e).Errorf("expected source=target")
			return
		}
		m[from] = to
	}
	return
}

func printPlan(w io.Writer, plan *jk.Plan) {
	_, _ = fmt.Fprintf(w, "source %s (ClickHouse %s) → target %s (ClickHouse %s)\n",
		plan.Source.Label(), plan.SourceServer.Version, plan.Target.URL, plan.TargetServer.Version)
	for _, n := range plan.Notes {
		_, _ = fmt.Fprintf(w, "note: %s\n", n)
	}
	_, _ = fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "VERDICT\tSOURCE\tTARGET\tROWS\tBYTES\tLEEWAY")
	for _, t := range plan.Tables {
		lw := ""
		if t.Leeway {
			lw = "yes"
			if t.LeewayRelation != "" {
				lw += " (" + t.LeewayRelation + ")"
			}
		}
		target := t.Target.String()
		if t.Target == t.Source {
			target = "="
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", t.Verdict, t.Source, target, t.Rows, progressest.FormatBytes(int64(t.Bytes)), lw)
	}
	_ = tw.Flush()

	var detail strings.Builder
	for _, t := range plan.Tables {
		if len(t.Reasons) == 0 && len(t.Notes) == 0 && t.Filter == "" {
			continue
		}
		detail.WriteString("  " + t.Source.String() + "\n")
		if t.Filter != "" {
			detail.WriteString("    filter: " + t.Filter + "\n")
		}
		for _, r := range t.Reasons {
			detail.WriteString("    ✗ " + r + "\n")
		}
		for _, n := range t.Notes {
			detail.WriteString("    · " + n + "\n")
		}
	}
	if detail.Len() > 0 {
		_, _ = fmt.Fprintf(w, "\nreasons and notes:\n%s", detail.String())
	}

	counts := plan.CountVerdicts()
	parts := make([]string, 0, len(jk.AllVerdicts))
	for _, v := range jk.AllVerdicts {
		if counts[v] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[v], v))
		}
	}
	_, _ = fmt.Fprintf(w, "\n%d tables: %s\n", len(plan.Tables), strings.Join(parts, ", "))

	if !plan.HasPendingDDL() {
		return
	}
	_, _ = fmt.Fprintln(w, "\nproposed DDL for the target:")
	for _, sql := range plan.DatabaseDDL {
		_, _ = fmt.Fprintf(w, "  %s;\n", sql)
	}
	for _, t := range plan.Tables {
		for _, sql := range t.DDL {
			_, _ = fmt.Fprintf(w, "  %s;\n", sql)
		}
	}
}

func newApplyDDLCommand() *cli.Command {
	return &cli.Command{
		Name:  "apply-ddl",
		Usage: "re-check a plan against both servers and run its DDL on the target",
		Description: "Discovers both servers again and rebuilds the plan's structure section. " +
			"If any table's verdict or DDL has changed since the plan was written, nothing runs. " +
			"Without --confirm the statements are printed, not executed.",
		Flags: []cli.Flag{
			planFlag(true),
			&cli.BoolFlag{Name: "confirm", Usage: "execute the DDL (without it, only print what would run)"},
		},
		Action: func(c *cli.Context) (err error) {
			ctx := c.Context
			w := os.Stdout
			path := c.Path("plan")
			var plan jk.Plan
			plan, err = jk.LoadPlan(path)
			if err != nil {
				return
			}
			var srcQ jk.SourceI
			srcQ, err = openSource(plan.Source, false)
			if err != nil {
				return
			}
			dstCfg := jk.TargetClientConfig(plan.Target)
			dstQ := metaClient(dstCfg)
			if !c.Bool("confirm") {
				var pending jk.Plan
				var stale []string
				pending, stale, err = jk.Recheck(ctx, srcQ, dstQ, &plan, time.Now())
				if err != nil {
					return
				}
				if len(stale) > 0 {
					return refuseStale(w, path, stale)
				}
				if !pending.HasPendingDDL() {
					_, _ = fmt.Fprintln(w, "the plan has no DDL left to apply")
					return
				}
				_, _ = fmt.Fprintln(w, "would run on the target (pass --confirm to execute):")
				for _, sql := range pending.DatabaseDDL {
					_, _ = fmt.Fprintf(w, "  %s;\n", sql)
				}
				for _, t := range pending.Tables {
					for _, sql := range t.DDL {
						_, _ = fmt.Fprintf(w, "  %s;\n", sql)
					}
				}
				return
			}
			var guards []string
			guards, err = jk.DDLGuardSettings(ctx, dstQ)
			if err != nil {
				return
			}
			var applied, stale []string
			var after jk.Plan
			applied, after, stale, err = jk.ApplyDDLStep(ctx, srcQ, dstQ, chclient.New(jk.DDLClientConfig(dstCfg, guards), nil), &plan, time.Now())
			for _, sql := range applied {
				_, _ = fmt.Fprintf(w, "ran: %s\n", sql)
			}
			if err != nil {
				return
			}
			if len(stale) > 0 {
				return refuseStale(w, path, stale)
			}
			if len(applied) == 0 {
				_, _ = fmt.Fprintln(w, "the plan had no DDL left to apply")
			}
			err = after.Save(path)
			if err != nil {
				return
			}
			_, _ = fmt.Fprintln(w)
			printPlan(w, &after)
			_, _ = fmt.Fprintf(w, "\nplan updated in %s\n", path)
			return
		},
	}
}

func newDiffCommand() *cli.Command {
	def := jk.DefaultDiffOptions()
	return &cli.Command{
		Name:  "diff",
		Usage: "compare the content of the plan's tables on both servers by primary key, moving no rows",
		Description: "Re-checks the plan against both servers, then scans each table once per side for leaf digests " +
			"and once more for the row pairs of small differing leaves. Results are written into the plan.",
		Flags: []cli.Flag{
			planFlag(true),
			&cli.StringSliceFlag{Name: "table", Usage: "source table to diff, as database.name (repeatable); default: every diffable table"},
			&cli.BoolFlag{Name: "final", Usage: "read merge-semantics engines (Replacing, Collapsing, …) with FINAL"},
			&cli.Uint64Flag{Name: "pair-threshold", Value: def.PairThreshold, Usage: "largest differing leaf, in rows, compared row by row"},
			&cli.Uint64Flag{Name: "pair-budget", Value: def.PairBudget, Usage: "most rows fetched per table and side for row-by-row comparison"},
		},
		Action: func(c *cli.Context) (err error) {
			ctx := c.Context
			w := os.Stdout
			path := c.Path("plan")
			var only []datacatalog.TableRef
			only, err = parseRefs(c.StringSlice("table"))
			if err != nil {
				return
			}
			var plan jk.Plan
			plan, err = jk.LoadPlan(path)
			if err != nil {
				return
			}
			var src jk.SourceI
			src, err = openSource(plan.Source, true)
			if err != nil {
				return
			}
			dstCfg := jk.TargetClientConfig(plan.Target)
			var prev *jk.PlanTable
			var started time.Time
			opts := jk.DiffOptionsAll{
				Final:    c.Bool("final"),
				Chunking: jk.DefaultChunkingOptions(),
				Diff:     jk.DiffOptions{PairThreshold: c.Uint64("pair-threshold"), PairBudget: c.Uint64("pair-budget"), MaxExamples: def.MaxExamples},
				Only:     only,
				Progress: func(i int, n int, pt *jk.PlanTable) {
					if prev != nil {
						printDiff(w, prev, time.Since(started))
					}
					prev, started = pt, time.Now()
				},
			}
			// Digest scans read whole tables; the default client timeout is
			// for metadata. Cancellation comes from the context.
			var fresh jk.Plan
			var skipped, stale []string
			fresh, skipped, stale, err = jk.DiffStep(ctx, src, scanClient(dstCfg), &plan, opts, time.Now)
			for _, s := range skipped {
				_, _ = fmt.Fprintf(w, "skip %s\n", s)
			}
			if err != nil {
				return
			}
			if len(stale) > 0 {
				return refuseStale(w, path, stale)
			}
			err = fresh.Save(path)
			if err != nil {
				return
			}
			_, _ = fmt.Fprintf(w, "\nplan updated in %s\n", path)
			return
		},
	}
}

func printDiff(w io.Writer, pt *jk.PlanTable, took time.Duration) {
	d := pt.Diff
	head := fmt.Sprintf("%s → %s  [%d %s chunk(s) × %d leaves; %s]", pt.Source, pt.Target, d.Chunks, pt.Chunking.Kind, pt.Chunking.Leaves, progressest.FormatDuration(took))
	if d.IsIdentical() {
		_, _ = fmt.Fprintf(w, "identical  %s  rows %d\n", head, d.SrcRows)
		return
	}
	_, _ = fmt.Fprintf(w, "differs    %s\n", head)
	_, _ = fmt.Fprintf(w, "  rows: source %d, target %d; chunks identical %d of %d; differing leaves %d (%d not compared row by row)\n",
		d.SrcRows, d.DstRows, d.IdenticalChunks, d.Chunks, d.DifferingLeaves, d.UnresolvedLeaves)
	_, _ = fmt.Fprintf(w, "  counted: %d missing on target, %d extra on target, %d changed", d.Missing, d.Extra, d.Changed)
	if d.UnresolvedLeaves > 0 {
		_, _ = fmt.Fprint(w, " (in the leaves compared; raise --pair-budget to compare more)")
	}
	_, _ = fmt.Fprintln(w)
	if d.MaybeSpurious {
		_, _ = fmt.Fprintln(w, "  · merge-semantics engine read without FINAL: differences may vanish once both sides are merged (--final)")
	}
	const maxChunks = 8
	for i, cd := range d.Differing {
		if i == maxChunks {
			_, _ = fmt.Fprintf(w, "  … %d more differing chunks\n", len(d.Differing)-maxChunks)
			break
		}
		label := cd.Display
		switch {
		case label != "":
		case pt.Chunking.Kind == jk.ChunkingSingle:
			label = "(whole table)"
		default:
			label = cd.Id
		}
		switch {
		case cd.AbsentOnTarget:
			_, _ = fmt.Fprintf(w, "  chunk %s: absent on target (%d rows)\n", label, cd.SrcRows)
		case cd.AbsentOnSource:
			_, _ = fmt.Fprintf(w, "  chunk %s: absent on source (%d target rows)\n", label, cd.DstRows)
		default:
			_, _ = fmt.Fprintf(w, "  chunk %s: %d / %d rows, %d differing leaves\n", label, cd.SrcRows, cd.DstRows, len(cd.Leaves))
		}
	}
	for _, ex := range d.Examples {
		_, _ = fmt.Fprintf(w, "  e.g. %-7s %s\n", ex.Kind, ex.Key)
	}
}

func newSyncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "copy the plan's tables from source to target, chunk by chunk, verifying each chunk by digest",
		Description: "Re-checks the plan against both servers, then relays each chunk as a Native stream. " +
			"A chunk counts as done only when the target's digest matches; done chunks are journaled beside the plan, " +
			"so running the same command again resumes. repair acts only on what the plan's diff showed.",
		Flags: []cli.Flag{
			planFlag(true),
			&cli.StringFlag{Name: "mode", Required: true, Usage: "full, repair (the leaves the diff found) or sample"},
			&cli.StringSliceFlag{Name: "table", Usage: "source table to sync, as database.name (repeatable); default: every table ready to sync"},
			&cli.StringFlag{Name: "sample", Value: "1/100", Usage: "fraction of keys a sample copies, as num/den"},
			&cli.StringFlag{Name: "existing", Value: "refuse", Usage: "a non-empty target table under full or sample: refuse, append or replace"},
			&cli.BoolFlag{Name: "restart", Usage: "begin a new run instead of resuming the plan's"},
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be copied and cleared, and change nothing"},
			&cli.StringFlag{Name: "compression", Value: "zstd", Usage: "HTTP compression of the relayed stream: zstd, gzip or none"},
			&cli.Float64Flag{Name: "headroom", Value: 1.5, Usage: "pre-flight factor over the estimated bytes the target must have free"},
			&cli.Uint64Flag{Name: "min-free-bytes", Value: jk.DefaultFreeFloor().MinFreeBytes, Usage: "wait between chunks while a target disk has less free"},
			&cli.Float64Flag{Name: "min-free-fraction", Value: jk.DefaultFreeFloor().MinFreeFraction, Usage: "wait between chunks while a target disk has less than this share of its size free"},
		},
		Action: func(c *cli.Context) (err error) {
			ctx := c.Context
			var w io.Writer = os.Stdout
			path := c.Path("plan")
			var req jk.SyncRequest
			var compression string
			req, compression, err = parseSyncRequest(c)
			if err != nil {
				return
			}
			var plan jk.Plan
			plan, err = jk.LoadPlan(path)
			if err != nil {
				return
			}
			// Digest scans and the relay read whole tables; the default
			// client timeout is for metadata. Cancellation comes from the
			// context.
			var srcC jk.SourceI
			srcC, err = openSource(plan.Source, true)
			if err != nil {
				return
			}
			dstC := scanClient(jk.TargetClientConfig(plan.Target))
			var prep jk.SyncPrepared
			prep, err = jk.PrepareSyncStep(ctx, srcC, dstC, &plan, req, time.Now())
			if err != nil {
				return
			}
			if len(prep.Stale) > 0 {
				return refuseStale(w, path, prep.Stale)
			}
			for _, s := range prep.Skipped {
				_, _ = fmt.Fprintf(w, "skip %s\n", s)
			}
			if len(prep.Chosen) == 0 {
				_, _ = fmt.Fprintln(w, "nothing to sync")
				return
			}
			if c.Bool("dry-run") {
				printSyncPreview(w, prep.Chosen)
			}
			printPreflight(w, prep.Disks)
			if c.Bool("dry-run") {
				return
			}

			opts := jk.DefaultSyncOptions()
			opts.Compression = compression
			mon := newSyncMonitor(ctx, dstC, prep.ExpectedRows)
			mon.attach(&opts)
			w = mon.bar.LogWriter()
			floor := jk.FreeFloor{MinFreeBytes: c.Uint64("min-free-bytes"), MinFreeFraction: c.Float64("min-free-fraction"), Poll: jk.DefaultFreeFloor().Poll}
			opts.FreeFloor = &floor
			opts.OnLowDisk = func(low []jk.DiskInfo) {
				for _, d := range low {
					mon.bar.Printf("  waiting: target disk %s has %s free, below the floor; free space or Ctrl-C (the run resumes later)\n",
						d.Name, progressest.FormatBytes(int64(d.FreeSpace)))
				}
			}
			opts.BeforeTable = func(pt *jk.PlanTable) {
				mon.beginTable(pt)
				_, _ = fmt.Fprintf(w, "%s → %s (%s)\n", pt.Source, pt.Target, pt.Sync.Mode)
			}
			opts.AfterTable = func(pt *jk.PlanTable) {
				rep := pt.SyncReport
				_, _ = fmt.Fprintf(w, "  %d copied, %d already done, %d identical, %d stale, %d failed; %d rows, %s in %s\n",
					rep.Copied, rep.Done, rep.Identical, rep.Stale, rep.Failed, rep.Rows, progressest.FormatBytes(int64(rep.Bytes)),
					progressest.FormatDuration(mon.tableTook()))
			}
			opts.Progress = func(r jk.ChunkResult) {
				_, _ = fmt.Fprintln(w, chunkLine(r))
			}
			var out jk.SyncOutcome
			out, err = jk.RunSync(ctx, srcC, dstC, &prep, path, opts, time.Now)
			mon.stop()
			if out.Run.RunId != "" {
				verb := "began"
				if out.Resumed {
					verb = "resumed"
				}
				_, _ = fmt.Fprintf(os.Stdout, "run %s %s\n", out.Run.RunId, verb)
			}
			if err != nil {
				return
			}
			targets := make([]datacatalog.TableRef, 0, len(prep.Chosen))
			for _, pt := range prep.Chosen {
				targets = append(targets, pt.Target)
			}
			if after, derr := jk.ReadDisks(ctx, dstC, targets); derr == nil {
				printFootprints(os.Stdout, prep.Chosen, &after)
			}
			_, _ = fmt.Fprintf(os.Stdout, "\nplan updated in %s; run diff to compare the result\n", path)
			if out.Failed > 0 {
				return eb.Build().Int("chunks", out.Failed).Errorf("some chunks were not synced")
			}
			return
		},
	}
}

// parseSyncRequest reads the sync command's flags.
func parseSyncRequest(c *cli.Context) (req jk.SyncRequest, compression string, err error) {
	err = req.Mode.UnmarshalText([]byte(c.String("mode")))
	if err != nil {
		return
	}
	err = req.Existing.UnmarshalText([]byte(c.String("existing")))
	if err != nil {
		return
	}
	if req.Mode == jk.SyncModeSample {
		req.SampleNum, req.SampleDen, err = jk.ParseFraction(c.String("sample"))
		if err != nil {
			return
		}
	}
	req.Only, err = parseRefs(c.StringSlice("table"))
	if err != nil {
		return
	}
	req.Restart = c.Bool("restart")
	req.Chunking = jk.DefaultChunkingOptions()
	req.Headroom = c.Float64("headroom")
	compression, err = jk.ParseCompression(c.String("compression"))
	return
}

// chunkLine is one chunk's outcome as the sync prints it.
func chunkLine(r jk.ChunkResult) (line string) {
	label := r.Display
	if label == "" {
		label = r.Chunk
	}
	line = fmt.Sprintf("  %-9s %s  %d rows, %s", r.Status, label, r.Rows, progressest.FormatBytes(int64(r.Bytes)))
	if r.Cleared > 0 {
		line += fmt.Sprintf(", cleared %d", r.Cleared)
	}
	if r.Attempts > 1 {
		line += fmt.Sprintf(", %d attempts", r.Attempts)
	}
	if r.Note != "" {
		line += " — " + r.Note
	}
	return
}

// syncMonitor owns the sync's progress bar: the rows and bytes counters the
// engine advances, the feed that moves the bar, and the target's free space,
// read every few seconds off the relay's path.
type syncMonitor struct {
	bar        *progressbar.Bar
	rows       atomic.Int64
	bytes      atomic.Int64
	current    atomic.Pointer[string]
	freeBytes  atomic.Uint64
	started    time.Time
	tableStart time.Time
	stop       func()
}

func newSyncMonitor(ctx context.Context, dst jk.QueryI, expectedRows int64) (m *syncMonitor) {
	m = &syncMonitor{bar: progressbar.New(expectedRows, "rows"), started: time.Now()}
	m.bar.SetDetail(func(processed int64, total int64) string {
		detail := ""
		if t := m.current.Load(); t != nil {
			detail = *t
		}
		if el := time.Since(m.started).Seconds(); el > 0 {
			if rate := progressest.FormatRate(float64(m.bytes.Load())/el, "bytes"); rate != "" {
				detail += "  " + rate + " on the wire"
			}
		}
		if f := m.freeBytes.Load(); f > 0 {
			detail += "  target free " + progressest.FormatBytes(int64(f))
		}
		return detail
	})
	stopFeed := make(chan struct{})
	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		var last int64
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		disks := time.NewTicker(5 * time.Second)
		defer disks.Stop()
		for {
			select {
			case <-stopFeed:
				m.bar.Add(m.rows.Load() - last)
				return
			case <-t.C:
				n := m.rows.Load()
				m.bar.Add(n - last)
				last = n
			case <-disks.C:
				if rep, rerr := jk.ReadDisks(ctx, dst, nil); rerr == nil {
					var free uint64
					for _, d := range rep.Disks {
						free += d.FreeSpace
					}
					m.freeBytes.Store(free)
				}
			}
		}
	}()
	m.bar.Start(ctx)
	var once sync.Once
	m.stop = func() {
		once.Do(func() {
			close(stopFeed)
			<-feedDone
			m.bar.Stop()
		})
	}
	return
}

// attach gives the engine the counters the bar reads.
func (inst *syncMonitor) attach(opts *jk.SyncOptions) {
	opts.Rows, opts.Bytes = &inst.rows, &inst.bytes
}

func (inst *syncMonitor) beginTable(pt *jk.PlanTable) {
	label := pt.Source.String()
	inst.current.Store(&label)
	inst.tableStart = time.Now()
}

func (inst *syncMonitor) tableTook() (d time.Duration) {
	return time.Since(inst.tableStart)
}

func printSyncPreview(w io.Writer, tables []*jk.PlanTable) {
	for _, pt := range tables {
		if pt.Sync.Mode != jk.SyncModeRepair {
			_, _ = fmt.Fprintf(w, "%s → %s: %s, existing rows: %s, %s chunking\n", pt.Source, pt.Target, pt.Sync.Mode, pt.Sync.Existing, pt.Chunking.Kind)
			_, _ = fmt.Fprintf(w, "  copies about %d of %d rows\n", uint64(float64(pt.Rows)*jk.ExpectedCopyFraction(pt)), pt.Rows)
			continue
		}
		_, _ = fmt.Fprintf(w, "%s → %s: repair, %s chunking\n", pt.Source, pt.Target, pt.Chunking.Kind)
		_, _ = fmt.Fprintf(w, "  %d differing chunks: clears %d target rows, copies %d source rows (as of the diff at %s)\n",
			len(pt.Diff.Differing), jk.RepairClearRows(pt.Diff), jk.RepairCopyRows(pt.Diff), pt.Diff.ComputedAt.Format(time.RFC3339))
	}
}

func printPreflight(w io.Writer, groups []jk.PreflightDisk) {
	_, _ = fmt.Fprintln(w, "pre-flight (target free space):")
	for _, g := range groups {
		verdict := "ok"
		if !g.OK {
			verdict = "WARNING: may not fit"
		}
		held := ""
		if g.Held > 0 {
			held = ", plus " + progressest.FormatBytes(int64(g.Held)) + " cleared but held until merges"
		}
		_, _ = fmt.Fprintf(w, "  disks %s: need about %s%s (%s with headroom), %s free — %s\n",
			strings.Join(g.Disks, ","), progressest.FormatBytes(int64(g.Need)), held, progressest.FormatBytes(int64(g.Headroom)),
			progressest.FormatBytes(int64(g.Free)), verdict)
	}
}

func printFootprints(out io.Writer, tables []*jk.PlanTable, rep *jk.DiskReport) {
	// Rendered whole, then written once: the sync's writer is the progress
	// bar's log writer, which ends every write with a newline.
	var buf strings.Builder
	w := &buf
	defer func() { _, _ = io.WriteString(out, buf.String()) }()
	_, _ = fmt.Fprintln(w, "\ntarget footprint (active parts; rows a sync cleared hold space until their parts merge):")
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  TABLE\tSOURCE\tTARGET\tPARTS\tDISKS")
	for _, pt := range tables {
		fp, _ := rep.Table(pt.Target)
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%d\t%s\n", pt.Target, progressest.FormatBytes(int64(pt.Bytes)),
			progressest.FormatBytes(int64(fp.BytesOnDisk)), fp.Parts, strings.Join(fp.Disks, ","))
	}
	_ = tw.Flush()
	for _, d := range rep.Disks {
		_, _ = fmt.Fprintf(w, "  disk %s: %s free of %s\n", d.Name, progressest.FormatBytes(int64(d.FreeSpace)), progressest.FormatBytes(int64(d.TotalSpace)))
	}
}

func newStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "show where a plan stands: structure, diff, and the sync run's journal",
		Description: "Reads the plan and its journal without contacting a server. " +
			"With --disk it also reads the target's disks and the footprint of the plan's tables.",
		Flags: []cli.Flag{
			planFlag(true),
			&cli.BoolFlag{Name: "disk", Usage: "also read the target's disks and table footprints"},
		},
		Action: func(c *cli.Context) (err error) {
			w := os.Stdout
			path := c.Path("plan")
			var plan jk.Plan
			plan, err = jk.LoadPlan(path)
			if err != nil {
				return
			}
			_, _ = fmt.Fprintf(w, "plan %s, written %s\n", path, plan.CreatedAt.Format(time.RFC3339))
			_, _ = fmt.Fprintf(w, "source %s (ClickHouse %s) → target %s (ClickHouse %s)\n",
				plan.Source.Label(), plan.SourceServer.Version, plan.Target.URL, plan.TargetServer.Version)
			var j *jk.Journal
			if plan.SyncRun != nil {
				_, _ = fmt.Fprintf(w, "sync run %s, begun %s\n", plan.SyncRun.RunId, plan.SyncRun.StartedAt.Format(time.RFC3339))
				j, err = jk.ReadJournal(jk.JournalPath(path), plan.SyncRun.RunId)
				if err != nil {
					return
				}
			}
			_, _ = fmt.Fprintln(w)
			tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "TABLE\tVERDICT\tDIFF\tSYNC\tJOURNAL")
			for i := range plan.Tables {
				pt := &plan.Tables[i]
				diff := "—"
				if pt.Diff != nil {
					if pt.Diff.IsIdentical() {
						diff = "identical"
					} else {
						diff = fmt.Sprintf("%d missing, %d extra, %d changed", pt.Diff.Missing, pt.Diff.Extra, pt.Diff.Changed)
						if pt.Diff.UnresolvedLeaves > 0 {
							diff += fmt.Sprintf(" (+%d leaves uncounted)", pt.Diff.UnresolvedLeaves)
						}
					}
				}
				sync := "—"
				if pt.Sync != nil {
					sync = pt.Sync.Mode.String()
					if r := pt.SyncReport; r != nil {
						sync += fmt.Sprintf(": %d copied, %d stale, %d failed", r.Copied+r.Done, r.Stale, r.Failed)
					}
				}
				journal := "—"
				if j != nil {
					if n, rows := j.DoneChunks(pt.Source.String()); n > 0 {
						journal = fmt.Sprintf("%d chunks, %d rows verified", n, rows)
					}
				}
				_, _ = fmt.Fprintf(tw, "%s → %s\t%s\t%s\t%s\t%s\n", pt.Source, pt.Target, pt.Verdict, diff, sync, journal)
			}
			_ = tw.Flush()
			for i := range plan.Tables {
				if r := plan.Tables[i].SyncReport; r != nil {
					for _, p := range r.Problems {
						_, _ = fmt.Fprintf(w, "  %s: %s\n", plan.Tables[i].Source, p)
					}
				}
			}
			if !c.Bool("disk") {
				return
			}
			refs := make([]datacatalog.TableRef, 0, len(plan.Tables))
			tables := make([]*jk.PlanTable, 0, len(plan.Tables))
			for i := range plan.Tables {
				if plan.Tables[i].Verdict.IsSyncable() {
					refs = append(refs, plan.Tables[i].Target)
					tables = append(tables, &plan.Tables[i])
				}
			}
			var rep jk.DiskReport
			rep, err = jk.ReadDisks(c.Context, chclient.New(jk.TargetClientConfig(plan.Target), nil), refs)
			if err != nil {
				return
			}
			printFootprints(w, tables, &rep)
			return
		},
	}
}
