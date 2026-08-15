// Package storeexec adapts [github.com/stergiotis/boxer/public/keelson/data/chclient]
// to [github.com/stergiotis/boxer/public/storage/recordstore.ExecutorI], the
// seam a generated record store (ADR-0100) uses to reach ClickHouse.
//
// This is ADR-0105 D1. The adapter lives on the keelson side so the
// dependency direction stays one-way: keelson imports recordstore, never the
// reverse. Its sibling
// [github.com/stergiotis/boxer/public/storage/recordstore/chexec.LocalExecutor]
// covers tests and local tooling by shelling out to `clickhouse-local`; this
// one talks to a server over HTTP and is what a long-running keelson service
// binds.
//
// # Reads stream
//
// QueryArrow appends ` FORMAT ArrowStream` to the caller's statement and
// decodes the response as it arrives, so a large result never materializes in
// full. That is the streaming implementation the ExecutorI iterator shape was
// written for; the buffered LocalExecutor satisfies the same contract by
// iterating a materialized slice.
//
// Two preconditions ride on the appended clause, both of which fail loudly
// (ClickHouse answers with a syntax error carrying the offending text):
//
//   - The statement must not carry its own FORMAT clause. Generated stores
//     never emit one — they end at a SETTINGS clause, and FORMAT sits after
//     SETTINGS in ClickHouse's grammar.
//   - The statement must be a single statement. The HTTP interface rejects a
//     multi-statement script outright ("Multi-statements are not allowed",
//     verified against 26.7.3). A generated EnsureTable honours this — it
//     issues its CREATE DATABASE and CREATE TABLE one per Exec
//     (recordstore.ProvisioningStatements) — so a store may provision itself
//     through this executor; before 2026-08-15 it shipped the embedded
//     script whole and could not. Callers issuing their own DDL send one
//     statement per Exec.
//
// # What a mid-result failure looks like
//
// ClickHouse has already answered 200 by the time rows stream, so a
// server-side failure part way through a result arrives as a corrupt Arrow
// stream — an Arrow decode error from the iterator, not an HTTP status. The
// `wait_end_of_query=1` setting would trade streaming (and server memory) for
// a clean error code; it is deliberately not set here.
//
// # Concurrency
//
// A Client is goroutine-safe and so is this wrapper, but a generated store is
// single-goroutine (ADR-0100) and ADR-0105 D4 confines it to one owner. One
// executor may back several confined stores.
package storeexec

import (
	"bufio"
	"context"
	"errors"
	"io"
	"iter"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

var _ recordstore.ExecutorI = (*Executor)(nil)

// arrowStreamFormat is the output-format clause QueryArrow appends. Stream,
// not file: the file format carries its footer last and would force the whole
// response into memory before the first batch could be read.
const arrowStreamFormat = " FORMAT ArrowStream"

// Executor moves statements and Arrow batches between a generated record
// store and a ClickHouse server over HTTP.
type Executor struct {
	client *chclient.Client
	alloc  memory.Allocator
}

// New wraps client. A nil alloc takes the Go allocator, matching chexec.
func New(client *chclient.Client, alloc memory.Allocator) (inst *Executor, err error) {
	if client == nil {
		err = eh.Errorf("storeexec: nil client")
		return
	}
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	inst = &Executor{client: client, alloc: alloc}
	return
}

// Exec runs sql for its side effect. See the package comment on single
// statements.
//
// It returns once the server has *accepted* the statement, which is not quite
// the same as having completed it: a failure raised after ClickHouse has
// answered 200 rides in a response body that
// [github.com/stergiotis/boxer/public/keelson/data/chclient.Client.Exec]
// discards, so it reads here as success. That doc carries the detail and the
// reason nothing catches it yet.
func (inst *Executor) Exec(ctx context.Context, sql string) (err error) {
	err = inst.client.Exec(ctx, sql)
	if err != nil {
		err = eh.Errorf("storeexec: exec: %w", err)
	}
	return
}

// QueryArrow runs sql with ArrowStream output and yields the decoded batches
// as they arrive.
//
// Ownership of each yielded batch transfers to the consumer, which must
// Release it — including the batch it breaks on. Batches never yielded stay
// this implementation's to release. An error ends the sequence as a final
// (nil, err) pair.
func (inst *Executor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		body, err := inst.client.Query(ctx, withArrowStreamFormat(sql))
		if err != nil {
			yield(nil, eh.Errorf("storeexec: query: %w", err))
			return
		}
		defer func() { _ = body.Close() }()

		// A zero-row result still carries its Arrow schema message, so an
		// entirely empty body means the statement produced no result set at
		// all. Yield nothing rather than report the EOF the IPC reader would
		// raise — the buffered sibling makes the same call for the same
		// reason.
		buf := bufio.NewReader(body)
		if _, perr := buf.Peek(1); perr != nil {
			if errors.Is(perr, io.EOF) {
				return
			}
			yield(nil, eh.Errorf("storeexec: read response: %w", perr))
			return
		}

		rdr, err := ipc.NewReader(buf, ipc.WithAllocator(inst.alloc))
		if err != nil {
			yield(nil, eh.Errorf("storeexec: arrow reader: %w", err))
			return
		}
		defer rdr.Release()

		for rdr.Next() {
			// RecordBatch is valid only until the next Next, so retain before
			// handing it over; the consumer's Release balances this one.
			rec := rdr.RecordBatch()
			rec.Retain()
			if !yield(rec, nil) {
				// The consumer owns rec now even though it stopped — releasing
				// here would drop the reference it is expected to drop.
				return
			}
		}
		// The reader clears its error at clean end of stream, so a non-nil Err
		// here is a real decode failure.
		if rerr := rdr.Err(); rerr != nil {
			yield(nil, eh.Errorf("storeexec: arrow decode: %w", rerr))
		}
	}
}

// InsertArrow appends records to table and returns once the insert is
// acknowledged. The records are not retained; the caller releases them after
// return.
func (inst *Executor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	err = inst.client.InsertArrow(ctx, table, records)
	if err != nil {
		err = eh.Errorf("storeexec: insert into %s: %w", table, err)
	}
	return
}

// withArrowStreamFormat appends the output-format clause. A trailing
// semicolon is trimmed first: it is legal on a single statement but would put
// the clause past the end of it.
func withArrowStreamFormat(sql string) (out string) {
	out = strings.TrimRight(sql, " \t\r\n;") + arrowStreamFormat
	return
}
