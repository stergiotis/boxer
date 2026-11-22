package sessiondata

import (
	"encoding/json/v2"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubServer is enough of a ClickHouse HTTP server for the capability's
// own statements: sessions that hold temporary tables, session_check,
// a session that runs one statement at a time, external data, and the
// three statements the capability composes besides its creates.
type stubServer struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	sessions map[string]*stubSession
	requests []stubRequest
	// rowsFor answers how many rows a create's read yields; nil is 10.
	rowsFor func(inner string) uint64
	// bytesPerRow sizes a table; zero is 8.
	bytesPerRow uint64
	// delay holds every statement this long, to make overlap visible.
	delay time.Duration
	// overlaps counts statements that arrived while their session was busy.
	overlaps int
	// fail makes a statement matching the pattern fail with the message.
	fail map[*regexp.Regexp]string
	// afterProbe runs after a probe of every table is answered.
	afterProbe func()
	// afterCreate runs after a create is answered.
	afterCreate func()
	// madeButFailed is how many of the next creates make their table and
	// then answer an error, as a create the caller stopped waiting for does.
	madeButFailed int
}

type stubSession struct {
	tables map[string]stubTable
	busy   bool
}

type stubTable struct {
	rows  uint64
	bytes uint64
}

// stubRequest is one request as the stub received it.
type stubRequest struct {
	SQL        string
	Params     map[string]string
	Files      map[string]string
	LogComment string
}

func newStub(t *testing.T) (inst *stubServer) {
	inst = &stubServer{t: t, sessions: map[string]*stubSession{}, fail: map[*regexp.Regexp]string{}}
	inst.srv = httptest.NewServer(http.HandlerFunc(inst.serve))
	t.Cleanup(inst.srv.Close)
	return
}

func (inst *stubServer) url() (u string) { return inst.srv.URL }

var (
	reCreate   = regexp.MustCompile("(?s)^CREATE TEMPORARY TABLE `([A-Za-z0-9_]+)` ENGINE = MergeTree ORDER BY (.+?) AS SELECT \\* FROM \\(\n(.*)\n\\) LIMIT (\\d+)$")
	reDrop     = regexp.MustCompile("^DROP TEMPORARY TABLE IF EXISTS `([A-Za-z0-9_]+)`$")
	reDescribe = regexp.MustCompile("^DESCRIBE TABLE `([A-Za-z0-9_]+)`$")
	reProbeOne = regexp.MustCompile(`AND name = '([A-Za-z0-9_]+)'$`)
	reSdTable  = regexp.MustCompile(`__sd_[A-Za-z0-9_]+_r\d+`)
	reAdhoc    = regexp.MustCompile(`__adhoc_\d+`)
)

func (inst *stubServer) serve(w http.ResponseWriter, r *http.Request) {
	req := stubRequest{Params: map[string]string{}, Files: map[string]string{}}
	for k, v := range r.URL.Query() {
		req.Params[k] = v[0]
	}
	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			raw, _ := io.ReadAll(p)
			if p.FileName() != "" {
				req.Files[p.FormName()] = string(raw)
			} else if p.FormName() == "query" {
				req.SQL = string(raw)
			}
		}
	} else {
		raw, _ := io.ReadAll(r.Body)
		req.SQL = string(raw)
	}
	req.LogComment = req.Params["log_comment"]

	inst.mu.Lock()
	inst.requests = append(inst.requests, req)
	id := req.Params["session_id"]
	var sess *stubSession
	if id != "" {
		sess = inst.sessions[id]
		if sess == nil {
			if req.Params["session_check"] == "1" {
				inst.mu.Unlock()
				fail(w, 372, "Session "+id+" not found. (SESSION_NOT_FOUND)")
				return
			}
			sess = &stubSession{tables: map[string]stubTable{}}
			inst.sessions[id] = sess
		}
		if sess.busy {
			inst.overlaps++
			inst.mu.Unlock()
			fail(w, 373, "Session "+id+" is locked by a concurrent client. (SESSION_IS_LOCKED)")
			return
		}
		sess.busy = true
	}
	delay := inst.delay
	inst.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	status, code, body := inst.answer(req, sess)
	inst.mu.Lock()
	if sess != nil {
		sess.busy = false
	}
	inst.mu.Unlock()
	if status != http.StatusOK {
		fail(w, code, body)
		return
	}
	_, _ = w.Write([]byte(body))
	if req.SQL == probeStatement && inst.afterProbe != nil {
		inst.afterProbe()
	}
	if reCreate.MatchString(req.SQL) && inst.afterCreate != nil {
		inst.afterCreate()
	}
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("X-ClickHouse-Exception-Code", strconv.Itoa(code))
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("Code: " + strconv.Itoa(code) + ". DB::Exception: " + msg))
}

func (inst *stubServer) answer(req stubRequest, sess *stubSession) (status int, code int, body string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for re, msg := range inst.fail {
		if re.MatchString(req.SQL) {
			return http.StatusInternalServerError, 1, msg
		}
	}
	if sess == nil {
		return http.StatusInternalServerError, 1, "the stub answers only statements in a session"
	}
	sql := req.SQL
	switch {
	case reCreate.MatchString(sql):
		m := reCreate.FindStringSubmatch(sql)
		table, inner := m[1], m[3]
		limit, _ := strconv.ParseUint(m[4], 10, 64)
		for _, dep := range reSdTable.FindAllString(inner, -1) {
			if _, ok := sess.tables[dep]; !ok {
				return http.StatusInternalServerError, 60, "Unknown table expression identifier '" + dep + "'. (UNKNOWN_TABLE)"
			}
		}
		for _, ext := range reAdhoc.FindAllString(inner, -1) {
			if _, ok := req.Files[ext]; !ok || req.Params[ext+"_structure"] == "" {
				return http.StatusInternalServerError, 60, "Unknown table expression identifier '" + ext + "'. (UNKNOWN_TABLE)"
			}
		}
		if _, exists := sess.tables[table]; exists {
			return http.StatusInternalServerError, 57, "Table " + table + " already exists. (TABLE_ALREADY_EXISTS)"
		}
		rows := uint64(10)
		if inst.rowsFor != nil {
			rows = inst.rowsFor(inner)
		}
		rows = min(rows, limit)
		per := inst.bytesPerRow
		if per == 0 {
			per = 8
		}
		sess.tables[table] = stubTable{rows: rows, bytes: rows * per}
		if inst.madeButFailed > 0 {
			inst.madeButFailed--
			return http.StatusInternalServerError, 394, "Query was cancelled. (QUERY_WAS_CANCELLED)"
		}
		return http.StatusOK, 0, ""
	case reDrop.MatchString(sql):
		delete(sess.tables, reDrop.FindStringSubmatch(sql)[1])
		return http.StatusOK, 0, ""
	case reDescribe.MatchString(sql):
		return http.StatusOK, 0, `{"name":"x","type":"UInt64"}` + "\n" + `{"name":"s","type":"String"}` + "\n"
	case strings.HasPrefix(sql, probeStatement):
		only := ""
		if m := reProbeOne.FindStringSubmatch(sql); m != nil {
			only = m[1]
		}
		var b strings.Builder
		for name, tb := range sess.tables {
			if only != "" && name != only {
				continue
			}
			raw, _ := json.Marshal(map[string]any{"name": name, "total_rows": tb.rows, "total_bytes": tb.bytes})
			b.Write(raw)
			b.WriteString("\n")
		}
		return http.StatusOK, 0, b.String()
	}
	return http.StatusInternalServerError, 62, "the stub does not know the statement: " + sql
}

// expireAll drops every session, as a server does at its timeout.
func (inst *stubServer) expireAll() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.sessions = map[string]*stubSession{}
}

// creates lists the tables created, in order.
func (inst *stubServer) creates() (tables []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, r := range inst.requests {
		if m := reCreate.FindStringSubmatch(r.SQL); m != nil {
			tables = append(tables, m[1])
		}
	}
	return
}

// drops lists the tables dropped, in order.
func (inst *stubServer) drops() (tables []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, r := range inst.requests {
		if m := reDrop.FindStringSubmatch(r.SQL); m != nil {
			tables = append(tables, m[1])
		}
	}
	return
}

// lastCreate is the last create request.
func (inst *stubServer) lastCreate() (req stubRequest) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, r := range inst.requests {
		if reCreate.MatchString(r.SQL) {
			req = r
		}
	}
	return
}

// tablesOf lists a session's tables.
func (inst *stubServer) tablesOf(id string) (names []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if s := inst.sessions[id]; s != nil {
		for n := range s.tables {
			names = append(names, n)
		}
	}
	return
}

// sessionIds lists the sessions the stub holds.
func (inst *stubServer) sessionIds() (ids []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for id := range inst.sessions {
		ids = append(ids, id)
	}
	return
}
