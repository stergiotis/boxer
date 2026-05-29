//go:build llm_generated_opus47

package regex_explorer

// ClickHouse transport: capability-mediated via ADR-0028's
// chlocalbroker. The regex explorer publishes a request on
// `ch.local.exec.regex_explorer`; the runtime broker drains a
// pre-spawned `clickhouse-local` worker's stdout and replies with
// the Arrow IPC bytes. No subprocess management lives in this
// package post-M2.

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/observability/eh"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
)

// chLocalPoolName is the pool the regex explorer asks the broker for.
// Each app gets an isolated pool so warm workers are not shared with
// other consumers (separate accounting, watchdog, future cache).
const chLocalPoolName = "regex_explorer"

// chLocalCapPattern is the SubjectFilter pattern the app declares in
// its Manifest.Caps — see app_register.go.
const chLocalCapPattern = chlocalbroker.SubjectExecPrefix + chLocalPoolName

// clStats is the minimal query summary surfaced to the UI — wall-clock
// elapsed only. The broker's reply does carry an ElapsedNs that could
// populate this, but the existing job code measures end-to-end so we
// keep that for now.
type clStats struct {
	ElapsedNs uint64
}

// executeArrowStreamViaBus publishes the query on
// ch.local.exec.regex_explorer via the supplied BusI, ingests the
// reply bytes as an Arrow IPC stream, and returns the reader + a
// closer to release the reply. ctx.Deadline (if any) is forwarded to
// the broker so a cancelled caller doesn't pin a worker.
func executeArrowStreamViaBus(ctx context.Context, bus runtimeapp.BusI, sql string, alloc memory.Allocator) (rdr *ipc.Reader, closer io.Closer, err error) {
	if bus == nil {
		err = eh.Errorf("regex_explorer: bus unavailable; chlocalbroker not wired")
		return
	}
	rep, reqErr := chlocalbroker.ExecOnPool(ctx, bus, chLocalPoolName, chlocalbroker.ExecRequest{
		SQL:    sql,
		Format: "ArrowStream",
	})
	if reqErr != nil {
		err = eh.Errorf("regex_explorer: chlocal cap request: %w", reqErr)
		return
	}
	if repErr := rep.Err(); repErr != nil {
		_ = rep.Close()
		err = eh.Errorf("regex_explorer: chlocal exec: %w", repErr)
		return
	}
	rdrObj, rdrErr := ipc.NewReader(rep, ipc.WithAllocator(alloc))
	if rdrErr != nil {
		_ = rep.Close()
		err = eh.Errorf("regex_explorer: arrow reader: %w", rdrErr)
		return
	}
	rdr = rdrObj
	closer = rep
	return
}
