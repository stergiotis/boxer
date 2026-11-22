package play

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
)

// sdStub is enough of a ClickHouse HTTP server for the session data
// service's statements and for play's runs: sessions holding temporary
// tables, session_check, one statement per session, external data, and an
// Arrow answer to a run whose tables the session holds.
type sdStub struct {
	srv *httptest.Server

	mu       sync.Mutex
	sessions map[string]map[string]uint64
	runs     []sdRun
	// loseOnRun drops the session of the next run that many times and
	// answers it as a lost session, as a server whose session expired
	// between the prepare and the run does.
	loseOnRun int
	// failRuns fails every run with a server error.
	failRuns bool
	// hold delays every run until it is closed, when non-nil.
	hold chan struct{}
}

// sdRun is one of play's runs as the stub received it.
type sdRun struct {
	SQL    string
	Params map[string]string
	Files  []string
}

const sdProbe = "SELECT name, total_rows, total_bytes FROM system.tables WHERE is_temporary AND startsWith(name, '__sd_')"

var (
	sdCreate   = regexp.MustCompile("(?s)^CREATE TEMPORARY TABLE `([A-Za-z0-9_]+)` ENGINE = MergeTree ORDER BY (.+?) AS SELECT \\* FROM \\(\n(.*)\n\\) LIMIT (\\d+)$")
	sdDrop     = regexp.MustCompile("^DROP TEMPORARY TABLE IF EXISTS `([A-Za-z0-9_]+)`$")
	sdDescribe = regexp.MustCompile("^DESCRIBE TABLE `([A-Za-z0-9_]+)`$")
	sdProbeOne = regexp.MustCompile(`AND name = '([A-Za-z0-9_]+)'$`)
	sdTable    = regexp.MustCompile(`__sd_[A-Za-z0-9_]+_r\d+`)
	sdAdhoc    = regexp.MustCompile(`__adhoc_\d+`)
)

func newSdStub(t *testing.T) (inst *sdStub) {
	inst = &sdStub{sessions: map[string]map[string]uint64{}}
	inst.srv = httptest.NewServer(http.HandlerFunc(inst.serve))
	t.Cleanup(inst.srv.Close)
	return
}

func (inst *sdStub) url() string { return inst.srv.URL }

func sdFail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("X-ClickHouse-Exception-Code", strconv.Itoa(code))
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("Code: " + strconv.Itoa(code) + ". DB::Exception: " + msg))
}

func (inst *sdStub) serve(w http.ResponseWriter, r *http.Request) {
	params := map[string]string{}
	for k, v := range r.URL.Query() {
		params[k] = v[0]
	}
	var sql string
	var files []string
	mediaType, mp, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType == "multipart/form-data" {
		mr := multipart.NewReader(r.Body, mp["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			raw, _ := io.ReadAll(p)
			if p.FileName() != "" {
				files = append(files, p.FormName())
			} else if p.FormName() == "query" {
				sql = string(raw)
			}
		}
	} else {
		raw, _ := io.ReadAll(r.Body)
		sql = string(raw)
	}
	if q := params["query"]; q != "" {
		sql = q + sql
	}
	id := params["session_id"]
	inst.mu.Lock()
	var tables map[string]uint64
	if id != "" {
		tables = inst.sessions[id]
		if tables == nil {
			if params["session_check"] == "1" {
				inst.mu.Unlock()
				sdFail(w, 372, "Session "+id+" not found. (SESSION_NOT_FOUND)")
				return
			}
			tables = map[string]uint64{}
			inst.sessions[id] = tables
		}
	}
	isRun := strings.Contains(sql, "FORMAT ArrowStream") || params["default_format"] == "ArrowStream" ||
		strings.Contains(sql, "FORMAT JSONCompact") || params["default_format"] == "JSONCompact"
	if isRun {
		inst.runs = append(inst.runs, sdRun{SQL: sql, Params: params, Files: files})
		if id != "" && inst.loseOnRun > 0 {
			inst.loseOnRun--
			delete(inst.sessions, id)
			inst.mu.Unlock()
			sdFail(w, 372, "Session "+id+" not found. (SESSION_NOT_FOUND)")
			return
		}
	}
	hold, failRuns := inst.hold, inst.failRuns
	inst.mu.Unlock()
	if isRun {
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		if failRuns {
			sdFail(w, 60, "Unknown table expression identifier 'x'. (UNKNOWN_TABLE)")
			return
		}
		inst.answerRun(w, sql, tables, files)
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	switch {
	case tables == nil:
		sdFail(w, 1, "the stub answers the service's statements only in a session")
	case sdCreate.MatchString(sql):
		m := sdCreate.FindStringSubmatch(sql)
		for _, dep := range sdTable.FindAllString(m[3], -1) {
			if _, ok := tables[dep]; !ok {
				sdFail(w, 60, "Unknown table expression identifier '"+dep+"'. (UNKNOWN_TABLE)")
				return
			}
		}
		tables[m[1]] = 3
	case sdDrop.MatchString(sql):
		delete(tables, sdDrop.FindStringSubmatch(sql)[1])
	case sdDescribe.MatchString(sql):
		_, _ = w.Write([]byte(`{"name":"x","type":"UInt64"}` + "\n"))
	case strings.HasPrefix(sql, sdProbe):
		only := ""
		if m := sdProbeOne.FindStringSubmatch(sql); m != nil {
			only = m[1]
		}
		var b strings.Builder
		for name, rows := range tables {
			if only != "" && name != only {
				continue
			}
			raw, _ := json.Marshal(map[string]any{"name": name, "total_rows": rows, "total_bytes": rows * 8})
			b.Write(raw)
			b.WriteString("\n")
		}
		_, _ = w.Write([]byte(b.String()))
	default:
		sdFail(w, 62, "the stub does not know the statement")
	}
}

// answerRun answers a run with a three-row Arrow stream once every
// session table and external table it reads is there.
func (inst *sdStub) answerRun(w http.ResponseWriter, sql string, tables map[string]uint64, files []string) {
	inst.mu.Lock()
	for _, t := range sdTable.FindAllString(sql, -1) {
		if _, ok := tables[t]; !ok {
			inst.mu.Unlock()
			sdFail(w, 60, "Unknown table expression identifier '"+t+"'. (UNKNOWN_TABLE)")
			return
		}
	}
	inst.mu.Unlock()
	for _, x := range sdAdhoc.FindAllString(sql, -1) {
		if !slices.Contains(files, x) {
			sdFail(w, 60, "Unknown table expression identifier '"+x+"'. (UNKNOWN_TABLE)")
			return
		}
	}
	if strings.Contains(sql, "JSONCompact") {
		_, _ = w.Write([]byte(`{"meta":[{"name":"c"}],"data":[["3"]],"rows":1}`))
		return
	}
	_, _ = w.Write(arrowRows())
}

func arrowRows() []byte {
	schema := arrow.NewSchema([]arrow.Field{{Name: "x", Type: arrow.PrimitiveTypes.Uint64}}, nil)
	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer rb.Release()
	rb.Field(0).(*array.Uint64Builder).AppendValues([]uint64{1, 2, 3}, nil)
	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	_ = w.Write(rec)
	_ = w.Close()
	return buf.Bytes()
}

// lastRun is the last run the stub received.
func (inst *sdStub) lastRun() (r sdRun) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if len(inst.runs) > 0 {
		r = inst.runs[len(inst.runs)-1]
	}
	return
}

// sdDispatcher attests every call of a task it knows and lists the
// destinations its grant was given.
type sdDispatcher struct {
	mu    sync.Mutex
	dests map[string][]string
}

func (inst *sdDispatcher) CallContext(task string, epoch uint64, call string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, known := inst.dests[task]; !known {
		return cc, false, "no task by that id"
	}
	return app.CallContext{Task: task, Epoch: epoch, Call: call, App: sender, Instance: senderInstance}, true, ""
}

func (inst *sdDispatcher) AllowDestination(task string, _ uint64, destination string) (ok bool, reason string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if slices.Contains(inst.dests[task], destination) {
		return true, ""
	}
	return false, "not granted"
}

func (inst *sdDispatcher) TaskLive(task string) bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, ok := inst.dests[task]
	return ok
}

// sdAdhocReader serves ad-hoc datasets by alias, held to a grant as the
// dataset service holds a read.
type sdAdhocReader struct{ streams map[string][]byte }

func (inst sdAdhocReader) Read(_ context.Context, alias string, _ adhocdata.Identity, obo *app.OnBehalfOf) (res adhocdata.ReadResult, err error) {
	s, ok := inst.streams[alias]
	if !ok {
		return res, adhocdata.ErrNoLiveDataset
	}
	if obo != nil && !slices.Contains(obo.Destinations, DestinationKeelson(alias)) {
		return res, &adhocdata.GrantError{Destination: DestinationKeelson(alias), Reason: "the grant does not list " + DestinationKeelson(alias)}
	}
	return adhocdata.ReadResult{Alias: alias, Handle: "adhoc_00000000000000aa", Revision: 1, Rows: 3, ArrowIPCStream: s, StreamDigest: "d"}, nil
}

// sdRig is a session data service over a stub server and a bus, and the
// means to make play clients that speak to it as windows.
type sdRig struct {
	t    *testing.T
	stub *sdStub
	bus  *inprocbus.Inst
	svc  *sessiondata.Service
	disp *sdDispatcher
	host string
}

func newSdRig(t *testing.T) (r *sdRig) {
	t.Helper()
	r = &sdRig{t: t, stub: newSdStub(t), bus: inprocbus.NewInst(zerolog.Nop()), disp: &sdDispatcher{dests: map[string][]string{}}}
	svc, err := sessiondata.NewService(sessiondata.Config{Bus: r.bus, Log: zerolog.Nop(), SweepInterval: -1,
		Adhoc: sdAdhocReader{streams: map[string][]byte{"items": arrowRows()}}})
	require.NoError(t, err)
	svc.SetDispatcher(r.disp)
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	r.svc, r.host = svc, endpointHost(r.stub.url())
	return
}

// grant gives task's grant the destinations.
func (inst *sdRig) grant(task string, dests ...string) {
	inst.disp.mu.Lock()
	inst.disp.dests[task] = append(inst.disp.dests[task], dests...)
	inst.disp.mu.Unlock()
}

// window is a bus client with play's session caps, as a window of appId.
func (inst *sdRig) window(appId app.AppIdT, instance uint64) (bus app.BusI) {
	m := (&PlayLauncher{}).Manifest()
	caps := []app.SubjectFilter{{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionSub, Reason: "replies"}}
	for _, c := range m.Caps {
		if strings.HasPrefix(c.Pattern, sessiondata.SubjectPrefix) {
			caps = append(caps, c)
		}
	}
	cl := inst.bus.NewClient(appId, caps)
	cl.SetInstanceKey(instance)
	return cl
}

// client is a play client on the stub, wired as a window.
func (inst *sdRig) client(appId app.AppIdT, instance uint64) (c *Client) {
	c = NewClient(ClientConfig{URL: inst.stub.url(), User: "u", Password: "p"}, nil)
	c.SetSessionBus(inst.window(appId, instance))
	return
}
