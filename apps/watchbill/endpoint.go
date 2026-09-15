package watchbill

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The trail and the worker row are introspection tables, not client verbs
// (ADR-0236 §SD1): two SQL reads over keelson() on the process's local
// query endpoint (ADR-0094 §SD6). The rows come back as JSONEachRow, the
// one text format a small reader decodes without an Arrow allocator.

// endpointTimeout bounds one read of the local endpoint.
const endpointTimeout = 5 * time.Second

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

// endpointClient reads the two tables.
type endpointClient struct {
	cli *chclient.Client
}

// newEndpointClient returns nil when url is empty — the process serves no
// local endpoint — so the window can say so rather than fail each read.
func newEndpointClient(url string) (inst *endpointClient) {
	if url == "" {
		return nil
	}
	inst = &endpointClient{cli: chclient.New(chclient.Config{URL: url}, &http.Client{Timeout: endpointTimeout})}
	return
}

func (inst *endpointClient) events(ctx context.Context, jobId string) (rows []eventRow, err error) {
	sql := "SELECT job_id, at, state, attempt, worker_run, note, error FROM keelson('watchbill_event') WHERE job_id = " +
		marshalling.EscapeString(jobId) + " ORDER BY at FORMAT JSONEachRow"
	err = inst.query(ctx, sql, func(dec *json.Decoder) (derr error) {
		var r eventRow
		if derr = dec.Decode(&r); derr == nil {
			rows = append(rows, r)
		}
		return
	})
	return
}

func (inst *endpointClient) workers(ctx context.Context) (rows []workerRow, err error) {
	const sql = "SELECT run_id, host, kinds, queues, max_workers, started_at, alive, local, running, last_tick, poll_ms, serving, sweeping FROM keelson('watchbill_worker') WHERE alive ORDER BY local DESC, started_at FORMAT JSONEachRow"
	err = inst.query(ctx, sql, func(dec *json.Decoder) (derr error) {
		var r workerRow
		if derr = dec.Decode(&r); derr == nil {
			rows = append(rows, r)
		}
		return
	})
	return
}

// query runs sql and hands the body to each until it is exhausted.
func (inst *endpointClient) query(ctx context.Context, sql string, each func(*json.Decoder) error) (err error) {
	ctx, cancel := context.WithTimeout(ctx, endpointTimeout)
	defer cancel()
	body, err := inst.cli.Query(ctx, sql)
	if err != nil {
		return eh.Errorf("introspection read: %w", err)
	}
	defer func() { _ = body.Close() }()
	dec := json.NewDecoder(body)
	for dec.More() {
		if err = each(dec); err != nil {
			return eh.Errorf("introspection row: %w", err)
		}
	}
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
