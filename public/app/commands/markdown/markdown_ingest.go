package markdown

import (
	"context"
	"fmt"
	"os"
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// newIngestCommand is `boxer markdown ingest`: every markdown file under the
// arguments becomes one document row plus its item rows in `boxer.facts`.
//
// The store is never provisioned here — chstore owns the facts table
// (ADR-0184 §SD2) — so a host without the schema is reported, not repaired.
// ClickHouse is reached through the registered CLICKHOUSE_* variables; no
// new configuration surface.
func newIngestCommand() *cli.Command {
	return &cli.Command{
		Name:      "ingest",
		Usage:     "read markdown files into boxer.facts as document, heading, code, link, emphasis, tag and frontmatter rows",
		ArgsUsage: "<file | dir>...",
		Description: "Each .md file becomes one mdDoc row, one row per heading, fenced code block, " +
			"link, emphasised span and tag, and one frontmatter row carrying the YAML block " +
			"as typed (path, params, value) leaves. Files under a directory are stored under " +
			"their path relative to it. Every ingest writes new rows: the document's content " +
			"hash is the natural key that ties re-ingests of identical text together. " +
			"Needs CLICKHOUSE_ENDPOINT (and the other CLICKHOUSE_* variables) pointing at a " +
			"host where the facts schema has been provisioned.",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "batch", Value: 64,
				Usage: "flush to ClickHouse after this many documents"},
			&cli.BoolFlag{Name: "dry-run",
				Usage: "extract and count the rows without connecting or writing"},
			&cli.DurationFlag{Name: "timeout", Value: 10 * time.Minute,
				Usage: "bound on the whole run: connect, verify, ingest, flush"},
		},
		Action: runIngest,
	}
}

func runIngest(c *cli.Context) (err error) {
	if c.NArg() == 0 {
		return eh.Errorf("at least one file or directory is required")
	}
	sources, err := walkSources(c.Args().Slice())
	if err != nil {
		return
	}
	batch := c.Int("batch")
	if batch < 1 {
		batch = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Duration("timeout"))
	defer cancel()

	var store *mddocfacts.MddocStore
	if !c.Bool("dry-run") {
		client := chclient.New(chclient.ConfigFromEnv(), nil)
		if err = client.Ping(ctx); err != nil {
			return eh.Errorf("ping clickhouse: %w", err)
		}
		var exec *storeexec.Executor
		exec, err = storeexec.New(client, nil)
		if err != nil {
			return
		}
		store = mddocfacts.NewMddocStore(exec, nil, mddocfacts.MddocStoreConfig{})
		defer store.Close()
		if err = store.VerifySchema(ctx); err != nil {
			return eh.Errorf("verify facts schema (chstore provisions it; this command never does): %w", err)
		}
	}

	ts := time.Now().UTC()
	var docs, rows, pending int
	for _, s := range sources {
		var src []byte
		src, err = os.ReadFile(s.path)
		if err != nil {
			return eb.Build().Str("path", s.path).Errorf("read: %w", err)
		}
		var r mddocfacts.Rows
		if store == nil {
			r = mddocfacts.BuildRows(src, s.name, ts, mdextract.Extract(src))
		} else {
			r, err = store.IngestDocument(src, s.name, ts)
			if err != nil {
				return eb.Build().Str("path", s.path).Errorf("ingest: %w", err)
			}
		}
		docs++
		rows += r.Count()
		pending++
		if store != nil && pending >= batch {
			if _, err = store.Flush(ctx); err != nil {
				return eh.Errorf("flush: %w", err)
			}
			pending = 0
		}
	}
	if store != nil && pending > 0 {
		if _, err = store.Flush(ctx); err != nil {
			return eh.Errorf("flush: %w", err)
		}
	}
	verb := "ingested"
	if store == nil {
		verb = "would ingest"
	}
	_, err = fmt.Fprintf(c.App.Writer, "%s %d documents as %d rows\n", verb, docs, rows)
	return
}
