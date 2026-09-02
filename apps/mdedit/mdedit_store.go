package mdedit

// The lading connection (F: browse snapshots). A trimmed copy of tally's
// storeConn and lane (apps/tally/tally_store.go, tally_lane.go) rather than a
// shared package — both are deliberately app-local there (ADR-0200 kept the
// widget generic and the store plumbing per-host), and the trim is real:
// mdedit reads mounts and files, never audits, sizes or problem reports.

import (
	"context"
	"fmt"
	"io/fs"
	"sync"
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

// storeConn is the app's connection to the lading store: the executor, the
// generated stores, and the adapter views it has opened.
//
// A generated record store is single-goroutine (ADR-0100); every path into
// these takes one mutex (ladingview.Locked / the guard), so the app is serial
// against the store — the right trade for a surface ADR-0198 calls
// batch-shaped. The send-to-play pipeline (mdedit_sendplay.go) deliberately
// does NOT share this connection: its store would ride the same executor from
// another goroutine, outside this guard.
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

// connectLading pings the env-configured server, verifies the store's shape
// and opens the generated stores. Called off the render thread (a lane).
func connectLading(ctx context.Context) (sc *storeConn, err error) {
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
		views:    make(map[viewKey]*ladingadapter.FS, 4),
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

// mountRow is one mount with its snapshots, newest first.
type mountRow struct {
	id        identifier.TaggedId
	name      string
	snapshots []ladingadapter.Snapshot
}

// label is the name when a policy declared one, else the hex id.
func (m mountRow) label() string {
	if m.name != "" {
		return m.name
	}
	return fmt.Sprintf("%016x", m.id.Value())
}

// latest is the newest snapshot, or ok=false for a mount that has none.
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
	names := make(map[identifier.TaggedId]string, len(ids))
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
		// The policy log is append-only and the scan runs in write order, so
		// the last record per mount is its current declaration.
		names[identifier.TaggedId(pm.Id)] = pm.Name
	}
	rows = make([]mountRow, 0, len(ids))
	for _, id := range ids {
		row := mountRow{id: id, name: names[id]}
		row.snapshots, err = ladingadapter.Snapshots(ctx, sc.exec, id)
		if err != nil {
			return
		}
		rows = append(rows, row)
	}
	return
}

// lane runs one background computation at a time, keyed by what it is for,
// and hands the render thread the result when it is there — tally's lane,
// copied whole (its header records why a value with a disposer is
// lane-owned).
type lane[T any] struct {
	mu      sync.Mutex
	key     string
	gen     uint64
	running bool
	done    bool
	val     T
	err     error
	cancel  context.CancelFunc
	dispose func(T)
}

// demand returns the state for key, starting run when key is new. busy is
// true while the run is in flight; done and err are meaningful once it is
// not. A nil run is a question, not an order: it polls without starting.
func (l *lane[T]) demand(key string, run func(ctx context.Context) (T, error)) (val T, done bool, err error, busy bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key != l.key || (!l.done && !l.running) {
		if run == nil {
			var zero T
			return zero, false, nil, false
		}
		l.start(key, run)
	}
	return l.val, l.done, l.err, l.running
}

// invalidate forgets the current key so the next demand re-runs it.
func (l *lane[T]) invalidate() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.key, l.running = "", false
	l.clearLocked()
}

func (l *lane[T]) clearLocked() {
	if l.done && l.dispose != nil {
		l.dispose(l.val)
	}
	l.done = false
	var zero T
	l.val, l.err = zero, nil
}

func (l *lane[T]) start(key string, run func(ctx context.Context) (T, error)) {
	if l.cancel != nil {
		l.cancel()
	}
	l.gen++
	gen := l.gen
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.clearLocked()
	l.key, l.running = key, true
	go func() {
		v, err := run(ctx)
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.gen != gen {
			// Superseded, and nobody will ever be handed this: a value that
			// owns something has to be released here or it leaks.
			if l.dispose != nil {
				l.dispose(v)
			}
			return
		}
		l.val, l.err, l.done, l.running = v, err, true, false
	}()
}

// close cancels whatever is in flight and releases what the lane holds.
func (l *lane[T]) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.key, l.running = "", false
	l.clearLocked()
}
