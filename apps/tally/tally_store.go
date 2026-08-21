package tally

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingview"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// storeConn is the app's connection to one lading store: the executor, the
// generated stores, and the adapter views it has opened.
//
// A generated record store is single-goroutine (ADR-0100), and this app
// reads through them from the render thread (the browser's directory reads)
// and from lanes (previews, the mount list). Every path into the stores takes
// one mutex (ladingview.Locked), so the app is serial against the store the
// way the SFTP head is — the right trade for a surface ADR-0198 calls
// batch-shaped.
type storeConn struct {
	guard    ladingview.Guard
	client   *chclient.Client
	exec     recordstore.ExecutorI
	stores   lading.Stores
	policies *ladingpolicy.PolicyStore
	views    map[viewKey]*ladingadapter.FS
}

type viewKey struct {
	mount identifier.TaggedId
	snap  int64
}

// connect pings the env-configured server, verifies the store's shape and
// opens the generated stores. It is called off the render thread.
func connect(ctx context.Context) (sc *storeConn, err error) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	if err = client.Ping(ctx); err != nil {
		err = eh.Errorf("ClickHouse not reachable: %w", err)
		return
	}
	exec, err := storeexec.New(client, nil)
	if err != nil {
		err = eh.Errorf("executor: %w", err)
		return
	}
	if err = lading.Verify(ctx, exec); err != nil {
		err = eh.Errorf("lading store: %w", err)
		return
	}
	sc = &storeConn{
		client:   client,
		exec:     exec,
		policies: ladingpolicy.NewPolicyStore(exec, nil, ladingpolicy.PolicyStoreConfig{}),
		views:    make(map[viewKey]*ladingadapter.FS, 8),
	}
	sc.stores = lading.Stores{
		Meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}
	return
}

func (sc *storeConn) close() {
	sc.guard.Lock()
	defer sc.guard.Unlock()
	if sc.stores.Meta != nil {
		sc.stores.Meta.Close()
	}
	if sc.stores.Data != nil {
		sc.stores.Data.Close()
	}
	if sc.policies != nil {
		sc.policies.Close()
	}
}

// view returns the adapter over one snapshot, opened once and kept: a pinned
// snapshot cannot change, so neither can the view.
func (sc *storeConn) view(mount identifier.TaggedId, snap time.Time) (fsys fs.FS, err error) {
	sc.guard.Lock()
	defer sc.guard.Unlock()
	k := viewKey{mount: mount, snap: snap.UnixNano()}
	if v, ok := sc.views[k]; ok {
		return ladingview.NewLocked(&sc.guard, v), nil
	}
	v, err := ladingadapter.Open(sc.stores, mount, snap)
	if err != nil {
		return
	}
	sc.views[k] = v
	return ladingview.NewLocked(&sc.guard, v), nil
}

// mountRow is what the Mounts pane shows per mount.
type mountRow struct {
	id        identifier.TaggedId
	name      string
	store     string
	snapshots []ladingadapter.Snapshot // newest first
}

// label is the name when a policy declared one, else the hex id.
func (m mountRow) label() string {
	if m.name != "" {
		return m.name
	}
	return hexID(m.id)
}

func hexID(id identifier.TaggedId) string {
	return fmt.Sprintf("%016x", id.Value())
}

// latest is the newest complete snapshot, or the zero time.
func (m mountRow) latest() (s ladingadapter.Snapshot, ok bool) {
	if len(m.snapshots) == 0 {
		return
	}
	return m.snapshots[0], true
}

// listMounts reads the snapshot index and the policy records: every mount,
// its snapshots newest first, and its declared name where one exists.
func (sc *storeConn) listMounts(ctx context.Context) (rows []mountRow, err error) {
	sc.guard.Lock()
	defer sc.guard.Unlock()
	ids, err := ladingadapter.Mounts(ctx, sc.exec)
	if err != nil {
		return
	}
	names := make(map[identifier.TaggedId]mountRow, len(ids))
	for ent, serr := range sc.policies.ScanLadingMount(ctx, recordstore.ScanOpts{}) {
		if serr != nil {
			// A missing or unreadable policy table is not a reason to hide
			// the mounts; they show by id.
			break
		}
		if ent == nil || !ent.LadingMount.Has {
			continue
		}
		pm := ent.LadingMount.Val
		id := identifier.TaggedId(pm.Id)
		prev, had := names[id]
		if !had || !ent.Ts.Before(prev.snapshotsTs()) {
			names[id] = mountRow{id: id, name: pm.Name, store: pm.Store}
		}
	}
	rows = make([]mountRow, 0, len(ids))
	for _, id := range ids {
		row := mountRow{id: id}
		if n, ok := names[id]; ok {
			row.name, row.store = n.name, n.store
		}
		row.snapshots, err = ladingadapter.Snapshots(ctx, sc.exec, id)
		if err != nil {
			return
		}
		rows = append(rows, row)
	}
	return
}

// snapshotsTs is a placeholder for ordering policy records by time; the scan
// is in write order, so the last record per id wins and this returns the zero
// time to keep "not before" true.
func (m mountRow) snapshotsTs() time.Time { return time.Time{} }
