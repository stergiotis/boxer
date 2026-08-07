package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/apps/jsonbench/jsonmap"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	leewaydml "github.com/stergiotis/boxer/public/semistructured/leeway/dml"
)

func jsonmapIngestCommand() *cli.Command {
	return &cli.Command{
		Name:  "ingest",
		Usage: "shred gzipped Jetstream files into a canonical JSON mapping table",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8123/"},
			&cli.StringFlag{Name: "user", Value: "default"},
			&cli.StringFlag{Name: "password"},
			&cli.StringFlag{Name: "database"},
			&cli.StringFlag{Name: "table", Value: "json"},
			&cli.StringFlag{
				Name: "parquet-out",
				// The second-substrate trial's arm W. With this set the ingest
				// writes the same Arrow record batches to a Parquet file and
				// never contacts ClickHouse, which is what makes the neutrality
				// claim about leeway's writer rather than about the file format:
				// the shredder, the generated builder and the record batches are
				// unchanged, only the sink moves.
				Usage: "write Parquet to this path instead of inserting into ClickHouse",
			},
			&cli.StringFlag{
				Name:  "parquet-compression",
				Value: "zstd",
				Usage: "zstd or uncompressed; only with --parquet-out",
			},
			&cli.StringFlag{Name: "data-dir", Required: true},
			&cli.IntFlag{Name: "files", Value: 1, Usage: "number of file_*.json.gz to ingest"},
			&cli.IntFlag{
				Name: "file-offset",
				// Sharding knob. The 10M run's logbook entry names the
				// single-process ingest as the thing to parallelise first; with
				// an offset, N processes over disjoint file ranges write to the
				// same table concurrently, which ClickHouse handles natively.
				Usage: "skip this many files before ingesting (shard by file for parallel loads)",
			},
			&cli.IntFlag{Name: "batch", Value: 50000, Usage: "rows per Arrow record batch"},
			&cli.IntFlag{Name: "sample", Value: 20000, Usage: "docs sampled to decide symbol vs string routing"},
			&cli.Float64Flag{Name: "symbol-ratio", Value: 0.01, Usage: "distinct/total below which a path routes to the symbol section"},
			&cli.IntFlag{Name: "limit", Value: 0, Usage: "stop after N documents (0 = no limit)"},
			&cli.StringSliceFlag{
				Name:  "symbol-path",
				Usage: "force a path into the symbol section; repeatable. Skips sampling when set.",
			},
		},
		Action: runJsonmapIngest,
	}
}

func runJsonmapIngest(cCtx *cli.Context) (err error) {
	dataDir := cCtx.String("data-dir")
	offset := cCtx.Int("file-offset")
	var files []string
	files, err = tierFilesFrom(dataDir, offset, cCtx.Int("files"))
	if err != nil {
		return
	}

	// Which paths carry low-cardinality strings is the one judgement the
	// canonical scheme says "can be inferred appreciatively from sample data".
	// A shard must not infer it independently — two shards disagreeing would
	// put the same path in different sections — so a parallel load passes
	// --symbol-path explicitly and only the first pass samples.
	symbolPaths := make(map[string]struct{}, 16)
	if forced := cCtx.StringSlice("symbol-path"); len(forced) > 0 {
		for _, p := range forced {
			symbolPaths[p] = struct{}{}
		}
		log.Info().Int("paths", len(symbolPaths)).Msg("symbol routing supplied by the caller")
	} else {
		log.Info().Int("sample", cCtx.Int("sample")).Msg("sampling for symbol routing")
		symbolPaths, err = sampleSymbolPaths(files[0], cCtx.Int("sample"), cCtx.Float64("symbol-ratio"))
		if err != nil {
			return
		}
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

	ing := &jsonmapIngester{
		alloc:       memory.NewGoAllocator(),
		batch:       cCtx.Int("batch"),
		symbolPaths: symbolPaths,
		limit:       uint64(cCtx.Int("limit")),
	}

	pqOut := cCtx.String("parquet-out")
	if pqOut == "" {
		if cCtx.String("database") == "" {
			err = eh.Errorf("one of --database or --parquet-out is required")
			return
		}
		ing.cli = chclient.New(chclient.Config{
			URL:      cCtx.String("url"),
			User:     cCtx.String("user"),
			Password: cCtx.String("password"),
		}, nil)
		ing.table = cCtx.String("database") + "." + cCtx.String("table")
	} else {
		var closePq func() error
		closePq, err = ing.openParquet(pqOut, cCtx.String("parquet-compression"))
		if err != nil {
			return
		}
		defer func() {
			cerr := closePq()
			if err == nil {
				err = cerr
			}
		}()
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
	log.Info().Msgf("docs=%d attributes=%d nulls_dropped=%d undecodable_skipped=%d elapsed_s=%.3f docs_per_s=%.0f",
		ing.docs, ing.attrs, ing.shr.nulls, undecodable, elapsed.Seconds(), float64(ing.docs)/elapsed.Seconds())
	return
}

// tierFilesFrom selects `n` files starting at `offset`, so shards can carve up
// the corpus without overlapping.
func tierFilesFrom(dir string, offset int, n int) (out []string, err error) {
	matches, err := filepath.Glob(filepath.Join(dir, "file_*.json.gz"))
	if err != nil {
		err = eh.Errorf("glob %s: %w", dir, err)
		return
	}
	sort.Strings(matches)
	if offset < 0 || n <= 0 {
		err = eh.Errorf("invalid shard: offset %d, files %d", offset, n)
		return
	}
	if len(matches) < offset+n {
		err = eh.Errorf("shard needs files %d..%d, %s holds %d", offset+1, offset+n, dir, len(matches))
		return
	}
	out = matches[offset : offset+n]
	return
}

// jsonmapIngester writes the shredded corpus through the generated canonical
// JSON mapping builder. It shares the shredder with the facts arm — the
// shredder already emits exactly the triples this mapping wants, path with "_"
// for array positions plus the elided indices — so the two arms provably hold
// the same decomposition of the same documents and differ only in the schema
// receiving it.
type jsonmapIngester struct {
	cli         *chclient.Client
	table       string
	alloc       memory.Allocator
	batch       int
	symbolPaths map[string]struct{}
	limit       uint64

	ent  *jsonmap.InEntityJson
	shr  shredder
	held int

	// pq is the arm-W sink. When it is set the ClickHouse client is nil and
	// flush writes the batches to Parquet instead. recs is its scratch slice,
	// reused across flushes.
	pq   *pqarrow.FileWriter
	recs []arrow.RecordBatch

	docs  uint64
	attrs uint64
}

// openParquet prepares the arm-W sink and returns its closer. The schema comes
// from the generated builder, so the file's columns are the leeway DDL
// pipeline's own output rather than a hand-written approximation — which is the
// whole point of the arm.
//
// Note what is *not* passed to the writer: the schema's encoding aspects. The
// mapping declares DoubleDelta on its int64 lane and low-cardinality on its
// symbol lanes, and neither reaches the Parquet writer properties, because
// nothing carries encoding aspects across that seam today. Measuring the cost
// of that is arm W's job, not working around it here.
func (inst *jsonmapIngester) openParquet(path string, compression string) (closer func() error, err error) {
	var codec compress.Compression
	switch compression {
	case "zstd":
		codec = compress.Codecs.Zstd
	case "uncompressed":
		codec = compress.Codecs.Uncompressed
	default:
		err = eh.Errorf("unknown --parquet-compression %q; want zstd or uncompressed", compression)
		return
	}
	var f *os.File
	f, err = os.Create(path)
	if err != nil {
		err = eh.Errorf("create %s: %w", path, err)
		return
	}
	buf := bufio.NewWriterSize(f, 1<<20)
	ent := jsonmap.NewInEntityJson(inst.alloc, inst.batch)
	inst.ent = ent
	inst.pq, err = pqarrow.NewFileWriter(ent.GetSchema(), buf,
		parquet.NewWriterProperties(
			parquet.WithAllocator(inst.alloc),
			parquet.WithCompression(codec),
		),
		pqarrow.NewArrowWriterProperties(
			pqarrow.WithAllocator(inst.alloc),
			pqarrow.WithStoreSchema(),
		))
	if err != nil {
		_ = f.Close()
		err = eh.Errorf("create parquet writer for %s: %w", path, err)
		return
	}
	log.Info().Str("path", path).Str("compression", compression).Msg("writing parquet")
	closer = func() error {
		if cerr := inst.pq.Close(); cerr != nil {
			_ = f.Close()
			return eh.Errorf("close parquet writer: %w", cerr)
		}
		if cerr := buf.Flush(); cerr != nil {
			_ = f.Close()
			return eh.Errorf("flush %s: %w", path, cerr)
		}
		return f.Close()
	}
	return
}

func (inst *jsonmapIngester) ingestFile(ctx context.Context, file string) (err error) {
	err = eachDoc(file, 0, func(raw []byte, doc map[string]any) error {
		if inst.limit > 0 && inst.docs >= inst.limit {
			return errStopIngest
		}
		return inst.add(ctx, raw, doc)
	})
	if errors.Is(err, errStopIngest) {
		err = nil
	}
	return
}

func (inst *jsonmapIngester) add(ctx context.Context, raw []byte, doc map[string]any) (err error) {
	if inst.ent == nil {
		inst.ent = jsonmap.NewInEntityJson(inst.alloc, inst.batch)
	}
	triples := inst.shr.shred(doc)
	inst.docs++

	// The row's identity is the hash of the document's own bytes — the
	// mapping's single plain value column. Unlike the facts arm there is no
	// separate timestamp column: this schema holds the document and nothing
	// else, so /time_us is an ordinary int64 attribute like any other.
	id := blake3.Sum256(raw)
	inst.ent.BeginEntity().SetId(id[:])

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
			// Counted by the shredder. The mapping *does* declare a null
			// section; this load deliberately leaves it empty (see the file
			// header on jsonbench_jsonmap.go), so a null is dropped here
			// exactly as the facts arm drops it — and reported, never silent.
		}
	}
	inst.attrs += uint64(len(strs) + len(syms) + len(ints) + len(floats) + len(bools))

	if len(syms) > 0 {
		sec := inst.ent.GetSectionSymbol()
		for _, t := range syms {
			a := sec.BeginAttribute(t.s)
			err = addVerbatimPath(a.AddMembershipMixedLowCardVerbatim, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}
	if len(strs) > 0 {
		sec := inst.ent.GetSectionString()
		for _, t := range strs {
			a := sec.BeginAttribute(t.s)
			err = addVerbatimPath(a.AddMembershipMixedLowCardVerbatim, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}
	if len(ints) > 0 {
		sec := inst.ent.GetSectionInt64()
		for _, t := range ints {
			a := sec.BeginAttribute(t.i)
			err = addVerbatimPath(a.AddMembershipMixedLowCardVerbatim, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}
	if len(floats) > 0 {
		sec := inst.ent.GetSectionFloat64()
		for _, t := range floats {
			a := sec.BeginAttribute(t.f)
			err = addVerbatimPath(a.AddMembershipMixedLowCardVerbatim, t)
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
			err = addVerbatimPath(a.AddMembershipMixedLowCardVerbatim, t)
			a.EndAttribute()
			if err != nil {
				return
			}
		}
		sec.EndSection()
	}

	err = inst.ent.CommitEntity()
	if err != nil {
		err = eh.Errorf("commit entity %d: %w", inst.docs, err)
		return
	}
	inst.held++
	if inst.held >= inst.batch {
		err = inst.flush(ctx)
	}
	return
}

// addVerbatimPath attaches the shredded value's address as a single membership:
// the path verbatim on `lmv`, the elided array indices on `mvhp`. This is the
// whole difference from the facts arm, which needs two memberships because its
// sections accept only Ref-shaped identities and the path has to ride the
// parameter channel of a synthetic "json path" ref.
//
// One membership per attribute is also what makes `lmv` co-index 1:1 with the
// value lane, so resolving a path in SQL is one indexOf rather than a
// membership-index → attribute-index indirection.
func addVerbatimPath[T any](add func([]byte, []byte) T, t shredded) (err error) {
	var params []byte
	params, err = formatParams(t.params)
	if err != nil {
		err = eh.Errorf("path %s: %w", t.path, err)
		return
	}
	add([]byte(t.path), params)
	return
}

func (inst *jsonmapIngester) flush(ctx context.Context) (err error) {
	if inst.ent == nil || inst.held == 0 {
		return
	}
	if inst.pq != nil {
		// WriteArrowRecords transfers, writes and releases in one step — the
		// same helper the leeway DML example uses, unchanged.
		inst.recs, err = leewaydml.WriteArrowRecords(inst.ent, inst.recs[:0], nil, inst.pq)
		if err != nil {
			err = eh.Errorf("write parquet: %w", err)
			return
		}
		inst.held = 0
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
