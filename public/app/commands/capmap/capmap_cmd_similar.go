package capmap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapsimilarity"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// similarCommand is `boxer capmap similar`: rank every competence's nearest
// neighbours by the compression distance of their prose and record them as
// `similar:` frontmatter in the vault (ADR-0168 §SD11's rule — the CLI is the
// mutation surface — applied to the one derived fact the corpus carries).
//
// It writes to the vault, not to the store: the vault is authoritative (§SD3),
// so a ranking that only reached `boxer.facts` would be lost at the next load,
// and one written into the notes is reviewed as a diff like any other edit and
// then loaded like any other frontmatter. `--dry-run` with `--out` is the way
// to look before writing.
func similarCommand() *cli.Command {
	return &cli.Command{
		Name:  "similar",
		Usage: "rank each competence's nearest neighbours by the compression distance of their prose, and record them as `similar:` frontmatter",
		Flags: []cli.Flag{
			vaultFlag(),
			&cli.Float64Flag{Name: "threshold", Value: capmapsimilarity.DefaultThreshold,
				Usage: "keep a pair when its NCD is at most this (0 identical, 1 nothing shared)"},
			&cli.IntFlag{Name: "top", Value: capmapsimilarity.DefaultTop, Usage: "neighbours recorded per competence"},
			&cli.BoolFlag{Name: "cross", Usage: "compare across catalogs rather than within one"},
			&cli.BoolFlag{Name: "dry-run", Usage: "rank, report, and write nothing into the vault"},
			&cli.StringFlag{Name: "out", Usage: "write the ranking as a JSON report to this file"},
			&cli.IntFlag{Name: "workers", Usage: "goroutines for the all-pairs pass; 0 uses every CPU"},
		},
		Action: actionSimilar,
	}
}

func actionSimilar(c *cli.Context) (err error) {
	var (
		corpus capmapcorpus.Corpus
		dir    string
	)
	if corpus, dir, err = readVault(c); err != nil {
		return err
	}
	var res capmapsimilarity.Result
	if res, err = capmapsimilarity.Rank(corpus, capmapsimilarity.Options{
		Threshold: c.Float64("threshold"),
		Top:       c.Int("top"),
		Cross:     c.Bool("cross"),
		Workers:   c.Int("workers"),
	}); err != nil {
		return err
	}

	written, unchanged := 0, 0
	if !c.Bool("dry-run") {
		if written, unchanged, err = writeSimilar(dir, corpus, res); err != nil {
			return err
		}
	}

	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(out, "vault\t%s\n", dir)
	fmt.Fprintf(out, "competences\t%d\n", len(corpus.Competences))
	fmt.Fprintf(out, "  compared\t%d\n", len(res.Entries))
	fmt.Fprintf(out, "  without prose\t%d\n", res.Unwritten)
	fmt.Fprintf(out, "pairs measured\t%d\n", res.Compared)
	fmt.Fprintf(out, "  under threshold %.2f\t%d\n", res.Threshold, res.Kept)
	if c.Bool("dry-run") {
		fmt.Fprintf(out, "notes written\t0 (dry run)\n")
	} else {
		fmt.Fprintf(out, "notes written\t%d\n", written)
		fmt.Fprintf(out, "notes unchanged\t%d\n", unchanged)
	}
	_ = out.Flush()
	reportNearest(res)

	if path := c.String("out"); path != "" {
		var data []byte
		if data, err = json.MarshalIndent(res, "", "  "); err != nil {
			return eh.Errorf("unable to encode the report: %w", err)
		}
		if err = os.WriteFile(path, data, 0o644); err != nil {
			return eh.Errorf("unable to write the report to %q: %w", path, err)
		}
		fmt.Printf("\nreport written to %s\n", path)
	}
	return nil
}

// writeSimilar records each compared competence's neighbours in its note,
// touching only the notes whose stanza actually changes. A competence the
// ranker left out — no prose — is not written either: its note has nothing to
// say about resemblance, and an earlier run cannot have given it a stanza.
func writeSimilar(dir string, corpus capmapcorpus.Corpus, res capmapsimilarity.Result) (written, unchanged int, err error) {
	dirBacked := make(map[string]bool, len(corpus.Competences))
	for _, comp := range corpus.Competences {
		dirBacked[comp.Slug] = comp.DirectoryBacked()
	}
	for _, e := range res.Entries {
		entries := make([]capmapcorpus.SimilarEntry, 0, len(e.Similar))
		for _, n := range e.Similar {
			entries = append(entries, capmapcorpus.SimilarEntry{Target: n.Slug, Ncd: n.Ncd, Qualified: dirBacked[n.Slug]})
		}
		path := filepath.Join(dir, e.VaultPath)
		var content, out []byte
		if content, err = os.ReadFile(path); err != nil {
			return written, unchanged, eb.Build().Str("path", path).Errorf("unable to read: %w", err)
		}
		var changed bool
		if out, changed, err = capmapcorpus.UpsertSimilar(content, entries); err != nil {
			return written, unchanged, eb.Build().Str("path", path).Errorf("unable to update: %w", err)
		}
		if !changed {
			unchanged++
			continue
		}
		if err = os.WriteFile(path, out, 0o644); err != nil {
			return written, unchanged, eb.Build().Str("path", path).Errorf("unable to write: %w", err)
		}
		written++
	}
	return written, unchanged, nil
}

// reportNearest prints the closest pairs found, nearest first, so a run's
// output shows what the threshold is admitting without opening the report.
func reportNearest(res capmapsimilarity.Result) {
	type shown struct {
		a, b string
		ncd  float64
	}
	seen := make(map[[2]string]struct{}, res.Kept)
	pairs := make([]shown, 0, res.Kept)
	for _, e := range res.Entries {
		for _, n := range e.Similar {
			key := [2]string{min(e.Slug, n.Slug), max(e.Slug, n.Slug)}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			pairs = append(pairs, shown{a: key[0], b: key[1], ncd: n.Ncd})
		}
	}
	if len(pairs) == 0 {
		fmt.Println("\nno pairs under the threshold")
		return
	}
	sortShown := func(x, y int) bool {
		if pairs[x].ncd != pairs[y].ncd {
			return pairs[x].ncd < pairs[y].ncd
		}
		if pairs[x].a != pairs[y].a {
			return pairs[x].a < pairs[y].a
		}
		return pairs[x].b < pairs[y].b
	}
	sort.Slice(pairs, sortShown)
	fmt.Printf("\nnearest pairs, showing %d of %d:\n", min(len(pairs), maxReported), len(pairs))
	for i, p := range pairs {
		if i >= maxReported {
			break
		}
		fmt.Printf("  %.4f  %s ~ %s\n", p.ncd, p.a, p.b)
	}
}
