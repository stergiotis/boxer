package queryrunfacts

import (
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// CaptureStateE is what a reader can say about the capture pipeline from
// the server's side: whether the refreshable view exists, and whether its
// last refresh reached queryrunsd.
type CaptureStateE uint8

const (
	// CaptureUnknown: the status query itself did not answer (a server
	// without system.view_refreshes, or no permission to read it).
	CaptureUnknown CaptureStateE = iota
	// CaptureAbsent: no capture view on this server; queryrunsd has never
	// reconciled it here.
	CaptureAbsent
	// CaptureFailing: the view exists and its latest refresh failed —
	// typically queryrunsd is not running, so the pull is refused.
	CaptureFailing
	// CaptureRunning: the latest refresh succeeded.
	CaptureRunning
)

// CaptureStatus is one reading of the capture view's refresh state.
type CaptureStatus struct {
	State       CaptureStateE
	LastSuccess time.Time
	LastRefresh time.Time
	// Exception is the first line of the last refresh's error, when it
	// failed.
	Exception string
}

// CaptureStatusSql asks system.view_refreshes for the capture view in
// database. queryrunsvc leaves refresh health to ClickHouse on purpose;
// this is the read side of that choice.
func CaptureStatusSql(database string) (sql string) {
	return "SELECT status, toUnixTimestamp(ifNull(last_success_time, toDateTime(0))), " +
		"toUnixTimestamp(ifNull(last_refresh_time, toDateTime(0))), " +
		"replaceRegexpAll(splitByChar('\\n', exception)[1], '\\t', ' ') " +
		"FROM system.view_refreshes WHERE database = " + quoteLiteral(database) +
		" AND view = " + quoteLiteral(MvBaseName) + " FORMAT TabSeparated"
}

// ParseCaptureStatus reads the answer to [CaptureStatusSql]. queryErr is
// what the query returned, if anything; it makes the state unknown rather
// than an error, since the status is advice next to the history, not a
// precondition for it.
func ParseCaptureStatus(raw []byte, queryErr error) (st CaptureStatus, err error) {
	if queryErr != nil {
		st.State = CaptureUnknown
		return
	}
	line := strings.TrimRight(string(raw), "\n")
	if line == "" {
		st.State = CaptureAbsent
		return
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	parts := strings.Split(line, "\t")
	if len(parts) != 4 {
		err = eb.Build().Int("got", len(parts)).Str("line", line).Errorf("queryrunfacts: capture status has the wrong column count")
		return
	}
	succ, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		err = eb.Build().Str("raw", parts[1]).Errorf("queryrunfacts: capture status: last success: %w", err)
		return
	}
	refr, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		err = eb.Build().Str("raw", parts[2]).Errorf("queryrunfacts: capture status: last refresh: %w", err)
		return
	}
	if succ > 0 {
		st.LastSuccess = time.Unix(succ, 0).UTC()
	}
	if refr > 0 {
		st.LastRefresh = time.Unix(refr, 0).UTC()
	}
	st.Exception = UnescapeTabSeparated(parts[3])
	// The exception column keeps the last error after a later success, so
	// it is read as current only when no success followed the failed
	// refresh.
	if st.Exception != "" && (succ == 0 || refr > succ) {
		st.State = CaptureFailing
		return
	}
	st.State = CaptureRunning
	return
}
