package agentrunstore

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// BodyTable is the qualified vault table reference (ADR-0320 §SD1).
func (inst Layout) BodyTable() (name string) { return inst.DatabaseName() + "." + TableNameBody }

// BodyDDLTail is what the vault table's CREATE TABLE gains after the
// composed settings: a deletion that rewrites a part leaves the part it
// replaced on disk as an outdated part until the server's cleanup
// thread removes it, after old_parts_lifetime (480 s by default) and on
// the cleanup period, which starts at cleanup_delay_period (30 s plus a
// random share) and backs off to max_cleanup_delay_period (300 s) while
// it finds nothing to do. The vault pins all of them to a second, so the
// bytes an erasure removed from the table leave the disk within seconds
// of it, not minutes (ADR-0320 §SD4). The tail is appended verbatim, so
// it continues the SETTINGS clause the composed DDL ends with.
const BodyDDLTail = ", old_parts_lifetime = 1, cleanup_delay_period = 1, cleanup_delay_period_random_add = 0, max_cleanup_delay_period = 1"

// NewBodyStoreIn opens the vault store over the layout's table; the
// caller closes. It is single-goroutine, as every generated store is.
func NewBodyStoreIn(exec recordstore.ExecutorI, layout Layout) *BodyRowStore {
	return NewBodyRowStore(exec, nil, BodyRowStoreConfig{Table: layout.BodyTable(), DDLTail: BodyDDLTail})
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
