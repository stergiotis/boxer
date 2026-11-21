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

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingview"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
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
	guard    ladingview.Guard
	exec     recordstore.ExecutorI
	stores   lading.Stores
	policies *ladingpolicy.PolicyStore
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
	sc = &storeConn{exec: exec, policies: ladingpolicy.NewPolicyStore(exec, nil, ladingpolicy.PolicyStoreConfig{}),
		stores: lading.Stores{
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
	if inst.policies != nil {
		inst.policies.Close()
	}
}

// maxListed bounds what a listing of the store returns.
const maxListed = 400

// stored is one markdown file of a mount's newest snapshot: a notebook, if
// it holds a record.
type stored struct {
	ref snapshotRef
	// mount is the mount's declared name, or its id in hex.
	mount string
}

// listing is the store's markdown files, newest snapshot of each mount.
type listing struct {
	files []stored
	// truncated says the store holds more than maxListed.
	truncated bool
}

// list reads the newest snapshot of every mount and names the markdown
// files at its root and one folder down: where a collected notebook is, and
// where a folder taken into the store usually keeps its reports. It reads
// directory listings, no file.
//
// m is the mounts and d the directories at each one's root: O(m·d) listing
// reads, bounded by maxListed files.
func (inst *storeConn) list(ctx context.Context) (out *listing, err error) {
	inst.guard.Lock()
	ids, err := ladingadapter.Mounts(ctx, inst.exec)
	names := make(map[identifier.TaggedId]string, len(ids))
	if err == nil {
		for ent, sErr := range inst.policies.ScanLadingMount(ctx, recordstore.ScanOpts{}) {
			if sErr != nil {
				// An unreadable policy table does not hide the mounts; they
				// show by id.
				break
			}
			if ent == nil || !ent.LadingMount.Has {
				continue
			}
			// The policy log is append-only and scanned in write order: the
			// last record of a mount is its declaration.
			names[identifier.TaggedId(ent.LadingMount.Val.Id)] = ent.LadingMount.Val.Name
		}
	}
	inst.guard.Unlock()
	if err != nil {
		return nil, eh.Errorf("list the mounts: %w", err)
	}
	out = &listing{}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		inst.guard.Lock()
		snaps, sErr := ladingadapter.Snapshots(ctx, inst.exec, id)
		var v *ladingadapter.FS
		if sErr == nil && len(snaps) > 0 {
			v, sErr = ladingadapter.Open(inst.stores, id, snaps[0].Snap)
		}
		inst.guard.Unlock()
		if sErr != nil || v == nil {
			// A mount that cannot be read is left out, not a reason to show
			// none.
			continue
		}
		label := names[id]
		if label == "" {
			label = strconv.FormatUint(id.Value(), 16)
		}
		fsys := ladingview.NewLocked(&inst.guard, v)
		for _, pattern := range []string{"*.md", "*/*.md"} {
			matches, _ := fs.Glob(fsys, pattern)
			for _, p := range matches {
				if len(out.files) >= maxListed {
					out.truncated = true
					return out, nil
				}
				out.files = append(out.files, stored{ref: snapshotRef{mount: id, snap: snaps[0].Snap, path: p}, mount: label})
			}
		}
	}
	return out, nil
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
	return &document{name: path.Base(ref.path), origin: "snapshot " + ref.String(), text: h.Data, fsys: dir, ref: &ref}, nil
}
