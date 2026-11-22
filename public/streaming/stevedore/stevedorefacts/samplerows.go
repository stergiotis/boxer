package stevedorefacts

import (
	"context"

	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// sampleKeyBase keeps this store's sample rows apart from the other
// stores' over the same table.
const sampleKeyBase = 7 << 32

// The store contributes rows of its own components to checks over the
// table it shares with other stores (recordstore.RegisterSampleRows): one
// entity per component, written through the store.
func init() {
	recordstore.RegisterSampleRows(StevedoreTableName, recordstore.SampleRowWriter{Name: "stevedorefacts", Write: func(ctx context.Context, exec recordstore.ExecutorI) (err error) {
		st := NewStevedoreStore(exec, nil, StevedoreStoreConfig{})
		defer st.Close()
		_, err = recordstore.IngestSamples(ctx, st, sampleKeyBase)
		return
	}})
}
