package appstate

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The entries are an introspection table, not a verb (ADR-0185 §SD1): one
// SQL read over keelson('app_state') on the process's local query endpoint
// (ADR-0094 §SD6), as JSONEachRow — the text format a small reader decodes
// without an Arrow allocator. The watchbill window reads its trail the
// same way.

// endpointTimeout bounds one read of the local endpoint.
const endpointTimeout = 5 * time.Second

// entryRow is one row of keelson('app_state').
type entryRow struct {
	Kind         string `json:"kind"`
	AppId        string `json:"app_id"`
	Key          string `json:"key"`
	EntityId     string `json:"entity_id"`
	PayloadBytes int64  `json:"payload_bytes"`
	Detail       string `json:"detail"`
	WrittenAt    string `json:"written_at"`
	RunId        string `json:"run_id"`
	InstanceKey  uint64 `json:"instance_key"`
}

// entriesReaderI is what the poller reads through: the endpoint in the
// window, a fixture in tests.
type entriesReaderI interface {
	entries(ctx context.Context) (rows []entryRow, err error)
}

// endpointReader reads the table from the local endpoint.
type endpointReader struct {
	cli *chclient.Client
}

// newEndpointReader returns nil when url is empty — the process serves no
// local endpoint — so the window can say so rather than fail each read.
func newEndpointReader(url string) (inst *endpointReader) {
	if url == "" {
		return nil
	}
	inst = &endpointReader{cli: chclient.New(chclient.Config{URL: url}, &http.Client{Timeout: endpointTimeout})}
	return
}

const entriesSql = "SELECT kind, app_id, key, entity_id, payload_bytes, detail, written_at, run_id, instance_key " +
	"FROM keelson('app_state') ORDER BY app_id, kind, key FORMAT JSONEachRow"

func (inst *endpointReader) entries(ctx context.Context) (rows []entryRow, err error) {
	ctx, cancel := context.WithTimeout(ctx, endpointTimeout)
	defer cancel()
	body, err := inst.cli.Query(ctx, entriesSql)
	if err != nil {
		return nil, eh.Errorf("introspection read: %w", err)
	}
	defer func() { _ = body.Close() }()
	dec := json.NewDecoder(body)
	for dec.More() {
		var r entryRow
		if err = dec.Decode(&r); err != nil {
			return nil, eh.Errorf("introspection row: %w", err)
		}
		rows = append(rows, r)
	}
	return
}
