package statestore

import (
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Memory is the in-process StoreI a host uses when ClickHouse is down, so
// saves and restores still work within one process and are simply not
// durable — ADR-0148's documented degradation. It keeps each kind as an
// append-only trail with tombstones and reads it latest-wins, so it
// answers the same questions persist.StoreBackend does and a test can
// exercise either.
//
// Safe for concurrent use.
type Memory struct {
	mu          sync.RWMutex
	workingsets []memoryEntry[WorkingsetRow]
	colWidths   []memoryEntry[ColumnWidthRow]
}

type memoryEntry[R any] struct {
	row       R
	tombstone bool
}

var _ StoreI = (*Memory)(nil)

// NewMemory returns an empty store.
func NewMemory() (inst *Memory) {
	inst = &Memory{}
	return
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

// WriteWorkingset appends row. The config bytes are copied so the
// composing app can recycle its buffer.
func (inst *Memory) WriteWorkingset(row WorkingsetRow) (err error) {
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	row.Config = cloneBytes(row.Config)
	inst.mu.Lock()
	inst.workingsets = append(inst.workingsets, memoryEntry[WorkingsetRow]{row: row})
	inst.mu.Unlock()
	return
}

// LatestWorkingset scans the trail in reverse so the most recent write for
// (appId, name) wins; a tombstone reached first reads back as not-found.
func (inst *Memory) LatestWorkingset(appId app.AppIdT, name string) (cfg []byte, kind string, found bool, err error) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	for i := len(inst.workingsets) - 1; i >= 0; i-- {
		e := inst.workingsets[i]
		if e.row.AppId != appId || e.row.Name != name {
			continue
		}
		if e.tombstone {
			return
		}
		cfg = cloneBytes(e.row.Config)
		if cfg == nil {
			cfg = []byte{}
		}
		kind = e.row.Kind
		found = true
		return
	}
	return
}

// ListWorkingsets walks the trail once in reverse, so the first entry seen
// for a key is its newest, and reports the winners. A key whose newest
// entry is a tombstone is skipped but still consumed, which is what keeps
// a deleted record from being resurrected by the write that preceded its
// tombstone.
func (inst *Memory) ListWorkingsets() (rows []WorkingsetRow, err error) {
	type wsKey struct {
		appId app.AppIdT
		name  string
	}
	inst.mu.RLock()
	seen := make(map[wsKey]struct{}, len(inst.workingsets))
	rows = make([]WorkingsetRow, 0, len(inst.workingsets))
	for i := len(inst.workingsets) - 1; i >= 0; i-- {
		e := inst.workingsets[i]
		k := wsKey{appId: e.row.AppId, name: e.row.Name}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if e.tombstone {
			continue
		}
		row := e.row
		row.Config = cloneBytes(e.row.Config)
		rows = append(rows, row)
	}
	inst.mu.RUnlock()
	SortWorkingsets(rows)
	return
}

// DeleteWorkingset appends a tombstone for (appId, name), so history stays
// in the trail.
func (inst *Memory) DeleteWorkingset(appId app.AppIdT, name string) (err error) {
	inst.mu.Lock()
	inst.workingsets = append(inst.workingsets, memoryEntry[WorkingsetRow]{
		row:       WorkingsetRow{AppId: appId, Name: name, Ts: time.Now().UTC()},
		tombstone: true,
	})
	inst.mu.Unlock()
	return
}

// Workingsets returns every written workingset row in insertion order,
// tombstones excluded — the whole trail, for a test asserting what was
// written rather than what is live.
func (inst *Memory) Workingsets() (rows []WorkingsetRow) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	rows = make([]WorkingsetRow, 0, len(inst.workingsets))
	for _, e := range inst.workingsets {
		if e.tombstone {
			continue
		}
		rows = append(rows, e.row)
	}
	return
}

// WriteColumnWidth appends one override to the trail.
func (inst *Memory) WriteColumnWidth(row ColumnWidthRow) (err error) {
	if row.Ts.IsZero() {
		row.Ts = time.Now().UTC()
	}
	inst.mu.Lock()
	inst.colWidths = append(inst.colWidths, memoryEntry[ColumnWidthRow]{row: row})
	inst.mu.Unlock()
	return
}

// ListColumnWidths collapses the trail to the latest entry per key for
// appId. The scan runs in reverse and keeps the first sighting of each
// key, so a tombstone reached first suppresses the key entirely rather
// than letting an older surviving write show through.
func (inst *Memory) ListColumnWidths(appId app.AppIdT) (rows []ColumnWidthRow, err error) {
	rows = []ColumnWidthRow{}
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	seen := make(map[ColumnWidthKey]struct{}, len(inst.colWidths))
	for i := len(inst.colWidths) - 1; i >= 0; i-- {
		e := inst.colWidths[i]
		if e.row.AppId != appId {
			continue
		}
		k := e.row.Key()
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if e.tombstone {
			continue
		}
		rows = append(rows, e.row)
	}
	SortColumnWidths(rows)
	return
}

// DeleteColumnWidth appends a tombstone for one override key.
func (inst *Memory) DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error) {
	inst.mu.Lock()
	inst.colWidths = append(inst.colWidths, memoryEntry[ColumnWidthRow]{
		row: ColumnWidthRow{
			AppId: appId, Tier: tier, Scope: scope, ColumnKey: columnKey,
			Ts: time.Now().UTC(),
		},
		tombstone: true,
	})
	inst.mu.Unlock()
	return
}
