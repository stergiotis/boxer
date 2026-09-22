package persist

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// What the app-state manager's service clears through (ADR-0185 §SD3/§SD4):
// entries addressed by the state store's own key, whatever their kind. The
// per-kind verbs (Delete, DeleteWorkingset, DeleteColumnWidth) serve the
// kind's own owner; these serve a caller that sees every app's state, and
// so name an entry by the key keelson('app_state') shows.

// StateEntry is one live entry of any kind, as keelson('app_state') names it.
type StateEntry struct {
	Kind     string
	AppId    app.AppIdT
	Key      string
	EntityId string
}

func stateEntryOf(ent *persiststore.PersistEntity) (e StateEntry) {
	e = StateEntry{Kind: persiststore.KindOf(ent), Key: persiststore.EntryKeyOf(ent), EntityId: ent.ID}
	if ent.Owner.Has {
		e.AppId = app.AppIdT(ent.Owner.Val.AppId)
	}
	return
}

// LiveEntries returns every live entry appId owns, of every kind — one live
// scan of the Owner component every state row carries.
func (inst *StoreBackend) LiveEntries(appId app.AppIdT) (entries []StateEntry, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for ent, serr := range inst.st.ScanLiveOwner(context.Background(), recordstore.ScanOpts{}) {
		if serr != nil {
			err = eb.Build().Str("appId", string(appId)).Errorf("persist: live entries: %w", serr)
			entries = nil
			return
		}
		if ent.Owner.Val.AppId == string(appId) {
			entries = append(entries, stateEntryOf(ent))
		}
	}
	return
}

// EntryAt reads the live entry at entityId through the cache view. A key
// never written and a deleted key both answer found=false.
func (inst *StoreBackend) EntryAt(entityId string) (e StateEntry, found bool, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	ent, found, err := inst.pc.GetFetch(context.Background(), entityId)
	if err != nil {
		err = eb.Build().Str("entityId", entityId).Errorf("persist: entry at: %w", err)
		found = false
		return
	}
	if !found || ent.IsTombstone() {
		found = false
		return
	}
	e = stateEntryOf(ent)
	return
}

// DeleteEntity tombstones the entry at entityId. Like every mutating call
// here it is on disk, or never happened, when it returns; the attached cache
// view sees the tombstone at once, so the owning app's next read is absent.
func (inst *StoreBackend) DeleteEntity(entityId string) (err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	err = inst.landLocked(inst.st.Delete(entityId, time.Now().UTC()))
	if err != nil {
		err = eb.Build().Str("entityId", entityId).Errorf("persist: delete entity: %w", err)
	}
	return
}
