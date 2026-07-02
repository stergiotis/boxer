// Package chexec provides concrete recordstore.ExecutorI implementations.
//
// LocalExecutor runs every call as a one-shot `clickhouse-local` process
// against a persistent --path directory, so tables created by Exec survive
// across calls (use a durable engine such as MergeTree; Memory tables
// vanish with each process). It is meant for tests and local tooling, not
// servers — each call pays process startup.
package chexec

import (
	"bytes"
	"context"
	"os/exec"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/recordstore"
)

var _ recordstore.ExecutorI = (*LocalExecutor)(nil)

type LocalExecutor struct {
	binary string
	path   string
	alloc  memory.Allocator
}

// NewLocalExecutor creates an executor over `clickhouse-local` with state
// persisted under path (a directory; created by clickhouse-local on first
// use). Returns an error when the binary is not on PATH.
func NewLocalExecutor(path string, alloc memory.Allocator) (inst *LocalExecutor, err error) {
	bin, err := exec.LookPath("clickhouse-local")
	if err != nil {
		err = eh.Errorf("clickhouse-local not on PATH: %w", err)
		return
	}
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	inst = &LocalExecutor{binary: bin, path: path, alloc: alloc}
	return
}

func (inst *LocalExecutor) run(ctx context.Context, sql string, outputFormat string, stdin []byte) (stdout []byte, err error) {
	args := []string{"--path", inst.path, "--multiquery"}
	if outputFormat != "" {
		args = append(args, "--output-format", outputFormat)
	}
	args = append(args, "--query", sql)
	cmd := exec.CommandContext(ctx, inst.binary, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		err = eh.Errorf("clickhouse-local failed: %w; stderr: %s", err, stderr.String())
		return
	}
	stdout = out.Bytes()
	return
}

func (inst *LocalExecutor) Exec(ctx context.Context, sql string) (err error) {
	_, err = inst.run(ctx, sql, "", nil)
	return
}

func (inst *LocalExecutor) QueryArrow(ctx context.Context, sql string) (records []arrow.RecordBatch, err error) {
	raw, err := inst.run(ctx, sql, "Arrow", nil)
	if err != nil {
		return
	}
	if len(raw) == 0 {
		return // no rows: clickhouse may emit an empty body
	}
	rdr, err := ipc.NewFileReader(bytes.NewReader(raw), ipc.WithAllocator(inst.alloc))
	if err != nil {
		err = eh.Errorf("decode arrow result: %w", err)
		return
	}
	defer rdr.Close()
	records = make([]arrow.RecordBatch, 0, rdr.NumRecords())
	for i := 0; i < rdr.NumRecords(); i++ {
		var rec arrow.RecordBatch
		rec, err = rdr.RecordBatchAt(i)
		if err != nil {
			for _, r := range records {
				r.Release()
			}
			records = nil
			err = eh.Errorf("read arrow record %d: %w", i, err)
			return
		}
		rec.Retain()
		records = append(records, rec)
	}
	return
}

func (inst *LocalExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	if len(records) == 0 {
		return
	}
	var buf bytes.Buffer
	w, err := ipc.NewFileWriter(&buf, ipc.WithSchema(records[0].Schema()), ipc.WithAllocator(inst.alloc))
	if err != nil {
		err = eh.Errorf("create arrow writer: %w", err)
		return
	}
	for _, rec := range records {
		err = w.Write(rec)
		if err != nil {
			err = eh.Errorf("write arrow record: %w", err)
			return
		}
	}
	err = w.Close()
	if err != nil {
		err = eh.Errorf("close arrow writer: %w", err)
		return
	}
	_, err = inst.run(ctx, "INSERT INTO "+table+" FORMAT Arrow", "", buf.Bytes())
	return
}
