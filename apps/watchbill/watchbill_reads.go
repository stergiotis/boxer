package watchbill

import (
	"context"
	"iter"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
)

// The trail and the worker row are introspection tables, not client verbs
// (ADR-0236 §SD1): two reads over keelson() tables, each through the
// `keelson.query.<table>` grant the manifest declares for it (ADR-0253).
// The rows come back as ArrowStream and are decoded into columns
// (ADR-0257).

// eventCols is keelson('watchbill_event') as columns.
type eventCols struct {
	JobId     []string `ch:"job_id"`
	At        []string `ch:"at"`
	State     []string `ch:"state"`
	Attempt   []int64  `ch:"attempt"`
	WorkerRun []string `ch:"worker_run"`
	Note      []string `ch:"note"`
	Error     []string `ch:"error"`
}

// eventRow is one transition, assembled from the columns.
type eventRow struct {
	JobId     string
	At        string
	State     string
	Attempt   int64
	WorkerRun string
	Note      string
	Error     string
}

func (inst *eventCols) Len() (n int) { return len(inst.JobId) }

// Row assembles transition i.
func (inst *eventCols) Row(i int) (r eventRow) {
	r = eventRow{JobId: inst.JobId[i], At: inst.At[i], State: inst.State[i], Attempt: inst.Attempt[i],
		WorkerRun: inst.WorkerRun[i], Note: inst.Note[i], Error: inst.Error[i]}
	return
}

// All assembles every transition in turn.
func (inst *eventCols) All() iter.Seq2[int, eventRow] {
	return func(yield func(int, eventRow) bool) {
		for i := range inst.Len() {
			if !yield(i, inst.Row(i)) {
				return
			}
		}
	}
}

// workerCols is keelson('watchbill_worker') as columns: a run on the cell
// as its presence row and heartbeat say (ADR-0237), the live fields filled
// for the process's own worker.
type workerCols struct {
	RunId      []string   `ch:"run_id"`
	Host       []string   `ch:"host"`
	Kinds      [][]string `ch:"kinds"`
	Queues     [][]string `ch:"queues"`
	MaxWorkers []int64    `ch:"max_workers"`
	StartedAt  []string   `ch:"started_at"`
	Alive      []bool     `ch:"alive"`
	Local      []bool     `ch:"local"`
	Running    [][]string `ch:"running"`
	LastTick   []string   `ch:"last_tick"`
	PollMs     []int64    `ch:"poll_ms"`
	Serving    []bool     `ch:"serving"`
	Sweeping   []bool     `ch:"sweeping"`
}

// workerRow is one run, assembled from the columns.
type workerRow struct {
	RunId      string
	Host       string
	Kinds      []string
	Queues     []string
	MaxWorkers int64
	StartedAt  string
	Alive      bool
	Local      bool
	Running    []string
	LastTick   string
	PollMs     int64
	Serving    bool
	Sweeping   bool
}

func (inst *workerCols) Len() (n int) { return len(inst.RunId) }

// Row assembles run i.
func (inst *workerCols) Row(i int) (r workerRow) {
	r = workerRow{RunId: inst.RunId[i], Host: inst.Host[i], Kinds: inst.Kinds[i], Queues: inst.Queues[i],
		MaxWorkers: inst.MaxWorkers[i], StartedAt: inst.StartedAt[i], Alive: inst.Alive[i], Local: inst.Local[i],
		Running: inst.Running[i], LastTick: inst.LastTick[i], PollMs: inst.PollMs[i], Serving: inst.Serving[i],
		Sweeping: inst.Sweeping[i]}
	return
}

// All assembles every run in turn.
func (inst *workerCols) All() iter.Seq2[int, workerRow] {
	return func(yield func(int, workerRow) bool) {
		for i := range inst.Len() {
			if !yield(i, inst.Row(i)) {
				return
			}
		}
	}
}

// tableReader reads the two tables over the bus.
type tableReader struct {
	cli *keelsonquery.Client
}

// newTableReader returns nil when bus is nil — the host minted no bus for
// the window — so the window can say so rather than fail each read.
func newTableReader(bus app.BusI) (inst *tableReader) {
	if bus == nil {
		return nil
	}
	inst = &tableReader{cli: keelsonquery.NewClient(bus)}
	return
}

func (inst *tableReader) events(ctx context.Context, jobId string) (cols eventCols, err error) {
	sql := "SELECT job_id, at, state, attempt, worker_run, note, error FROM keelson('" + wb.TableEvent + "') WHERE job_id = " +
		marshalling.EscapeString(jobId) + " ORDER BY at"
	_, err = keelsonquery.Columns(ctx, inst.cli, wb.TableEvent, sql, &cols)
	return
}

func (inst *tableReader) workers(ctx context.Context) (cols workerCols, err error) {
	const sql = "SELECT run_id, host, kinds, queues, max_workers, started_at, alive, local, running, last_tick, poll_ms, serving, sweeping FROM keelson('" + wb.TableWorker + "') WHERE alive ORDER BY local DESC, started_at"
	_, err = keelsonquery.Columns(ctx, inst.cli, wb.TableWorker, sql, &cols)
	return
}

// firstLine is the one-line spelling of a multi-line error text.
func firstLine(s string) (line string) {
	line = s
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	const maxLen = 100
	if len(line) > maxLen {
		line = line[:maxLen] + "…"
	}
	return
}
