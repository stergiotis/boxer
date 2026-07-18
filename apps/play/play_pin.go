package play

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"

	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// play_pin.go is Tier-1 result pinning (ADR-0115 S4 / plane C): persist
// the active result's Arrow batch as-is, any query, no classification.
// A pin is two objects on the user's own endpoint:
//
//   - runtime.pin_<fp> — a plain ClickHouse table with the result's OWN
//     schema (derived from the Arrow fields), holding the frozen rows.
//     The batch bytes go in verbatim via INSERT … FORMAT Arrow, and
//     "opening" a pin is ordinary SQL over that table — every play
//     panel works on frozen data with zero new display machinery.
//   - one metadata row in runtime.resultsets — content fingerprint
//     (play's lane fingerprint — the ResultSet identity of the entity
//     spine), provenance anchors (query_id, run/app identity, lane,
//     query text), and size accounting.
//
// Pins are content-addressed: re-pinning an identical result is a
// no-op (the fingerprint already has a metadata row). The plane-C
// ideal records ref-tuple lineage to the QueryRun fact and routes
// through a recordstore-generated store; this cut correlates by
// query_id/run_id (the S1 anchors) in a plain table instead — the
// ref-tuple lift and typed weaving are the S6 slice. Unpinning is
// deliberately absent in v1: pins are dropped by dropping their
// tables, an operator act.

const (
	// pinMetaTable is the qualified metadata table.
	pinMetaTable = "runtime.resultsets"
	// pinTablePrefix prefixes the per-pin data tables (qualified name:
	// runtime.pin_<16-hex fingerprint>).
	pinTablePrefix = "runtime.pin_"
	// pinTimeout bounds one pin round (DDL + data insert + metadata).
	pinTimeout = 60 * time.Second
	// pinBrowserLimit bounds the browser's metadata read.
	pinBrowserLimit = 100
)

// pinMetaDDL creates the metadata table. Plain readable columns — the
// browser is deliberately just SQL over this table.
const pinMetaDDL = `CREATE TABLE IF NOT EXISTS ` + pinMetaTable + ` (
  fingerprint UInt64,
  data_table String,
  pinned_at DateTime64(3,'UTC') DEFAULT now64(3),
  query_id String,
  run_id String,
  app String,
  lane String,
  query String,
  num_rows UInt64,
  num_cols UInt64
) ENGINE MergeTree() ORDER BY (fingerprint, pinned_at)`

// pinDataTableName derives the per-pin table from the content
// fingerprint — content-addressed, so identical results share one name.
func pinDataTableName(fingerprint uint64) string {
	return pinTablePrefix + fmt.Sprintf("%016x", fingerprint)
}

// arrowColumnType maps one Arrow field to the ClickHouse column type
// the pin table declares. Covers the types play results carry (scalars,
// strings, timestamps, dates, lists of those, dictionary-encoded
// values); anything else refuses loudly — a pin must not silently
// mangle data.
func arrowColumnType(dt arrow.DataType) (chType string, err error) {
	switch t := dt.(type) {
	case *arrow.BooleanType:
		chType = "Bool"
	case *arrow.Int8Type:
		chType = "Int8"
	case *arrow.Int16Type:
		chType = "Int16"
	case *arrow.Int32Type:
		chType = "Int32"
	case *arrow.Int64Type:
		chType = "Int64"
	case *arrow.Uint8Type:
		chType = "UInt8"
	case *arrow.Uint16Type:
		chType = "UInt16"
	case *arrow.Uint32Type:
		chType = "UInt32"
	case *arrow.Uint64Type:
		chType = "UInt64"
	case *arrow.Float32Type:
		chType = "Float32"
	case *arrow.Float64Type:
		chType = "Float64"
	case *arrow.StringType, *arrow.LargeStringType, *arrow.BinaryType, *arrow.LargeBinaryType:
		chType = "String"
	case *arrow.FixedSizeBinaryType:
		chType = fmt.Sprintf("FixedString(%d)", t.ByteWidth)
	case *arrow.Date32Type, *arrow.Date64Type:
		chType = "Date32"
	case *arrow.TimestampType:
		switch t.Unit {
		case arrow.Second:
			chType = "DateTime64(0,'UTC')"
		case arrow.Millisecond:
			chType = "DateTime64(3,'UTC')"
		case arrow.Microsecond:
			chType = "DateTime64(6,'UTC')"
		default:
			chType = "DateTime64(9,'UTC')"
		}
	case *arrow.ListType:
		inner, iErr := arrowColumnType(t.Elem())
		if iErr != nil {
			err = iErr
			return
		}
		chType = "Array(" + inner + ")"
	case *arrow.LargeListType:
		inner, iErr := arrowColumnType(t.Elem())
		if iErr != nil {
			err = iErr
			return
		}
		chType = "Array(" + inner + ")"
	case *arrow.DictionaryType:
		// ClickHouse's Arrow reader resolves dictionary indices to
		// values; declare the value type.
		chType, err = arrowColumnType(t.ValueType)
	default:
		err = eh.Errorf("play: pin: unsupported column type %s", dt)
	}
	return
}

// composePinTableDDL derives the frozen table's CREATE from the batch's
// own schema. Column names are quoted verbatim (play results carry names
// like "count()" and leeway wire names with ':'); a backtick inside a
// name has no safe quoting here and is refused.
func composePinTableDDL(tableName string, schema *arrow.Schema) (ddl string, err error) {
	if schema == nil || schema.NumFields() == 0 {
		err = eh.Errorf("play: pin: result has no columns")
		return
	}
	cols := make([]string, 0, schema.NumFields())
	for _, f := range schema.Fields() {
		if strings.ContainsRune(f.Name, '`') {
			err = eh.Errorf("play: pin: column name %q contains a backtick", f.Name)
			return
		}
		chType, tErr := arrowColumnType(f.Type)
		if tErr != nil {
			err = eb.Build().Str("column", f.Name).Errorf("play: pin: %w", tErr)
			return
		}
		if f.Nullable {
			// Arrays cannot be Nullable in ClickHouse; leave them plain
			// (Arrow nulls inside arrays do not occur in play results).
			if !strings.HasPrefix(chType, "Array(") {
				chType = "Nullable(" + chType + ")"
			}
		}
		cols = append(cols, "`"+f.Name+"` "+chType)
	}
	// Frozen, small, order-preserving: tuple() ordering keeps the
	// insertion order stable enough for a snapshot table.
	ddl = "CREATE TABLE IF NOT EXISTS " + tableName + " (\n  " +
		strings.Join(cols, ",\n  ") + "\n) ENGINE MergeTree() ORDER BY tuple()"
	return
}

// pinMetaRow is the metadata insert, shipped as JSONEachRow so string
// escaping is json.Marshal's problem.
type pinMetaRow struct {
	Fingerprint uint64 `json:"fingerprint"`
	DataTable   string `json:"data_table"`
	QueryId     string `json:"query_id"`
	RunId       string `json:"run_id"`
	App         string `json:"app"`
	Lane        string `json:"lane"`
	Query       string `json:"query"`
	NumRows     uint64 `json:"num_rows"`
	NumCols     uint64 `json:"num_cols"`
}

// pinStateE is the driver's user-visible phase.
type pinStateE uint8

const (
	pinIdle pinStateE = iota
	pinInFlight
	pinDone
	pinAlready
	pinFailed
)

// pinDriver owns the async pin round and its render-thread status.
type pinDriver struct {
	client *Client

	mu     sync.Mutex
	state  pinStateE
	lastFp uint64
	err    error
}

func newPinDriver(client *Client) *pinDriver { return &pinDriver{client: client} }

// status returns the render-thread view.
func (inst *pinDriver) status() (state pinStateE, fp uint64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.state, inst.lastFp, inst.err
}

// pin persists rec (retained here, released when the round ends).
// Single-flight; a click while a pin is in flight is dropped.
func (inst *pinDriver) pin(rec arrow.RecordBatch, meta pinMetaRow) {
	if inst.client == nil || rec == nil {
		return
	}
	inst.mu.Lock()
	if inst.state == pinInFlight {
		inst.mu.Unlock()
		return
	}
	inst.state = pinInFlight
	inst.lastFp = meta.Fingerprint
	inst.err = nil
	inst.mu.Unlock()

	rec.Retain()
	go func() {
		defer rec.Release()
		ctx, cancel := context.WithTimeout(context.Background(), pinTimeout)
		defer cancel()
		already, err := inst.doPin(ctx, rec, meta)
		inst.mu.Lock()
		switch {
		case err != nil:
			inst.state = pinFailed
			inst.err = err
		case already:
			inst.state = pinAlready
		default:
			inst.state = pinDone
		}
		inst.mu.Unlock()
	}()
}

// doPin is one pin round: metadata DDL, content-address dedup check,
// frozen-table DDL, the as-is Arrow insert, and the metadata row.
func (inst *pinDriver) doPin(ctx context.Context, rec arrow.RecordBatch, meta pinMetaRow) (already bool, err error) {
	cli := inst.client
	if _, err = cli.rawTsvQuery(ctx, pinMetaDDL); err != nil {
		err = eh.Errorf("play: pin: metadata ddl: %w", err)
		return
	}
	raw, err := cli.rawTsvQuery(ctx,
		"SELECT count() FROM "+pinMetaTable+" WHERE fingerprint = "+strconv.FormatUint(meta.Fingerprint, 10)+" FORMAT TabSeparated")
	if err != nil {
		err = eh.Errorf("play: pin: dedup check: %w", err)
		return
	}
	if strings.TrimSpace(string(raw)) != "0" {
		already = true
		return
	}
	ddl, err := composePinTableDDL(meta.DataTable, rec.Schema())
	if err != nil {
		return
	}
	if _, err = cli.rawTsvQuery(ctx, ddl); err != nil {
		err = eh.Errorf("play: pin: frozen-table ddl: %w", err)
		return
	}
	// The batch goes in as-is: Arrow IPC file bytes into FORMAT Arrow —
	// the same wire chclient.InsertArrow uses.
	buf := &bytes.Buffer{}
	w, err := ipc.NewFileWriter(buf, ipc.WithSchema(rec.Schema()))
	if err != nil {
		err = eh.Errorf("play: pin: ipc writer: %w", err)
		return
	}
	if err = w.Write(rec); err != nil {
		err = eh.Errorf("play: pin: ipc write: %w", err)
		return
	}
	if err = w.Close(); err != nil {
		err = eh.Errorf("play: pin: ipc close: %w", err)
		return
	}
	err = cli.rawInsertBody(ctx, "INSERT INTO "+meta.DataTable+" FORMAT Arrow", buf)
	if err != nil {
		err = eh.Errorf("play: pin: data insert: %w", err)
		return
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		err = eh.Errorf("play: pin: metadata marshal: %w", err)
		return
	}
	err = cli.rawInsertBody(ctx,
		"INSERT INTO "+pinMetaTable+" (fingerprint, data_table, query_id, run_id, app, lane, query, num_rows, num_cols) FORMAT JSONEachRow",
		bytes.NewReader(metaJSON))
	if err != nil {
		err = eh.Errorf("play: pin: metadata insert: %w", err)
		return
	}
	return
}

// rawInsertBody POSTs an INSERT whose statement rides the URL query
// parameter and whose data rides the request body (the ClickHouse HTTP
// convention for FORMAT Arrow / JSONEachRow payloads).
func (inst *Client) rawInsertBody(ctx context.Context, insertSQL string, body io.Reader) (err error) {
	reqURL := inst.URL()
	sep := "?"
	if strings.Contains(reqURL, "?") {
		sep = "&"
	}
	reqURL += sep + "query=" + url.QueryEscape(insertSQL)
	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, "POST", reqURL, body)
	if err != nil {
		err = eh.Errorf("unable to build insert request: %w", err)
		return
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if inst.cfg.User != "" {
		req.Header.Set("X-ClickHouse-User", inst.cfg.User)
	}
	if inst.cfg.Password != "" {
		req.Header.Set("X-ClickHouse-Key", inst.cfg.Password)
	}
	resp, err := inst.http.Do(req)
	if err != nil {
		err = eh.Errorf("insert request failed: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		err = eb.Build().Int("statusCode", resp.StatusCode).Str("body", strings.TrimSpace(string(raw))).
			Errorf("insert http %d", resp.StatusCode)
		return
	}
	return
}

// --- browser -------------------------------------------------------------

// pinRow is one browser entry from the metadata table.
type pinRow struct {
	Fingerprint uint64
	DataTable   string
	PinnedAt    time.Time
	QueryId     string
	RunId       string
	Lane        string
	NumRows     uint64
	NumCols     uint64
	Query       string
}

// pinRowColumns is the browser SELECT's arity; compose and parse move
// together.
const pinRowColumns = 9

// composePinBrowserSql lists pins newest-first.
func composePinBrowserSql(limit int) string {
	if limit <= 0 || limit > pinBrowserLimit {
		limit = pinBrowserLimit
	}
	return fmt.Sprintf(`SELECT
  fingerprint, data_table, toUnixTimestamp(pinned_at), query_id, run_id, lane, num_rows, num_cols, query
FROM %s
ORDER BY pinned_at DESC
LIMIT %d
FORMAT TabSeparated`, pinMetaTable, limit)
}

// parsePinRows decodes the browser payload; arity drift errors loudly.
func parsePinRows(raw []byte) (rows []pinRow, err error) {
	rows = []pinRow{}
	if len(raw) == 0 {
		return
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != pinRowColumns {
			err = eh.Errorf("play: pin browser: expected %d columns, got %d (line=%q)", pinRowColumns, len(parts), line)
			return
		}
		var row pinRow
		u := func(i int) (v uint64) {
			if err != nil {
				return
			}
			v, perr := strconv.ParseUint(parts[i], 10, 64)
			if perr != nil {
				err = eh.Errorf("play: pin browser: column %d %q: %w", i, parts[i], perr)
			}
			return v
		}
		row.Fingerprint = u(0)
		tsSec := u(2)
		row.NumRows = u(6)
		row.NumCols = u(7)
		if err != nil {
			return
		}
		row.DataTable = queryrunfacts.UnescapeTabSeparated(parts[1])
		row.PinnedAt = time.Unix(int64(tsSec), 0).UTC()
		row.QueryId = queryrunfacts.UnescapeTabSeparated(parts[3])
		row.RunId = queryrunfacts.UnescapeTabSeparated(parts[4])
		row.Lane = queryrunfacts.UnescapeTabSeparated(parts[5])
		row.Query = queryrunfacts.UnescapeTabSeparated(parts[8])
		rows = append(rows, row)
	}
	return
}

// pinsBrowserDriver is the browser's async fetch — the runsHistoryDriver
// pattern (single-flight, fetched-once reveal, replaced-not-mutated
// rows).
type pinsBrowserDriver struct {
	fetch func(ctx context.Context) ([]pinRow, error)

	mu       sync.Mutex
	inFlight bool
	fetched  bool
	asOf     time.Time
	rows     []pinRow
	err      error

	// selected is the chosen pin's fingerprint (0 = none). Render-thread-only.
	selected uint64
}

func newPinsBrowserDriver(client *Client) (d *pinsBrowserDriver) {
	d = &pinsBrowserDriver{}
	if client != nil {
		d.fetch = func(ctx context.Context) (rows []pinRow, err error) {
			raw, err := client.rawTsvQuery(ctx, composePinBrowserSql(pinBrowserLimit))
			if err != nil {
				return
			}
			rows, err = parsePinRows(raw)
			return
		}
	}
	return
}

func (inst *pinsBrowserDriver) refresh() {
	if inst.fetch == nil {
		return
	}
	inst.mu.Lock()
	if inst.inFlight {
		inst.mu.Unlock()
		return
	}
	inst.inFlight = true
	fetch := inst.fetch
	inst.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runsHistoryTimeout)
		defer cancel()
		rows, err := fetch(ctx)
		inst.mu.Lock()
		inst.rows, inst.err = rows, err
		inst.fetched = true
		inst.asOf = time.Now()
		inst.inFlight = false
		inst.mu.Unlock()
	}()
}

func (inst *pinsBrowserDriver) maybeRefreshOnReveal() {
	if inst.fetch == nil {
		return
	}
	inst.mu.Lock()
	needed := !inst.fetched && !inst.inFlight
	inst.mu.Unlock()
	if needed {
		inst.refresh()
	}
}

func (inst *pinsBrowserDriver) snapshot() (rows []pinRow, err error, inFlight bool, fetched bool, asOf time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.rows, inst.err, inst.inFlight, inst.fetched, inst.asOf
}

// --- rendering -----------------------------------------------------------

// renderPinControl is the Table tab's pin affordance: one button acting
// on the record the tab currently shows, with the driver phase beside
// it. err carries no per-result identity, so the driver's own state is
// the whole story.
func (inst *PlayApp) renderPinControl(rec arrow.RecordBatch) {
	if inst.pins == nil || inst.pins.client == nil || rec == nil {
		return
	}
	ids := inst.ids
	for range c.IdScope(ids.PrepareStr("pin-control")) {
		for range c.Horizontal().KeepIter() {
			if c.Button(ids.PrepareStr("pin"), c.Atoms().Text("Pin result").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.pinActiveResult(rec)
			}
			state, fp, pinErr := inst.pins.status()
			switch state {
			case pinInFlight:
				diagWeak("pinning…")
			case pinDone:
				diagWeak(fmt.Sprintf("pinned → %s", pinDataTableName(fp)))
			case pinAlready:
				diagWeak(fmt.Sprintf("already pinned (%s)", pinDataTableName(fp)))
			case pinFailed:
				if pinErr != nil {
					diagWeak("pin failed: " + truncateRunes(firstErrorLine(pinErr), 90))
				}
			}
		}
	}
}

// pinActiveResult assembles provenance for the record the Table tab
// shows and hands it to the driver. The content fingerprint is the pin
// identity; query/lane provenance is best-effort — the `main` lane's
// when the table observes the sink, empty otherwise (the pin stays
// valid: it is content-addressed).
func (inst *PlayApp) pinActiveResult(rec arrow.RecordBatch) {
	fp := fingerprintRecord(rec)
	runId, appId := inst.client.stampIdentity()
	meta := pinMetaRow{
		Fingerprint: fp,
		DataTable:   pinDataTableName(fp),
		RunId:       runId,
		App:         appId,
		Lane:        "main",
		Query:       inst.graph.MainSQL(),
		NumRows:     uint64(rec.NumRows()),
		NumCols:     uint64(rec.NumCols()),
	}
	if inst.graph.mainLane != nil && inst.graph.mainLane.opts != nil {
		meta.QueryId = inst.graph.mainLane.opts.QueryID
	}
	if node := inst.resolvedTabNode("table"); node != "" && node != inst.currentSplit.Sink {
		// A bound/observed intermediate: the main lane's provenance would
		// be wrong — pin content-addressed with the lane named.
		meta.Lane = string(node)
		meta.Query = ""
		meta.QueryId = ""
	}
	inst.pins.pin(rec, meta)
	// A fresh pin invalidates the browser's fetched-once state.
	if inst.pinsBrowser != nil {
		inst.pinsBrowser.mu.Lock()
		inst.pinsBrowser.fetched = false
		inst.pinsBrowser.mu.Unlock()
	}
}

// renderPinnedResults is the History tab's pin browser section.
func (inst *PlayApp) renderPinnedResults() {
	d := inst.pinsBrowser
	if d == nil || d.fetch == nil {
		return
	}
	d.maybeRefreshOnReveal()
	ids := inst.ids
	for range c.IdScope(ids.PrepareStr("pinned-results")) {
		c.Separator().Send()
		rows, fetchErr, inFlight, fetched, asOf := d.snapshot()
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel("Pinned results") {
				rt.Strong()
			}
			if c.Button(ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).
				SendResp().HasPrimaryClicked() {
				d.refresh()
			}
		}
		switch {
		case !fetched && inFlight:
			diagWeak("Fetching pins…")
			return
		case fetchErr != nil:
			// A missing metadata table simply means nothing was pinned yet.
			if strings.Contains(fetchErr.Error(), "UNKNOWN_TABLE") {
				diagWeak("Nothing pinned yet — Pin result on the Table tab freezes the active rows into a queryable table.")
			} else {
				c.Label(firstErrorLine(fetchErr)).Wrap().Selectable(true).Send()
			}
			return
		case len(rows) == 0:
			diagWeak("Nothing pinned yet — Pin result on the Table tab freezes the active rows into a queryable table.")
			return
		}
		diagWeak(fmt.Sprintf("%d pins · fetched %s", len(rows), humanizeAgo(asOf)))
		for i := range rows {
			for range c.IdScope(ids.PrepareSeq(uint64(i))) {
				if c.Button(ids.PrepareStr("pin-row"),
					c.Atoms().Text(pinRowLabel(rows[i])).Keep()).
					Frame(false).
					Truncate().
					SendResp().HasPrimaryClicked() {
					if d.selected == rows[i].Fingerprint {
						d.selected = 0
					} else {
						d.selected = rows[i].Fingerprint
					}
				}
			}
		}
		if d.selected == 0 {
			return
		}
		for i := range rows {
			if rows[i].Fingerprint == d.selected {
				inst.renderPinDetail(rows[i])
				return
			}
		}
		d.selected = 0
	}
}

// pinRowLabel is one browser line.
func pinRowLabel(row pinRow) string {
	text := row.Query
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if text == "" {
		text = row.DataTable
	}
	label := fmt.Sprintf("%s  %d×%d  %s",
		row.PinnedAt.Local().Format("15:04:05"), row.NumRows, row.NumCols, text)
	return truncateRunes(label, historyLabelChar)
}

// renderPinDetail is the selected pin's metadata plus its two editor
// hand-offs: the frozen rows as a query, and the original query text.
func (inst *PlayApp) renderPinDetail(row pinRow) {
	ids := inst.ids
	c.Separator().Send()
	for rt := range c.RichTextLabel("Pin detail") {
		rt.Strong()
	}
	diagWeak(row.PinnedAt.UTC().Format("2006-01-02 15:04:05") + " UTC · " +
		fmt.Sprintf("%d rows × %d cols", row.NumRows, row.NumCols))
	diagWeak("table " + row.DataTable + " · fingerprint " + fmt.Sprintf("%016x", row.Fingerprint))
	if row.QueryId != "" || row.RunId != "" || row.Lane != "" {
		parts := make([]string, 0, 3)
		if row.Lane != "" {
			parts = append(parts, "lane "+row.Lane)
		}
		if row.QueryId != "" {
			parts = append(parts, "query_id "+row.QueryId)
		}
		if row.RunId != "" {
			parts = append(parts, "run "+row.RunId)
		}
		diagWeak(strings.Join(parts, " · "))
	}
	if row.Query != "" {
		for rt := range c.RichTextLabel(truncateRunes(row.Query, runsDetailTextRunes)) {
			rt.Monospace()
		}
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("open-pin"), c.Atoms().Text("Open pinned rows").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.ReplaceSql("SELECT * FROM " + row.DataTable)
		}
		if row.Query != "" {
			if c.Button(ids.PrepareStr("restore-pin-query"), c.Atoms().Text("Restore query").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.ReplaceSql(row.Query)
			}
		}
	}
}
