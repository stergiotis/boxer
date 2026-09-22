package appstate

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

// The entries are an introspection table, not a verb (ADR-0185 §SD1): one
// read over keelson('app_state') through the `keelson.query.app_state`
// grant the manifest declares (ADR-0253), as JSONEachRow — the text format
// a small reader decodes without an Arrow allocator. The watchbill window
// reads its trail the same way.

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

// entriesReaderI is what the poller reads through: the table over the bus
// in the window, a fixture in tests.
type entriesReaderI interface {
	entries(ctx context.Context) (rows []entryRow, err error)
}

// tableReader reads the table over the bus.
type tableReader struct {
	cli *keelsonquery.Client
}

// newTableReader returns nil when bus is nil — the host minted no bus for
// the window — so the window can say so rather than fail each read.
func newTableReader(bus app.BusI) (inst *tableReader) {
	if bus == nil {
		return nil
	}
	inst = &tableReader{cli: keelsonquery.NewClient(bus)}
	return
}

const entriesSql = "SELECT kind, app_id, key, entity_id, payload_bytes, detail, written_at, run_id, instance_key " +
	"FROM keelson('" + providers.TableAppState + "') ORDER BY app_id, kind, key"

func (inst *tableReader) entries(ctx context.Context) (rows []entryRow, err error) {
	return keelsonquery.Rows[entryRow](ctx, inst.cli, providers.TableAppState, entriesSql)
}
