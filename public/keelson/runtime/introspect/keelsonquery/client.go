package keelsonquery

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryreply"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/keelsonqueryrequest"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Client is the read as an app sees it: one verb over a bus client holding
// [ClientCaps] for the tables it names, answered by the host's [Service].
// The zero value is unusable; take one from [NewClient] with the bus the
// host minted for the app (its MountContextI.Bus()).
type Client struct {
	bus app.BusI
	// Timeout bounds one request; zero is [DefaultTimeout].
	Timeout time.Duration
}

// NewClient wraps bus.
func NewClient(bus app.BusI) (inst *Client) {
	inst = &Client{bus: bus}
	return
}

// Result is a reply as a Go value: the body in the request's FORMAT and
// what the engine said it is.
type Result struct {
	ContentType string
	Body        []byte
}

// RefusedError is a reply with Ok false: the service declined the statement
// or the engine failed it. It is an error rather than a Result field because
// a reader has no row to show either way; the reason is for the window to
// say.
type RefusedError struct {
	Table  string
	Reason string
}

func (inst *RefusedError) Error() string {
	return "keelson.query " + inst.Table + ": " + inst.Reason
}

// Request is one read as a Go value.
type Request struct {
	// Table is the introspection table the statement may read; the
	// subject is derived from it.
	Table string
	// Sql is the statement, without a FORMAT clause.
	Sql string
	// Format is the ClickHouse FORMAT the body comes back in; empty for
	// the engine's default.
	Format string
	// Params binds `{name:Type}` placeholders by bare name (ADR-0133
	// §SD2); nil binds none.
	Params map[string]string
}

// Query runs sql over table and returns the body in format (empty for the
// engine's default). sql carries no FORMAT clause. A refusal is a
// *RefusedError; any other err is a transport or codec failure.
func (inst *Client) Query(ctx context.Context, table string, sql string, format string) (res Result, err error) {
	return inst.QueryWith(ctx, Request{Table: table, Sql: sql, Format: format})
}

// QueryWith is Query over a Request, for a statement that binds
// placeholders.
func (inst *Client) QueryWith(ctx context.Context, r Request) (res Result, err error) {
	table := r.Table
	if inst == nil || inst.bus == nil {
		return res, eh.Errorf("keelson.query: client without a bus")
	}
	if err = ctx.Err(); err != nil {
		return res, eh.Errorf("keelson.query: before request: %w", err)
	}
	req := keelsonqueryrequest.KeelsonQueryRequest{At: time.Now().UTC(), Table: table, Sql: r.Sql, Format: r.Format}
	if len(r.Params) > 0 {
		names := make([]string, 0, len(r.Params))
		for n := range r.Params {
			names = append(names, n)
		}
		sort.Strings(names)
		req.ParamName = names
		req.ParamValue = make([]string, len(names))
		for i, n := range names {
			req.ParamValue[i] = r.Params[n]
		}
	}
	payload, err := buscodec.Encode(req)
	if err != nil {
		return res, eh.Errorf("encode request: %w", err)
	}
	timeout := inst.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		if until := time.Until(deadline); until < timeout {
			timeout = until
		}
	}
	raw, err := inst.bus.RequestWithTimeout(Subject(table), payload, timeout)
	if err != nil {
		return res, eb.Build().Str("table", table).Errorf("keelson.query request: %w", err)
	}
	rep, err := buscodec.Decode[keelsonqueryreply.KeelsonQueryReply](raw)
	if err != nil {
		return res, eh.Errorf("decode reply: %w", err)
	}
	if !rep.Ok {
		return res, &RefusedError{Table: table, Reason: rep.Reason}
	}
	res = Result{ContentType: rep.ContentType, Body: rep.Body}
	return
}

// FormatJSONEachRow is the FORMAT [Rows] asks for: one JSON object per
// line, the text format a small reader decodes without an Arrow allocator.
const FormatJSONEachRow = "JSONEachRow"

// Rows runs sql over table as JSONEachRow and decodes every row into a T
// through its `json` tags. It is the whole of what a window with a fixed
// statement needs.
func Rows[T any](ctx context.Context, cli *Client, table string, sql string) (rows []T, err error) {
	return RowsWith[T](ctx, cli, Request{Table: table, Sql: sql})
}

// RowsWith is Rows over a Request, for a statement that binds
// placeholders; the request's Format is replaced by JSONEachRow.
func RowsWith[T any](ctx context.Context, cli *Client, r Request) (rows []T, err error) {
	r.Format = FormatJSONEachRow
	res, err := cli.QueryWith(ctx, r)
	if err != nil {
		return nil, err
	}
	table := r.Table
	dec := jsontext.NewDecoder(bytes.NewReader(res.Body))
	for {
		var r T
		if err = json.UnmarshalDecode(dec, &r); err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
				break
			}
			return nil, eb.Build().Str("table", table).Errorf("keelson.query row: %w", err)
		}
		rows = append(rows, r)
	}
	return
}
