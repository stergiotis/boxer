package chlocalbroker

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// ExecRequest is the caller-facing request shape for ExecOnPool. The
// pool name is supplied as a separate argument so the same request
// can be retried against a different pool.
type ExecRequest struct {
	// SQL is the statement to execute. The broker appends "FORMAT X;"
	// where X = Format, then closes the worker's stdin.
	SQL string
	// Format is the ClickHouse FORMAT clause (e.g. "TabSeparated",
	// "JSONEachRow", "ArrowStream"). Empty means default.
	Format string
	// Streaming reserved for a later milestone; setting it true in
	// M2 returns a structured error from the broker.
	Streaming bool
	// Cacheable opts into the per-pool LRU result cache (ADR-0028
	// §SD5, M3). Hits skip the worker entirely. The broker also
	// enforces a SQL-prefix gate (SELECT/SHOW/DESCRIBE/EXPLAIN/WITH);
	// any other prefix is treated as not cacheable regardless of
	// this flag. Caller is responsible for not setting this on
	// non-deterministic queries (now(), rand(), network funcs,
	// mutable dictionaries).
	Cacheable bool
	// Settings is forwarded to the worker via SETTINGS k=v pairs.
	// Reserved for future use; ignored by the M2 broker.
	Settings map[string]string
	// InputTables exposes in-memory Arrow data as TEMPORARY tables to
	// the SQL (ADR-0094 §SD5). Each value is Arrow IPC in the `Arrow`
	// file format (with footer — i.e. ipc.FileWriter output, NOT
	// ArrowStream). The broker writes each to a private temp dir and
	// prepends `CREATE TEMPORARY TABLE <name> AS SELECT * FROM
	// file('<abs>','Arrow');` to SQL before submitting. Names must be
	// `[A-Za-z_][A-Za-z0-9_]*` (≤64 bytes); others are rejected. When
	// non-empty, each table's bytes fold into the result-cache key, so
	// a cached entry never outlives a changed input under unchanged
	// SQL — callers may set Cacheable even for volatile inputs.
	InputTables map[string][]byte
}

// ExecReply wraps a broker response. The embedded ReadCloser exposes
// the SQL output bytes; for M2 this is always a bytes.Reader (no
// streaming over the bus). Err returns the worker's exit error and
// stderr tail if the worker exited non-zero, otherwise nil. CacheHit
// is true when the result came from the per-pool LRU (ADR-0028 §SD5,
// M3) and no worker was touched.
type ExecReply struct {
	io.ReadCloser
	ContentType string
	Elapsed     time.Duration
	CacheHit    bool
	err         error
}

// Err returns the worker's error after the body has been read (or
// available immediately; reading the body is not required to observe
// Err in M2 since the broker has already drained the worker).
func (inst *ExecReply) Err() (err error) {
	err = inst.err
	return
}

// ExecOnPool publishes a request on `ch.local.exec.<poolName>` via
// the given bus and returns the decoded reply. The caller's bus
// client must hold a SubjectFilter with CapDirectionPub (or Both)
// matching `ch.local.exec.<poolName>` (or a wildcard that covers it).
//
// ctx is honoured two ways: (1) if it has a deadline, the deadline is
// encoded into the wire request so the broker can shorten its own
// execution ctx; (2) ctx.Err() is checked at entry. ctx cancellation
// during the bus.Request call is NOT propagated to the broker — the
// in-proc bus API has no per-call ctx. The bus's global request
// timeout still applies.
//
// On success rep.ReadCloser delivers the SQL output bytes; on
// worker-side failure rep is non-nil with Err() set to the worker's
// error and any stderr tail. On bus-side failure (timeout,
// permission denied) err is non-nil and rep is nil.
func ExecOnPool(ctx context.Context, bus app.BusI, poolName string, req ExecRequest) (rep *ExecReply, err error) {
	if err = ctx.Err(); err != nil {
		err = eh.Errorf("chlocalbroker: ctx cancelled before request: %w", err)
		return
	}
	if bus == nil {
		err = eh.Errorf("chlocalbroker: bus is nil")
		return
	}
	if poolName == "" {
		err = eh.Errorf("chlocalbroker: pool name is empty")
		return
	}
	wireReq := wireRequest{
		SQL:         req.SQL,
		Format:      req.Format,
		Streaming:   req.Streaming,
		Cacheable:   req.Cacheable,
		Settings:    req.Settings,
		InputTables: req.InputTables,
	}
	if deadline, ok := ctx.Deadline(); ok {
		wireReq.DeadlineUnixNanos = deadline.UnixNano()
	}
	reqBytes, err := encodeRequest(wireReq)
	if err != nil {
		return
	}
	replyBytes, err := bus.Request(SubjectExecPrefix+poolName, reqBytes)
	if err != nil {
		err = eh.Errorf("chlocalbroker: bus request %s: %w", SubjectExecPrefix+poolName, err)
		return
	}
	decoded, err := decodeReply(replyBytes)
	if err != nil {
		return
	}
	rep = &ExecReply{
		ReadCloser:  io.NopCloser(bytes.NewReader(decoded.Body)),
		ContentType: decoded.ContentType,
		Elapsed:     time.Duration(decoded.ElapsedNs),
		CacheHit:    decoded.CacheHit,
	}
	if !decoded.OK {
		if decoded.Stderr != "" {
			rep.err = eh.Errorf("chlocalbroker: %s (stderr: %q)", decoded.Error, decoded.Stderr)
		} else {
			rep.err = eh.Errorf("chlocalbroker: %s", decoded.Error)
		}
	}
	return
}
