package jackstay

import (
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/hmi/progressest"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func newExportCommand() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "write the selected source tables into a pack directory, for a sync on a host that cannot reach the source (ADR-0271)",
		Description: "Each chunk is stored as the source's Native response, compressed as the server sent it, and recorded in the pack's " +
			"manifest with its digest only when the source did not change while it was read. Running the same command again resumes. " +
			"On the other host: boxer jackstay structure --pack DIR --target … --plan plan.json, then apply-ddl, sync --mode full, diff.",
		Flags: []cli.Flag{
			endpointFlags()[0],
			endpointFlags()[1],
			&cli.PathFlag{Name: "pack", Required: true, Usage: "pack directory to write (created when missing)"},
			&cli.StringSliceFlag{Name: "database", Usage: "source database to include (repeatable); default: every non-system database"},
			&cli.BoolFlag{Name: "leeway-only", Usage: "export only the tables that classify as leeway (ADR-0170)"},
			filterFlag(),
			&cli.StringFlag{Name: "sample", Usage: "export only this fraction of each table's keys, as num/den"},
			&cli.StringFlag{Name: "compression", Value: "zstd", Usage: "compression of the stored chunks: zstd, gzip or none"},
			&cli.BoolFlag{Name: "restart", Usage: "discard the pack's earlier export and begin a new one"},
		},
		Action: func(c *cli.Context) (err error) {
			ctx := c.Context
			w := os.Stdout
			req := jk.ExportRequest{
				Selection: jk.Selection{Databases: c.StringSlice("database"), LeewayOnly: c.Bool("leeway-only")},
				Restart:   c.Bool("restart"),
				Chunking:  jk.DefaultChunkingOptions(),
			}
			req.Selection.Filters, err = parseFilters(c.StringSlice("filter"))
			if err != nil {
				return
			}
			if s := c.String("sample"); s != "" {
				req.SampleNum, req.SampleDen, err = parseFraction(s)
				if err != nil {
					return
				}
			}
			switch req.Compression = c.String("compression"); req.Compression {
			case "zstd", "gzip":
			case "none":
				req.Compression = ""
			default:
				return eb.Build().Str("compression", req.Compression).Errorf("expected zstd, gzip or none")
			}
			req.BeforeTable = func(t *jk.PackTable) {
				_, _ = fmt.Fprintf(w, "%s (%s chunking)\n", t.Source, t.Chunking.Kind)
				if t.Filter != "" {
					_, _ = fmt.Fprintf(w, "  filter: %s\n", t.Filter)
				}
			}
			req.Progress = func(r jk.ChunkResult) {
				_, _ = fmt.Fprintln(w, chunkLine(r))
			}
			srcEp := jk.SourceEndpoint()
			dir := c.Path("pack")
			var out jk.ExportOutcome
			out, err = jk.Export(ctx, scanClient(jk.SourceClientConfig(srcEp)), srcEp, dir, req, time.Now)
			for _, s := range out.Skipped {
				_, _ = fmt.Fprintf(w, "skip %s\n", s)
			}
			if err != nil {
				return
			}
			var rows, bytes uint64
			for i := range out.Manifest.Tables {
				t := &out.Manifest.Tables[i]
				rows += t.Rows()
				for j := range t.Chunks {
					bytes += t.Chunks[j].Bytes
				}
			}
			verb := "began"
			if out.Resumed {
				verb = "resumed"
			}
			_, _ = fmt.Fprintf(w, "\nexport %s %s: %d tables, %d rows, %s in %s\n", out.Manifest.ExportId, verb,
				len(out.Manifest.Tables), rows, progressest.FormatBytes(int64(bytes)), dir)
			if out.Failed > 0 {
				return eb.Build().Int("chunks", out.Failed).Errorf("some chunks were not exported; run the export again to finish the pack")
			}
			_, _ = fmt.Fprintf(w, "the pack is complete; on the target's host: boxer jackstay structure --pack %s --target … --plan plan.json\n", dir)
			return
		},
	}
}
