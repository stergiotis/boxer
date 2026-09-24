package appstate

import (
	"context"
	"iter"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
)

// The entries are an introspection table, not a verb (ADR-0185 §SD1): one
// read over keelson('app_state') through the `keelson.query.app_state`
// grant the manifest declares (ADR-0253), decoded from ArrowStream into
// columns (ADR-0257). The watchbill window reads its trail the same way.

// entryCols is keelson('app_state') as columns, one slice per column.
type entryCols struct {
	Kind         []string `ch:"kind"`
	AppId        []string `ch:"app_id"`
	Key          []string `ch:"key"`
	EntityId     []string `ch:"entity_id"`
	PayloadBytes []int64  `ch:"payload_bytes"`
	Detail       []string `ch:"detail"`
	WrittenAt    []string `ch:"written_at"`
	RunId        []string `ch:"run_id"`
	InstanceKey  []uint64 `ch:"instance_key"`
}

// entryRow is one entry, assembled from the columns where a row is what
// the caller holds on to — a Delete names the entry it clears.
type entryRow struct {
	Kind         string
	AppId        string
	Key          string
	EntityId     string
	PayloadBytes int64
	Detail       string
	WrittenAt    string
	RunId        string
	InstanceKey  uint64
}

func (inst *entryCols) Len() (n int) { return len(inst.Kind) }

// Row assembles entry i.
func (inst *entryCols) Row(i int) (r entryRow) {
	r = entryRow{Kind: inst.Kind[i], AppId: inst.AppId[i], Key: inst.Key[i], EntityId: inst.EntityId[i],
		PayloadBytes: inst.PayloadBytes[i], Detail: inst.Detail[i], WrittenAt: inst.WrittenAt[i], RunId: inst.RunId[i],
		InstanceKey: inst.InstanceKey[i]}
	return
}

// All assembles every entry in turn.
func (inst *entryCols) All() iter.Seq2[int, entryRow] {
	return func(yield func(int, entryRow) bool) {
		for i := range inst.Len() {
			if !yield(i, inst.Row(i)) {
				return
			}
		}
	}
}

// Add appends one entry to every column.
func (inst *entryCols) Add(r entryRow) {
	inst.Kind = append(inst.Kind, r.Kind)
	inst.AppId = append(inst.AppId, r.AppId)
	inst.Key = append(inst.Key, r.Key)
	inst.EntityId = append(inst.EntityId, r.EntityId)
	inst.PayloadBytes = append(inst.PayloadBytes, r.PayloadBytes)
	inst.Detail = append(inst.Detail, r.Detail)
	inst.WrittenAt = append(inst.WrittenAt, r.WrittenAt)
	inst.RunId = append(inst.RunId, r.RunId)
	inst.InstanceKey = append(inst.InstanceKey, r.InstanceKey)
}

// entriesReaderI is what the poller reads through: the table over the bus
// in the window, a fixture in tests.
type entriesReaderI interface {
	entries(ctx context.Context) (cols entryCols, err error)
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

func (inst *tableReader) entries(ctx context.Context) (cols entryCols, err error) {
	_, err = keelsonquery.Columns(ctx, inst.cli, providers.TableAppState, entriesSql, &cols)
	return
}
