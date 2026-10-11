package agentrunstore

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// BodyTable is the qualified vault table reference (ADR-0320 §SD1).
func (inst Layout) BodyTable() (name string) { return inst.DatabaseName() + "." + TableNameBody }

// NewBodyStoreIn opens the vault store over the layout's table; the
// caller closes. It is single-goroutine, as every generated store is.
func NewBodyStoreIn(exec recordstore.ExecutorI, layout Layout) *BodyRowStore {
	return NewBodyRowStore(exec, nil, BodyRowStoreConfig{Table: layout.BodyTable()})
}

// ProvisionBodyIn creates the vault table; idempotent. It is separate from
// ProvisionIn so a host whose ledger tables another change is moving can
// provision the vault alone.
func ProvisionBodyIn(ctx context.Context, exec recordstore.ExecutorI, layout Layout) (err error) {
	st := NewBodyStoreIn(exec, layout)
	defer st.Close()
	if err = st.EnsureTable(ctx); err != nil {
		return eh.Errorf("ensure body table: %w", err)
	}
	return
}
