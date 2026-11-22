package introspecthttp

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/csv"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// ExposedPathPrefix is the path an exposed dataset is served under:
// ExposedPathPrefix + alias for its Arrow stream, and + "/csv" after it for
// CSV (ADR-0407). A request presents the exposure's token as
// "Authorization: Bearer <token>"; the path carries no secret, so the
// listener, a proxy and ClickHouse's query log may record it.
const ExposedPathPrefix = "/exposed/"

// exposedNotFound is the one answer to a request that opens no dataset.
const exposedNotFound = "not found\n"

// handleExposed serves an exposed dataset's Arrow stream. It is a route of
// its own, beside /table: a token opens only the alias it was minted for
// and never a handle, and a handle never opens an exposure.
func (s *Server) handleExposed(w http.ResponseWriter, r *http.Request) {
	s.serveExposed(w, r, introspect.ExposedFormatArrowStream)
}

// handleExposedCSV serves an exposed dataset as CSV with a header line.
func (s *Server) handleExposedCSV(w http.ResponseWriter, r *http.Request) {
	s.serveExposed(w, r, introspect.ExposedFormatCSV)
}

// serveExposed answers one request for an exposed dataset. Every request
// that opens nothing gets the same 404 and the same body, whatever the
// reason, so an answer never confirms that an alias is exposed or that a
// token once existed. The token is read from the Authorization header and
// is never logged.
func (s *Server) serveExposed(w http.ResponseWriter, r *http.Request, format introspect.ExposedFormatE) {
	alias := r.PathValue("alias")
	token, hasToken := bearerToken(r.Header.Get("Authorization"))
	src, hasSource := s.reg.ExposureSource()
	if !hasToken || !hasSource || !introspect.ValidTableName(alias) {
		notExposed(w)
		return
	}
	rd, status := src.OpenExposed(alias, token, introspect.ExposedRequest{
		Head: r.Method == http.MethodHead, Format: format, Range: r.Header.Get("Range"),
		Remote: r.RemoteAddr, UserAgent: r.UserAgent(),
	})
	switch status {
	case introspect.ExposedOpen:
	case introspect.ExposedBusy:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "the exposure has as many reads open as it admits; retry", http.StatusServiceUnavailable)
		return
	default:
		notExposed(w)
		return
	}
	cw := &countingResponseWriter{ResponseWriter: w}
	w.Header().Set("Cache-Control", "no-store")
	var err error
	if format == introspect.ExposedFormatCSV {
		err = writeExposedCSV(cw, r, rd.Reader)
	} else {
		w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
		w.Header().Set("ETag", fmt.Sprintf("%q", fmt.Sprintf("%s-r%d", alias, rd.Revision)))
		tracked := &erroringReadSeeker{rs: rd.Reader}
		http.ServeContent(cw, r, "", time.Time{}, tracked)
		err = tracked.err
	}
	rd.Done(cw.n, err)
	if err != nil {
		if cw.n == 0 && !cw.wroteHeader {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		// The status is out: end the connection so the reader sees a failed
		// fetch rather than a short result.
		panic(http.ErrAbortHandler)
	}
}

// notExposed is the answer to a request that opens no exposed dataset.
func notExposed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, exposedNotFound)
}

// bearerToken is the token of an "Authorization: Bearer <token>" header.
func bearerToken(header string) (token string, ok bool) {
	scheme, rest, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(rest)
	return token, token != ""
}

// errCSVColumn is a dataset with a column CSV does not carry.
var errCSVColumn = errors.New("the dataset has a column CSV does not carry; read it as ArrowStream")

// writeExposedCSV streams the Arrow stream rc as CSV with a header line, one
// record batch at a time. A HEAD answers the headers alone; CSV is written
// as it is read, so it has no length and takes no range.
func writeExposedCSV(w *countingResponseWriter, r *http.Request, rc io.Reader) (err error) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8; header=present")
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	rdr, err := ipc.NewReader(rc)
	if err != nil {
		return
	}
	defer rdr.Release()
	cw := csv.NewWriter(w, rdr.Schema(), csv.WithHeader(true), csv.WithNullWriter(""))
	wrote := false
	for rdr.Next() {
		if err = cw.Write(rdr.RecordBatch()); err != nil {
			return errors.Join(errCSVColumn, err)
		}
		wrote = true
	}
	if err = rdr.Err(); err != nil {
		return
	}
	if !wrote {
		// No batch: the header line alone.
		empty := array.NewRecordBuilder(memory.DefaultAllocator, rdr.Schema())
		rec := empty.NewRecordBatch()
		err = cw.Write(rec)
		rec.Release()
		empty.Release()
		if err != nil {
			return errors.Join(errCSVColumn, err)
		}
	}
	cw.Flush()
	return cw.Error()
}

// countingResponseWriter counts the body bytes written and whether the
// status went out.
type countingResponseWriter struct {
	http.ResponseWriter
	n           uint64
	wroteHeader bool
}

func (inst *countingResponseWriter) WriteHeader(code int) {
	inst.wroteHeader = true
	inst.ResponseWriter.WriteHeader(code)
}

func (inst *countingResponseWriter) Write(p []byte) (n int, err error) {
	inst.wroteHeader = true
	n, err = inst.ResponseWriter.Write(p)
	inst.n += uint64(n)
	return
}
