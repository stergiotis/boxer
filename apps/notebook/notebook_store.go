package notebook

// The lading connection: a trimmed copy of mdedit's (apps/mdedit/
// mdedit_store.go), which is a trimmed copy of tally's. The store plumbing
// is per host by ADR-0200; this window opens one snapshot and reads files
// of it, and nothing else.

import (
	"context"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingview"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// maxSnapshotDocBytes bounds a notebook's text read from a snapshot.
const maxSnapshotDocBytes = int64(4 << 20)

// snapshotRef names a notebook in the store.
type snapshotRef struct {
	mount identifier.TaggedId
	snap  time.Time
	path  string
}

// String is the ref as the seed variable spells it.
func (inst snapshotRef) String() string {
	return strconv.FormatUint(inst.mount.Value(), 16) + "@" + strconv.FormatInt(inst.snap.UnixNano(), 10) + ":" + inst.path
}

// parseSnapshotRef reads <mount, hex>@<snapshot, unix nanoseconds>:<path>.
func parseSnapshotRef(s string) (ref snapshotRef, err error) {
	bad := func(what string) (snapshotRef, error) {
		return snapshotRef{}, eb.Build().Str("ref", s).Errorf("a snapshot is named <mount, hex>@<snapshot, unix nanoseconds>:<path>: %s", what)
	}
	mount, rest, ok := strings.Cut(s, "@")
	if !ok {
		return bad("no @")
	}
	snap, p, ok := strings.Cut(rest, ":")
	if !ok || p == "" {
		return bad("no path")
	}
	m, pErr := strconv.ParseUint(mount, 16, 64)
	if pErr != nil {
		return bad("the mount is not a hex number")
	}
	n, pErr := strconv.ParseInt(snap, 10, 64)
	if pErr != nil {
		return bad("the snapshot is not a number of nanoseconds")
	}
	return snapshotRef{mount: identifier.TaggedId(m), snap: time.Unix(0, n), path: path.Clean(p)}, nil
}

// storeConn is the window's connection to the lading store. A generated
// record store is single-goroutine (ADR-0100); every read goes through the
// guard, by way of a Locked view.
type storeConn struct {
	guard  ladingview.Guard
	stores lading.Stores
}

// connectLading pings the server the environment names, verifies the
// store's shape and opens the generated stores. Off the render goroutine.
func connectLading(ctx context.Context) (sc *storeConn, err error) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	if err = client.Ping(ctx); err != nil {
		return nil, eh.Errorf("ClickHouse not reachable: %w", err)
	}
	exec, err := storeexec.New(client, nil)
	if err != nil {
		return nil, eh.Errorf("executor: %w", err)
	}
	if err = lading.Verify(ctx, exec); err != nil {
		return nil, eh.Errorf("lading store: %w", err)
	}
	sc = &storeConn{stores: lading.Stores{
		Meta: ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{}),
		Data: ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{}),
	}}
	return sc, nil
}

func (inst *storeConn) close() {
	inst.guard.Lock()
	defer inst.guard.Unlock()
	if inst.stores.Meta != nil {
		inst.stores.Meta.Close()
	}
	if inst.stores.Data != nil {
		inst.stores.Data.Close()
	}
}

// open reads the notebook ref names: its text, and the snapshot at the
// notebook's directory as the place its blobs are found.
func (inst *storeConn) open(ref snapshotRef) (doc *document, err error) {
	inst.guard.Lock()
	v, err := ladingadapter.Open(inst.stores, ref.mount, ref.snap)
	inst.guard.Unlock()
	if err != nil {
		return nil, eh.Errorf("open the snapshot: %w", err)
	}
	fsys := ladingview.NewLocked(&inst.guard, v)
	h, err := ladingview.ReadHead(fsys, ref.path, maxSnapshotDocBytes, 0)
	if err != nil {
		return nil, eh.Errorf("read the notebook: %w", err)
	}
	if h.IsDir {
		return nil, eb.Build().Str("path", ref.path).Errorf("the path names a directory")
	}
	if h.Truncated {
		return nil, eb.Build().Str("path", ref.path).Int64("size", h.Size).Errorf("the notebook's text is over the window's size cap")
	}
	dir, err := fs.Sub(fsys, path.Dir(ref.path))
	if err != nil {
		return nil, err
	}
	return &document{name: path.Base(ref.path), origin: "snapshot " + ref.String(), text: h.Data, fsys: dir}, nil
}
