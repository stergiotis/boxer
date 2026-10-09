package regex_explorer

// ClickHouse transport: capability-mediated via ADR-0028's
// chlocalbroker. The regex explorer publishes a request on
// `ch.local.exec.regex_explorer`; the runtime broker drains a
// pre-spawned `clickhouse-local` worker's stdout and replies with
// the Arrow IPC bytes. No subprocess management lives in this
// package post-M2.

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// chLocalPoolName is the pool the regex explorer asks the broker for.
// Each app gets an isolated pool so warm workers are not shared with
// other consumers (separate accounting, watchdog, future cache).
const chLocalPoolName = "regex_explorer"

// ChLocalCapPattern is the SubjectFilter pattern the app declares in
// its Manifest.Caps — see app_register.go.
//
// Exported because the standalone app is not the only host: a widget that
// embeds [EmbeddedApp] carries no manifest of its own, so the capability
// has to be declared by whichever app hosts the widget. Without it the
// explorer's ClickHouse tabs and its SD1 tripwire are denied by the
// broker, with the reason on the tabs — the honest degradation, but
// only the grant makes them work.
const ChLocalCapPattern = chlocalbroker.SubjectExecPrefix + chLocalPoolName

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

// isEngineRejection reports whether err is ClickHouse itself refusing the
// query — a `DB::Exception` from the worker — as opposed to a failure on
// the way there (bus timeout, refused capability, no bus, pool trouble).
// The broker carries no typed error, so this keys on the exception marker
// clickhouse-local writes to stderr, which the broker forwards in the
// error text.
func isEngineRejection(err error) bool {
	return err != nil && strings.Contains(err.Error(), "DB::Exception")
}

// exceptionCode matches the error-code name ClickHouse puts in parentheses
// at the end of an exception, "(BAD_ARGUMENTS)".
var exceptionCode = regexp.MustCompile(`\(([A-Z][A-Z0-9_]+)\)`)

// clickHouseMessage reduces err to what a user needs to read: ClickHouse's
// own exception text and its code, without the transport chain the broker
// wraps it in ("execute query: … chlocalpool: worker exit: exit status 36
// (stderr: Code: 36. DB::Exception: …") or the echoed query ("In scope
// SELECT …"). A refused capability — the usual state of an explorer
// embedded in a host that does not grant ChLocalCapPattern — is said in
// those terms. Anything else comes back whole.
func clickHouseMessage(err error) (msg string) {
	if errors.Is(err, inprocbus.ErrPermissionViolation) {
		msg = "this window may not query ClickHouse — its host app does not grant " + ChLocalCapPattern
		return
	}
	msg = err.Error()
	const marker = "DB::Exception: "
	i := strings.Index(msg, marker)
	if i < 0 {
		return
	}
	rest := msg[i+len(marker):]
	text := rest
	if j := strings.Index(text, ": In scope "); j >= 0 {
		text = text[:j]
	}
	if j := strings.IndexByte(text, '\n'); j >= 0 {
		text = text[:j]
	}
	text = strings.TrimSuffix(strings.TrimSpace(text), ".")
	if m := exceptionCode.FindStringSubmatch(rest); m != nil && !strings.Contains(text, m[0]) {
		text += " " + m[0]
	}
	msg = text
	return
}
