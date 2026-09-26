package httpegress

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// CallRecord is one fetch the service answered or refused (ADR-0262 §SD5).
type CallRecord struct {
	Id             uint64
	At             time.Time
	Sender         app.AppIdT
	SenderInstance uint64
	Destination    string
	Purpose        string
	Sensitivity    queryengine.SensitivityE
	Method         string
	// URL is the request URL, redacted of any password.
	URL     string
	Status  int
	Bytes   int
	Elapsed time.Duration
	Refused bool
	Error   string
}

// DestinationRecord is one registered destination as the host resolved it.
type DestinationRecord struct {
	Name        string
	Description string
	Prefixes    []string
	Local       bool
	CAFile      string
	InsecureTLS bool
	UserAgent   string
	Timeout     time.Duration
	MaxBody     int64
	// Error is why the destination cannot be served; empty when it can.
	Error string
}

// record appends rec to the bounded ring.
func (inst *Service) record(rec CallRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.next++
	rec.Id = inst.next
	inst.calls = append(inst.calls, rec)
	if over := len(inst.calls) - inst.cfg.KeepCalls; over > 0 {
		inst.calls = append([]CallRecord(nil), inst.calls[over:]...)
	}
}

// Calls returns the kept records, oldest first.
func (inst *Service) Calls() (recs []CallRecord) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	recs = append([]CallRecord(nil), inst.calls...)
	return
}

// Destinations returns the registry as resolved, in registration order.
func (inst *Service) Destinations() (recs []DestinationRecord) {
	recs = make([]DestinationRecord, 0, len(inst.order))
	for _, name := range inst.order {
		r := inst.dests[name]
		rec := DestinationRecord{
			Name: name, Description: r.spec.Description, Prefixes: r.dest.Prefixes, Local: r.local,
			CAFile: r.dest.CAFile, InsecureTLS: r.dest.InsecureTLS, UserAgent: r.dest.UserAgent,
			Timeout: r.dest.Timeout, MaxBody: r.dest.MaxBodyBytes,
		}
		if r.err != nil {
			rec.Error = r.err.Error()
		}
		recs = append(recs, rec)
	}
	return
}

// CallsI is the read side the introspection providers need.
type CallsI interface {
	Calls() []CallRecord
	Destinations() []DestinationRecord
}

// RegisterIntrospect registers keelson('http_calls') and
// keelson('http_destinations') over svc; nil leaves both empty rather
// than absent.
func RegisterIntrospect(reg *introspect.Registry, svc CallsI) (err error) {
	if err = reg.Register(callsProvider{svc: svc}); err != nil {
		return
	}
	return reg.Register(destinationsProvider{svc: svc})
}

type callsProvider struct{ svc CallsI }

func (callsProvider) Name() string                         { return TableCalls }
func (callsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (callsProvider) Schema() *arrow.Schema                { return callsTable(nil).Schema() }

func (p callsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []CallRecord
	if p.svc != nil {
		rows = p.svc.Calls()
	}
	rec = callsTable(rows).Build(proj, len(rows))
	return
}

func callsTable(rows []CallRecord) *introspect.Table {
	return introspect.NewTable().
		Uint64("id", func(i int) uint64 { return rows[i].Id }).
		String("at", func(i int) string { return rows[i].At.UTC().Format(time.RFC3339Nano) }).
		String("app_id", func(i int) string { return string(rows[i].Sender) }).
		Uint64("instance_key", func(i int) uint64 { return rows[i].SenderInstance }).
		String("destination", func(i int) string { return rows[i].Destination }).
		String("purpose", func(i int) string { return rows[i].Purpose }).
		String("sensitivity", func(i int) string { return sensitivityName(rows[i].Sensitivity) }).
		String("method", func(i int) string { return rows[i].Method }).
		String("url", func(i int) string { return rows[i].URL }).
		Int64("status", func(i int) int64 { return int64(rows[i].Status) }).
		Int64("bytes", func(i int) int64 { return int64(rows[i].Bytes) }).
		Int64("elapsed_ms", func(i int) int64 { return rows[i].Elapsed.Milliseconds() }).
		Bool("refused", func(i int) bool { return rows[i].Refused }).
		String("error", func(i int) string { return rows[i].Error })
}

type destinationsProvider struct{ svc CallsI }

func (destinationsProvider) Name() string                         { return TableDestinations }
func (destinationsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (destinationsProvider) Schema() *arrow.Schema                { return destinationsTable(nil).Schema() }

func (p destinationsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []DestinationRecord
	if p.svc != nil {
		rows = p.svc.Destinations()
	}
	rec = destinationsTable(rows).Build(proj, len(rows))
	return
}

func destinationsTable(rows []DestinationRecord) *introspect.Table {
	return introspect.NewTable().
		String("name", func(i int) string { return rows[i].Name }).
		String("subject", func(i int) string { return Subject(rows[i].Name) }).
		String("description", func(i int) string { return rows[i].Description }).
		StringList("prefixes", func(i int) []string { return rows[i].Prefixes }).
		Bool("local", func(i int) bool { return rows[i].Local }).
		String("ca_file", func(i int) string { return rows[i].CAFile }).
		Bool("insecure_tls", func(i int) bool { return rows[i].InsecureTLS }).
		String("user_agent", func(i int) string { return rows[i].UserAgent }).
		Int64("timeout_ms", func(i int) int64 { return rows[i].Timeout.Milliseconds() }).
		Int64("max_body_bytes", func(i int) int64 { return rows[i].MaxBody }).
		String("error", func(i int) string { return rows[i].Error })
}

func sensitivityName(s queryengine.SensitivityE) (name string) {
	if s == queryengine.SensitivityConfined {
		return "confined"
	}
	return "ordinary"
}
