package main

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
)

// resultsCommand lands a run directory's measurements in a facts table, which
// the bookjsonbench applet then reads back. The trial protocol asks for this
// (§6 Reporting) so the benchmark reports through the layer it is measuring;
// the numbers themselves stay in the run directory as the provenance record.
//
// The target defaults to a benchmark-local database rather than the live
// store: these are the trial's own numbers, and §4's isolation rule applies to
// everything this trial writes.
func resultsCommand() *cli.Command {
	return &cli.Command{
		Name:  "results",
		Usage: "load a run directory's timings and sizes into a facts table",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8123/"},
			&cli.StringFlag{Name: "user", Value: "default"},
			&cli.StringFlag{Name: "password"},
			&cli.StringFlag{Name: "database", Value: "jsonbench_results"},
			&cli.StringFlag{Name: "table", Value: "facts"},
			&cli.StringFlag{Name: "run-dir", Required: true,
				Usage: "a doc/trials/jsonbench-on-facts/runs/<run> directory"},
			&cli.StringFlag{Name: "run-id",
				Usage: "identifier for this run; defaults to the run directory's base name"},
		},
		Action: runResults,
	}
}

func runResults(cCtx *cli.Context) (err error) {
	runDir := cCtx.String("run-dir")
	runID := cCtx.String("run-id")
	if runID == "" {
		runID = filepath.Base(strings.TrimRight(runDir, "/"))
	}
	// The run id encodes the date; use it as the fact timestamp so the facts
	// table's own ORDER BY ts groups a run together.
	ts := time.Now().UTC()
	if d, e := time.Parse("2006-01-02", runID[:min(10, len(runID))]); e == nil {
		ts = d.UTC()
	}

	arms, err := filepath.Glob(filepath.Join(runDir, "arm-*"))
	if err != nil {
		err = eb.Build().Str("runDir", runDir).Errorf("glob: %w", err)
		return
	}
	if len(arms) == 0 {
		err = eh.Errorf("no arm-* directories under %s", runDir)
		return
	}

	cli0 := chclient.New(chclient.Config{
		URL:      cCtx.String("url"),
		User:     cCtx.String("user"),
		Password: cCtx.String("password"),
	}, nil)
	table := cCtx.String("database") + "." + cCtx.String("table")

	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 512)
	var id uint64
	var timings, sizes int

	for _, dir := range arms {
		arm := strings.TrimPrefix(filepath.Base(dir), "arm-")

		for _, r := range readTimings(filepath.Join(dir, "timings.tsv")) {
			id++
			nk := blake3.Sum256([]byte(runID + "|" + arm + "|" + r.query + "|" + strconv.Itoa(r.try)))
			ent.BeginEntity().SetId(id, nk[:16]).SetTimestamp(ts)
			sym := ent.GetSectionSymbol()
			sym.BeginAttribute("jsonbenchTiming").AddMembershipLowCardRef(MembKindBenchTiming.GetId().Value()).EndAttribute()
			sym.BeginAttribute(runID).AddMembershipLowCardRef(MembBenchRun.GetId().Value()).EndAttribute()
			sym.BeginAttribute(arm).AddMembershipLowCardRef(MembBenchArm.GetId().Value()).EndAttribute()
			sym.BeginAttribute(r.query).AddMembershipLowCardRef(MembBenchQuery.GetId().Value()).EndAttribute()
			sym.EndSection()
			i64 := ent.GetSectionI64Array()
			i64.BeginAttributeSingle(int64(r.try)).AddMembershipLowCardRef(MembBenchTry.GetId().Value()).EndAttribute()
			i64.BeginAttributeSingle(r.memBytes).AddMembershipLowCardRef(MembBenchMemoryBytes.GetId().Value()).EndAttribute()
			i64.EndSection()
			f64 := ent.GetSectionF64Array()
			f64.BeginAttributeSingle(r.seconds).AddMembershipLowCardRef(MembBenchSeconds.GetId().Value()).EndAttribute()
			f64.EndSection()
			if err = ent.CommitEntity(); err != nil {
				err = eh.Errorf("commit timing %s/%s/%d: %w", arm, r.query, r.try, err)
				return
			}
			timings++
		}

		for _, m := range readSizes(filepath.Join(dir, "sizes.txt")) {
			id++
			nk := blake3.Sum256([]byte(runID + "|" + arm + "|" + m.name))
			ent.BeginEntity().SetId(id, nk[:16]).SetTimestamp(ts)
			sym := ent.GetSectionSymbol()
			sym.BeginAttribute("jsonbenchSize").AddMembershipLowCardRef(MembKindBenchSize.GetId().Value()).EndAttribute()
			sym.BeginAttribute(runID).AddMembershipLowCardRef(MembBenchRun.GetId().Value()).EndAttribute()
			sym.BeginAttribute(arm).AddMembershipLowCardRef(MembBenchArm.GetId().Value()).EndAttribute()
			sym.BeginAttribute(m.name).AddMembershipLowCardRef(MembBenchMetric.GetId().Value()).EndAttribute()
			sym.EndSection()
			i64 := ent.GetSectionI64Array()
			i64.BeginAttributeSingle(m.value).AddMembershipLowCardRef(MembBenchMetricValue.GetId().Value()).EndAttribute()
			i64.EndSection()
			if err = ent.CommitEntity(); err != nil {
				err = eh.Errorf("commit size %s/%s: %w", arm, m.name, err)
				return
			}
			sizes++
		}
	}

	var records []arrow.RecordBatch
	records, err = ent.TransferRecords(nil)
	if err != nil {
		err = eh.Errorf("transfer records: %w", err)
		return
	}
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	err = cli0.InsertArrow(context.Background(), table, records)
	if err != nil {
		err = eh.Errorf("insert arrow: %w", err)
		return
	}
	log.Info().Str("run", runID).Str("table", table).
		Int("timings", timings).Int("sizes", sizes).Msg("results landed as facts")
	return
}

type timingRow struct {
	query    string
	try      int
	seconds  float64
	memBytes int64
}

// readTimings parses measure.sh's timings.tsv: query, try, seconds, bytes.
// A missing or malformed file yields nothing rather than failing the load —
// an arm that was not benched simply contributes no timing facts.
func readTimings(path string) (out []timingRow) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 4 {
			continue
		}
		try, e1 := strconv.Atoi(p[1])
		secs, e2 := strconv.ParseFloat(p[2], 64)
		mem, e3 := strconv.ParseInt(p[3], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		out = append(out, timingRow{query: p[0], try: try, seconds: secs, memBytes: mem})
	}
	return
}

type sizeRow struct {
	name  string
	value int64
}

// readSizes parses measure.sh's sizes.txt: "<name> <integer>" per line.
func readSizes(path string) (out []sizeRow) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 2 {
			continue
		}
		v, e := strconv.ParseInt(p[1], 10, 64)
		if e != nil {
			continue
		}
		out = append(out, sizeRow{name: p[0], value: v})
	}
	return
}

// vocabCommand prints the trial's membership ids. The result facts tag their
// attributes with LowCardRef memberships — uint64 ids from the vcs-managed
// registry — so a SQL reader needs the id for the tag it wants. The ids are
// deterministic for a given registration, which is what lets the book pages
// carry them as literals.
func vocabCommand() *cli.Command {
	return &cli.Command{
		Name:  "vocab",
		Usage: "print the trial's membership natural keys and their ids",
		Action: func(*cli.Context) error {
			for _, m := range []struct {
				name string
				id   uint64
			}{
				{"jsonbenchKindTiming", MembKindBenchTiming.GetId().Value()},
				{"jsonbenchKindSize", MembKindBenchSize.GetId().Value()},
				{"jsonbenchRun", MembBenchRun.GetId().Value()},
				{"jsonbenchArm", MembBenchArm.GetId().Value()},
				{"jsonbenchQuery", MembBenchQuery.GetId().Value()},
				{"jsonbenchTry", MembBenchTry.GetId().Value()},
				{"jsonbenchSeconds", MembBenchSeconds.GetId().Value()},
				{"jsonbenchMemoryBytes", MembBenchMemoryBytes.GetId().Value()},
				{"jsonbenchMetric", MembBenchMetric.GetId().Value()},
				{"jsonbenchMetricValue", MembBenchMetricValue.GetId().Value()},
			} {
				log.Info().Str("nk", m.name).Uint64("id", m.id).Msg("membership")
			}
			return nil
		},
	}
}
