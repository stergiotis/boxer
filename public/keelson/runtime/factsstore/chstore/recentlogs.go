package chstore

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// recentLogsColumnExprs gathers the envelope projections of a log row that
// the log reads share.
type recentLogsColumnExprs struct {
	id      string
	tsSec   string
	appId   string
	level   string
	caller  string
	service string
	message string
	errStr  string
	stack   string
}

func buildColumnExprs() (e recentLogsColumnExprs) {
	e.id = hId
	// `ts` is z64 — emitted by the leeway DDL pipeline as
	// DateTime64(9,'UTC') (nano precision). We coerce to unix-seconds
	// so the Go side can re-hydrate via time.Unix without the
	// timezone-decode ambiguity TabSeparated otherwise has. Sub-second
	// precision is discarded here by design; callers needing per-event
	// ordering fall back to the `id` column.
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", hTs)
	e.appId = sym(vocab.MembRuntimeApp)
	e.level = sym(vocab.MembLogLevel)
	e.caller = sym(vocab.MembLogCaller)
	e.service = sym(vocab.MembLogService)
	e.message = str(vocab.MembLogMessage)
	e.errStr = str(vocab.MembLogError)
	e.stack = text(vocab.MembLogStack)
	return
}
