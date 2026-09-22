package persist

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The workingset and column-width halves of the state store (statestore.StoreI;
// ADR-0105 Update 2026-08-15, P5). Both kinds left `boxer.facts` for the
// state table this backend already serves persist state from, so they share
// its store, its mutex and its flush-per-operation durability: every
// mutating call is on disk when it returns, and a failed one never ships.
//
// Point reads go through the same cache view Get uses; list reads are the
// generated state view's ScanLive over the kind's key prefix, so the
// newest-row-per-key collapse and the tombstone test run in the store rather
// than in hand-written SQL here.

// orderOf is the Order a row is written at: the caller's timestamp when it
// has one — the facts stores ordered versions by it, and the resolver and
// the window host stamp theirs at capture — else the write time. Versions of
// one key are ordered by it, so a caller stamping a time later than a
// subsequent delete keeps its write live; both producers stamp "now".
func orderOf(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts.UTC()
}

func cloneBytes(b []byte) []byte {
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

// landLocked finishes a mutating call: a failed commit discards the frame's
// rows, a successful one flushes them. Either way the operation is on disk
// or never happened when this returns.
func (inst *StoreBackend) landLocked(commitErr error) (err error) {
	if commitErr != nil {
		inst.st.DiscardPending()
		return commitErr
	}
	return inst.flushLocked()
}

// WriteWorkingset records row as the newest version of (AppId, Name). The
// window that wrote it and its run ride on the row's Owner component; a
// row without a run id is stamped with this process's.
func (inst *StoreBackend) WriteWorkingset(row statestore.WorkingsetRow) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	id := persiststore.WorkingsetKey(string(row.AppId), row.Name)
	err = inst.landLocked(inst.st.Begin(id, orderOf(row.Ts)).
		AddOwner(inst.owner(id, row.AppId, row.RunId, row.TileKey)).
		// Cloned: the builder's write-through mirror holds this slice, so
		// the caller recycling its buffer would change what a later
		// LatestWorkingset answers from the cache.
		AddWorkingset(persiststore.Workingset{ID: id, Name: row.Name, Kind: row.Kind, Config: cloneBytes(row.Config), Reason: row.Reason}).
		Commit())
	if err != nil {
		err = eb.Build().Str("appId", string(row.AppId)).Str("name", row.Name).Errorf("persist: write workingset: %w", err)
	}
	return
}

// LatestWorkingset returns the live record for (appId, name), through the
// cache view. A key never written and a deleted key both answer
// found=false with no error.
func (inst *StoreBackend) LatestWorkingset(appId app.AppIdT, name string) (cfg []byte, kind string, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ent, found, err := inst.pc.GetFetch(context.Background(), persiststore.WorkingsetKey(string(appId), name))
	if err != nil {
		err = eb.Build().Str("appId", string(appId)).Str("name", name).Errorf("persist: latest workingset: %w", err)
		found = false
		return
	}
	if !found || ent.IsTombstone() {
		found = false
		return
	}
	if !ent.Workingset.Has {
		// A live row under a workingset key that carries no Workingset is
		// not a shape this backend writes; absent would hide it.
		err = eb.Build().Str("appId", string(appId)).Str("name", name).Errorf("persist: latest workingset: row carries no workingset component")
		found = false
		return
	}
	cfg = cloneBytes(ent.Workingset.Val.Config)
	kind = ent.Workingset.Val.Kind
	return
}

// ListWorkingsets returns the live record of every (AppId, Name) the table
// holds, ordered by statestore.SortWorkingsets.
func (inst *StoreBackend) ListWorkingsets() (rows []statestore.WorkingsetRow, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	rows = []statestore.WorkingsetRow{}
	for ent, serr := range inst.st.ScanLiveWorkingset(context.Background(), recordstore.ScanOpts{KeyPrefix: persiststore.WorkingsetKeyPrefix}) {
		if serr != nil {
			err = eb.Build().Errorf("persist: list workingsets: %w", serr)
			rows = nil
			return
		}
		if !ent.Owner.Has {
			err = eb.Build().Str("key", ent.ID).Errorf("persist: list workingsets: live row carries no owner component")
			rows = nil
			return
		}
		ow, w := ent.Owner.Val, ent.Workingset.Val
		rows = append(rows, statestore.WorkingsetRow{
			RunId:   ow.RunId,
			AppId:   app.AppIdT(ow.AppId),
			Name:    w.Name,
			Kind:    w.Kind,
			Config:  cloneBytes(w.Config),
			TileKey: ow.InstanceKey,
			Reason:  w.Reason,
			Ts:      ent.Ts,
		})
	}
	statestore.SortWorkingsets(rows)
	return
}

// DeleteWorkingset appends a tombstone for (appId, name); the record reads
// as absent until the next write, and its history stays in the trail.
func (inst *StoreBackend) DeleteWorkingset(appId app.AppIdT, name string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	err = inst.landLocked(inst.st.Delete(persiststore.WorkingsetKey(string(appId), name), time.Now().UTC()))
	if err != nil {
		err = eb.Build().Str("appId", string(appId)).Str("name", name).Errorf("persist: delete workingset: %w", err)
	}
	return
}

// WriteColumnWidth records row as the newest version of its override key.
// The capturing window rides on Owner.InstanceKey and the run is this
// process's.
func (inst *StoreBackend) WriteColumnWidth(row statestore.ColumnWidthRow) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	id := persiststore.ColumnWidthKey(string(row.AppId), row.Tier, row.Scope, row.ColumnKey)
	err = inst.landLocked(inst.st.Begin(id, orderOf(row.Ts)).
		AddOwner(inst.owner(id, row.AppId, "", row.InstanceKey)).
		AddColumnWidth(persiststore.ColumnWidth{
			ID: id, Tier: row.Tier, Scope: row.Scope, ColumnKey: row.ColumnKey,
			Points: row.Points, FontSize: row.FontSize,
		}).
		Commit())
	if err != nil {
		err = eb.Build().Str("appId", string(row.AppId)).Str("tier", row.Tier).Str("columnKey", row.ColumnKey).Errorf("persist: write column width: %w", err)
	}
	return
}

// ListColumnWidths returns the live override of every key belonging to
// appId — one key-range read over that app's column-width prefix, which
// by construction excludes every other app, including ones nested under
// appId's import path.
func (inst *StoreBackend) ListColumnWidths(appId app.AppIdT) (rows []statestore.ColumnWidthRow, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	rows = []statestore.ColumnWidthRow{}
	for ent, serr := range inst.st.ScanLiveColumnWidth(context.Background(), recordstore.ScanOpts{KeyPrefix: persiststore.ColumnWidthAppPrefix(string(appId))}) {
		if serr != nil {
			err = eb.Build().Str("appId", string(appId)).Errorf("persist: list column widths: %w", serr)
			rows = nil
			return
		}
		if !ent.Owner.Has {
			err = eb.Build().Str("key", ent.ID).Errorf("persist: list column widths: live row carries no owner component")
			rows = nil
			return
		}
		c := ent.ColumnWidth.Val
		rows = append(rows, statestore.ColumnWidthRow{
			AppId:       app.AppIdT(ent.Owner.Val.AppId),
			InstanceKey: ent.Owner.Val.InstanceKey,
			Tier:        c.Tier,
			Scope:       c.Scope,
			ColumnKey:   c.ColumnKey,
			Points:      c.Points,
			FontSize:    c.FontSize,
			Ts:          ent.Ts,
		})
	}
	statestore.SortColumnWidths(rows)
	return
}

// DeleteColumnWidth tombstones one override key. Clearing a key never
// written is not an error; the tombstone becomes the newest row of a key
// that had none.
func (inst *StoreBackend) DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	err = inst.landLocked(inst.st.Delete(persiststore.ColumnWidthKey(string(appId), tier, scope, columnKey), time.Now().UTC()))
	if err != nil {
		err = eb.Build().Str("appId", string(appId)).Str("tier", tier).Str("columnKey", columnKey).Errorf("persist: delete column width: %w", err)
	}
	return
}
