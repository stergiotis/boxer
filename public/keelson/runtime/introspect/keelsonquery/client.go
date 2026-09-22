package keelsonquery

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
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

// Query runs sql over table and returns the body in format (empty for the
// engine's default). sql carries no FORMAT clause. A refusal is a
// *RefusedError; any other err is a transport or codec failure.
func (inst *Client) Query(ctx context.Context, table string, sql string, format string) (res Result, err error) {
	if inst == nil || inst.bus == nil {
		return res, eh.Errorf("keelson.query: client without a bus")
	}
	if err = ctx.Err(); err != nil {
		return res, eh.Errorf("keelson.query: before request: %w", err)
	}
	req := keelsonqueryrequest.KeelsonQueryRequest{At: time.Now().UTC(), Table: table, Sql: sql, Format: format}
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
	r, err := buscodec.Decode[keelsonqueryreply.KeelsonQueryReply](raw)
	if err != nil {
		return res, eh.Errorf("decode reply: %w", err)
	}
	if !r.Ok {
		return res, &RefusedError{Table: table, Reason: r.Reason}
	}
	res = Result{ContentType: r.ContentType, Body: r.Body}
	return
}

// FormatJSONEachRow is the FORMAT [Rows] asks for: one JSON object per
// line, the text format a small reader decodes without an Arrow allocator.
const FormatJSONEachRow = "JSONEachRow"

// Rows runs sql over table as JSONEachRow and decodes every row into a T
// through its `json` tags. It is the whole of what a window with a fixed
// statement needs.
func Rows[T any](ctx context.Context, cli *Client, table string, sql string) (rows []T, err error) {
	res, err := cli.Query(ctx, table, sql, FormatJSONEachRow)
	if err != nil {
		return nil, err
	}
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
