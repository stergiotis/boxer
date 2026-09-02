// Command jsonbench is the solution artifact of the jsonbench-on-facts trial
// (doc/trials/jsonbench-on-facts/). It shreds JSONBench's Bluesky Jetstream
// corpus into a boxer.facts-shaped ClickHouse table using the canonical
// leeway JSON mapping scheme, so the same five benchmark queries can be run
// against the facts data model and compared with ClickHouse's native JSON
// type.
//
// It is deliberately trial-local. Nothing here is promoted into the leeway
// packages: the gaps it works around are filed as findings in the trial
// logbook, not patched into the toolbelt.
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
)

func main() {
	app := &cli.App{
		Name:    "jsonbench",
		Usage:   "shred the JSONBench Bluesky corpus into a boxer.facts-shaped table",
		Version: vcs.BuildVersionInfo(),
		Flags:   logging.LoggingFlags,
		Commands: []*cli.Command{
			chpackCommand(),
			ddlCommand(),
			ingestCommand(),
			jsonmapCommand(),
			resolveCommand(),
			resultsCommand(),
			vocabCommand(),
		},
		Before: logging.Apply,
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("jsonbench failed")
	}
}

func ingestCommand() *cli.Command {
	return &cli.Command{
		Name:  "ingest",
		Usage: "shred N gzipped Jetstream files into a facts table",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8123/"},
			&cli.StringFlag{Name: "user", Value: "default"},
			&cli.StringFlag{Name: "password"},
			&cli.StringFlag{Name: "database", Required: true},
			&cli.StringFlag{Name: "table", Value: "facts"},
			&cli.StringFlag{Name: "data-dir", Required: true},
			&cli.IntFlag{Name: "files", Value: 1, Usage: "tier: number of file_*.json.gz to ingest"},
			&cli.IntFlag{Name: "batch", Value: 50000, Usage: "rows per Arrow record batch"},
			&cli.IntFlag{Name: "sample", Value: 20000, Usage: "docs sampled to decide symbol vs string routing"},
			&cli.Float64Flag{Name: "symbol-ratio", Value: 0.01, Usage: "distinct/total below which a path routes to the symbol section"},
			&cli.IntFlag{Name: "limit", Value: 0, Usage: "stop after N documents (0 = no limit)"},
		},
		Action: runIngest,
	}
}

func runIngest(cCtx *cli.Context) (err error) {
	dataDir := cCtx.String("data-dir")
	nFiles := cCtx.Int("files")
	files, err := tierFiles(dataDir, nFiles)
	if err != nil {
		return
	}

	// Pass 1 — decide which paths carry low-cardinality strings. The
	// canonical leeway scheme splits `string` from `symbol` on exactly this
	// judgement and notes it "can be inferred appreciatively from sample
	// data"; this is that inference, made explicit and recorded.
	log.Info().Int("sample", cCtx.Int("sample")).Msg("sampling for symbol routing")
	symbolPaths, err := sampleSymbolPaths(files[0], cCtx.Int("sample"), cCtx.Float64("symbol-ratio"))
	if err != nil {
		return
	}
	routed := make([]string, 0, len(symbolPaths))
	for p := range symbolPaths {
		routed = append(routed, p)
	}
	sort.Strings(routed)
	log.Info().Strs("paths", routed).Msg("routing to the symbol section")
	// The sampling pass shares eachDoc's skip counter; zero it so the reported
	// figure covers the ingest only.
	undecodable = 0

	cli0 := chclient.New(chclient.Config{
		URL:      cCtx.String("url"),
		User:     cCtx.String("user"),
		Password: cCtx.String("password"),
	}, nil)
	table := cCtx.String("database") + "." + cCtx.String("table")

	ing := &ingester{
		cli:         cli0,
		table:       table,
		alloc:       memory.NewGoAllocator(),
		batch:       cCtx.Int("batch"),
		symbolPaths: symbolPaths,
		limit:       uint64(cCtx.Int("limit")),
	}
	start := time.Now()
	for _, f := range files {
		log.Info().Str("file", filepath.Base(f)).Msg("ingesting")
		err = ing.ingestFile(cCtx.Context, f)
		if err != nil {
			return
		}
	}
	err = ing.flush(cCtx.Context)
	if err != nil {
		return
	}
	elapsed := time.Since(start)
	log.Info().
		Uint64("docs", ing.docs).
		Uint64("attributes", ing.attrs).
		Uint64("nullsDropped", ing.shr.nulls).
		Uint64("undecodableSkipped", undecodable).
		Dur("elapsed", elapsed).
		Float64("docsPerSec", float64(ing.docs)/elapsed.Seconds()).
		Msg("ingest complete")
	// Machine-readable line for the run directory.
	fmt.Printf("docs=%d attributes=%d nulls_dropped=%d undecodable_skipped=%d elapsed_s=%.3f docs_per_s=%.0f\n",
		ing.docs, ing.attrs, ing.shr.nulls, undecodable, elapsed.Seconds(), float64(ing.docs)/elapsed.Seconds())
	return
}

func tierFiles(dir string, n int) (out []string, err error) {
	matches, err := filepath.Glob(filepath.Join(dir, "file_*.json.gz"))
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("glob: %w", err)
		return
	}
	sort.Strings(matches)
	if len(matches) < n {
		err = eb.Build().Int("need", n).Str("dir", dir).Int("have", len(matches)).Errorf("the tier needs more files than the directory holds")
		return
	}
	out = matches[:n]
	return
}

// sampleSymbolPaths reads the first `n` documents and returns the set of paths
// whose distinct-value ratio is below `ratio` — the paths worth putting in the
// dictionary-encoded symbol section rather than the plain string section.
func sampleSymbolPaths(file string, n int, ratio float64) (out map[string]struct{}, err error) {
	out = make(map[string]struct{}, 16)
	if n <= 0 {
		return
	}
	distinct := make(map[string]map[string]struct{}, 32)
	total := make(map[string]int, 32)
	shr := &shredder{}
	err = eachDoc(file, uint64(n), func(_ []byte, doc map[string]any) error {
		for _, s := range shr.shred(doc) {
			if s.kind != valueKindString {
				continue
			}
			total[s.path]++
			d, ok := distinct[s.path]
			if !ok {
				d = make(map[string]struct{}, 64)
				distinct[s.path] = d
			}
			// Bound the memory a genuinely high-cardinality path can cost.
			if len(d) < 4096 {
				d[s.s] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	for p, t := range total {
		if t < 64 {
			continue // too thin to judge
		}
		if float64(len(distinct[p]))/float64(t) < ratio {
			out[p] = struct{}{}
		}
	}
	return
}

// undecodable counts documents skipped because they would not parse. The
// Bluesky corpus contains a few genuinely malformed JSON-lines records —
// file 5 line 91840 is truncated mid-string at exactly 65,536 bytes, its
// remainder continuing on the next physical line — and the reference arm
// tolerates them: upstream's loader retries a failed file with
// `input_format_allow_errors_num`/`_ratio` wide open, which skips the
// offending rows (upstream/PIN.md § Run discipline). The facts arm has to
// tolerate the same documents or the two arms are not holding the same
// corpus. The count is always reported, so a skip is never silent.
var undecodable uint64

// eachDoc streams a gzipped JSON-lines file, decoding at most `limit`
// documents (0 = all). Documents that fail to decode are counted in
// [undecodable] and skipped.
//
// The callback receives the raw line beside the decoded document because the
// canonical-JSON-mapping arm identifies a row by the blake3 hash of the
// document's own bytes (its `id:blake3hash` plain value). The slice is only
// valid for the duration of the call — the scanner reuses its buffer.
func eachDoc(file string, limit uint64, fn func(raw []byte, doc map[string]any) error) (err error) {
	f, err := os.Open(file)
	if err != nil {
		err = eb.Build().Str("file", file).Errorf("open: %w", err)
		return
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		err = eb.Build().Str("file", file).Errorf("gzip: %w", err)
		return
	}
	defer func() { _ = gz.Close() }()
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var seen, line uint64
	for sc.Scan() {
		if limit > 0 && seen >= limit {
			return
		}
		line++
		var doc map[string]any
		if uerr := json.Unmarshal(sc.Bytes(), &doc); uerr != nil {
			undecodable++
			log.Warn().Err(uerr).Str("file", filepath.Base(file)).
				Uint64("line", line).Msg("skipping undecodable document")
			continue
		}
		if err = fn(sc.Bytes(), doc); err != nil {
			return
		}
		seen++
	}
	if err = sc.Err(); err != nil {
		err = eb.Build().Str("file", file).Errorf("scan: %w", err)
	}
	return
}

type ingester struct {
	cli         *chclient.Client
	table       string
	alloc       memory.Allocator
	batch       int
	symbolPaths map[string]struct{}
	limit       uint64

	ent  *dml.InEntityFacts
	shr  shredder
	held int

	docs  uint64
	attrs uint64
}

// errStopIngest unwinds eachDoc when --limit is reached. Without it the
// reader would keep decoding the rest of the file for nothing, which silently
// inflates every throughput number the trial records.
var errStopIngest = eh.Errorf("ingest limit reached")

func (inst *ingester) ingestFile(ctx context.Context, file string) (err error) {
	err = eachDoc(file, 0, func(_ []byte, doc map[string]any) error {
		if inst.limit > 0 && inst.docs >= inst.limit {
			return errStopIngest
		}
		return inst.add(ctx, doc)
	})
	if errors.Is(err, errStopIngest) {
		err = nil
	}
	return
}

func (inst *ingester) add(ctx context.Context, doc map[string]any) (err error) {
	if inst.ent == nil {
		inst.ent = dml.NewInEntityFacts(inst.alloc, inst.batch)
	}
	triples := inst.shr.shred(doc)

	// The row's identity and time. `ts` is the plain timestamp column the
	// facts table is sorted by, taken from the event's own microsecond epoch
	// so arm B is keyed exactly the way the live store is.
	inst.docs++
	id := inst.docs
	nk := blake3.Sum256(nil)
	ts := time.Unix(0, 0).UTC()
	if v, ok := doc["time_us"].(float64); ok {
		ts = time.UnixMicro(int64(v)).UTC()
	}
	if v, ok := doc["did"].(string); ok {
		nk = blake3.Sum256([]byte(v))
	}

	inst.ent.BeginEntity().SetId(id, nk[:16]).SetTimestamp(ts)

	// Kind tag: one symbol attribute carrying the row's fact kind, exactly
	// as every other boxer.facts writer does.
	sym := inst.ent.GetSectionSymbol()
	sym.BeginAttribute("blueskyEvent").
		AddMembershipLowCardRef(MembKindBlueskyEvent.GetId().Value()).EndAttribute()

	// Bucket the triples by target section before emitting: a section's
	// BeginAttribute/EndSection pair may be entered only once per entity.
	var (
		strs   []shredded
		syms   []shredded
		ints   []shredded
		floats []shredded
		bools  []shredded
	)
	for _, t := range triples {
		switch t.kind {
		case valueKindString:
			if _, ok := inst.symbolPaths[t.path]; ok {
				syms = append(syms, t)
				continue
			}
			strs = append(strs, t)
		case valueKindInt:
			ints = append(ints, t)
		case valueKindFloat:
			floats = append(floats, t)
		case valueKindBool:
			bools = append(bools, t)
		case valueKindNull:
			// counted in the shredder; facts has no section for it
		}
	}
	inst.attrs += uint64(len(strs) + len(syms) + len(ints) + len(floats) + len(bools))

	for _, t := range syms {
		a := sym.BeginAttribute(t.s)
		err = addPathMemberships(a.AddMembershipMixedLowCardRef, t)
		a.EndAttribute()
		if err != nil {
			return
		}
	}
	sym.EndSection()

	if len(strs) > 0 {
		sec := inst.ent.GetSectionStringArray()
		for _, t := range strs {
			a := sec.BeginAttributeSingle(t.s)
			err = addPathMemberships(a.AddMembershipMixedLowCardRef, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}
	if len(ints) > 0 {
		sec := inst.ent.GetSectionI64Array()
		for _, t := range ints {
			a := sec.BeginAttributeSingle(t.i)
			err = addPathMemberships(a.AddMembershipMixedLowCardRef, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}
	if len(floats) > 0 {
		sec := inst.ent.GetSectionF64Array()
		for _, t := range floats {
			a := sec.BeginAttributeSingle(t.f)
			err = addPathMemberships(a.AddMembershipMixedLowCardRef, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}
	if len(bools) > 0 {
		sec := inst.ent.GetSectionBool()
		for _, t := range bools {
			a := sec.BeginAttribute(t.b)
			err = addPathMemberships(a.AddMembershipMixedLowCardRef, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}

	err = inst.ent.CommitEntity()
	if err != nil {
		err = eb.Build().Uint64("id", id).Errorf("commit entity: %w", err)
		return
	}
	inst.held++
	if inst.held >= inst.batch {
		err = inst.flush(ctx)
	}
	return
}

// addPathMemberships attaches the shredded value's address. The path always
// rides MembJsonPath; the elided array indices ride MembJsonParams as a second
// membership on the same value, keeping the canonical scheme's lmv/mvhp split
// intact inside a schema that has no verbatim membership channel.
//
// An error here means the document holds an array longer than the params codec
// can address ([membership.MaxParamsIndex]); it aborts the ingest rather than
// silently dropping the position, which is the failure mode the codec exists to
// rule out.
func addPathMemberships[T any](add func(uint64, []byte) T, t shredded) (err error) {
	add(MembJsonPath.GetId().Value(), []byte(t.path))
	var p []byte
	p, err = formatParams(t.params)
	if err != nil {
		err = eb.Build().Str("path", t.path).Errorf("path: %w", err)
		return
	}
	if p != nil {
		add(MembJsonParams.GetId().Value(), p)
	}
	return
}

func (inst *ingester) flush(ctx context.Context) (err error) {
	if inst.ent == nil || inst.held == 0 {
		return
	}
	var records []arrow.RecordBatch
	records, err = inst.ent.TransferRecords(nil)
	if err != nil {
		err = eh.Errorf("transfer records: %w", err)
		return
	}
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	err = inst.cli.InsertArrow(ctx, inst.table, records)
	if err != nil {
		err = eh.Errorf("insert arrow: %w", err)
		return
	}
	inst.held = 0
	return
}
