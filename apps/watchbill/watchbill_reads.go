package watchbill

import (
	"context"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	wb "github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
)

// The trail and the worker row are introspection tables, not client verbs
// (ADR-0236 §SD1): two reads over keelson() tables, each through the
// `keelson.query.<table>` grant the manifest declares for it (ADR-0253).
// The rows come back as JSONEachRow, the one text format a small reader
// decodes without an Arrow allocator.

// eventRow is one row of keelson('watchbill_event').
type eventRow struct {
	JobId     string `json:"job_id"`
	At        string `json:"at"`
	State     string `json:"state"`
	Attempt   int64  `json:"attempt"`
	WorkerRun string `json:"worker_run"`
	Note      string `json:"note"`
	Error     string `json:"error"`
}

// workerRow is one row of keelson('watchbill_worker'): a run on the cell
// as its presence row and heartbeat say (ADR-0237), the live fields
// filled for the process's own worker.
type workerRow struct {
	RunId      string   `json:"run_id"`
	Host       string   `json:"host"`
	Kinds      []string `json:"kinds"`
	Queues     []string `json:"queues"`
	MaxWorkers int64    `json:"max_workers"`
	StartedAt  string   `json:"started_at"`
	Alive      bool     `json:"alive"`
	Local      bool     `json:"local"`
	Running    []string `json:"running"`
	LastTick   string   `json:"last_tick"`
	PollMs     int64    `json:"poll_ms"`
	Serving    bool     `json:"serving"`
	Sweeping   bool     `json:"sweeping"`
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

func (inst *tableReader) events(ctx context.Context, jobId string) (rows []eventRow, err error) {
	sql := "SELECT job_id, at, state, attempt, worker_run, note, error FROM keelson('" + wb.TableEvent + "') WHERE job_id = " +
		marshalling.EscapeString(jobId) + " ORDER BY at"
	return keelsonquery.Rows[eventRow](ctx, inst.cli, wb.TableEvent, sql)
}

func (inst *tableReader) workers(ctx context.Context) (rows []workerRow, err error) {
	const sql = "SELECT run_id, host, kinds, queues, max_workers, started_at, alive, local, running, last_tick, poll_ms, serving, sweeping FROM keelson('" + wb.TableWorker + "') WHERE alive ORDER BY local DESC, started_at"
	return keelsonquery.Rows[workerRow](ctx, inst.cli, wb.TableWorker, sql)
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
