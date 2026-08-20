package ladingingest

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"lukechampine.com/blake3"
)

// Result is what one snapshot turned out to be. Entries and Bytes are also
// what the root row records, so a caller can compare its own count against the
// commit record without a query.
type Result struct {
	// Snap identifies the snapshot: it is the row key's `ts`, and the name a
	// reader pins to.
	Snap time.Time
	// ExpiresAt is on every row of the snapshot and is what the TTL drops.
	ExpiresAt time.Time
	// Entries counts every node written, the root included.
	Entries uint64
	// Bytes is the sum of the entries' sizes, whether their content was
	// stored, referenced or skipped.
	Bytes uint64
	// Blocks counts the block rows written.
	Blocks uint64
	// Stored, Referenced and Skipped partition the entries by content mode.
	Stored, Referenced, Skipped uint64
	// Errors counts nodes whose walk failed. They are rows like any other,
	// carrying the error — the walk does not stop for them (ADR-0198 §SD6).
	Errors uint64
}

// content modes, as they are stored. Values, not identifiers, because a query
// filters on them.
const (
	contentNone   = "none"
	contentBlocks = "blocks"
	contentRef    = "ref"
)

// node kinds, as they are stored.
const (
	kindFile    = "file"
	kindDir     = "dir"
	kindSymlink = "symlink"
	kindOther   = "other"
)

// flushEvery bounds how many rows a walk holds before shipping them. It is not
// a correctness knob — the commit rule makes any prefix of a walk invisible —
// only a memory one.
const flushEvery = 4096

// Snapshot walks fsys once and writes it as one snapshot of mount.
//
// st is [lading.Stores] — the entry table and the block table. A nil Data is
// legal only for a metadata-only policy: a store that cannot write blocks must
// not be handed a policy that produces them.
//
// # The commit rule is the whole protocol
//
// Every row carries the mount's id, the snapshot's instant and the node's
// path, so a snapshot is a key range and needs no bookkeeping to describe.
// What makes it *complete* is one row: the root, `naturalKey = '.'`, carrying
// the totals and the policy applied. Snapshot writes it last, in its own
// flush, after every other row of the walk is durable — so a walk that dies
// part way through leaves rows no query can reach, which `TTL` then removes
// with nothing to clean up. There is no rollback here because there is nothing
// to roll back.
//
// # What a walk error does
//
// Nothing stops. A node whose Lstat, Open or Read fails becomes an ordinary
// entry row carrying the error text and `content = 'none'`, and the walk
// continues past it. A tree with one unreadable directory is still a snapshot,
// and the failure is a row someone can query for rather than a log line
// someone has to have kept.
//
// The context is honoured between nodes: a cancelled walk stops and returns
// its error, having written no root row.
func Snapshot(ctx context.Context, fsys fs.FS, mount identifier.TaggedId, policy Policy, st lading.Stores) (res Result, err error) {
	err = policy.check()
	if err != nil {
		return
	}
	if st.Meta == nil {
		err = eh.Errorf("ladingingest: no meta store")
		return
	}
	if st.Data == nil && !policy.MetaOnly {
		err = eh.Errorf("ladingingest: policy stores content but no block store was given")
		return
	}
	if !mount.IsValid() {
		err = eb.Build().Uint64("mount", mount.Value()).
			Errorf("ladingingest: mount id is not a valid tagged id — the application mints it under its own tag (ADR-0198 §SD3)")
		return
	}

	w := &walk{
		ctx: ctx, fsys: fsys, policy: policy, st: st,
		mount: mount.Value(),
	}
	w.res.Snap = time.Now().UTC()
	w.res.ExpiresAt = policy.Ttl.expiryOf(w.res.Snap)

	err = fs.WalkDir(fsys, ".", w.visit)
	if err != nil {
		return w.res, err
	}
	// Everything but the commit record, durable first.
	err = w.flush()
	if err != nil {
		return w.res, err
	}
	err = w.commit()
	return w.res, err
}

// walk is one Snapshot call's state.
type walk struct {
	ctx    context.Context
	fsys   fs.FS
	policy Policy
	st     lading.Stores
	mount  uint64
	res    Result

	// root is the root node's own stat, held back so the commit record can
	// ride the row that carries it.
	root     fs.FileInfo
	rootErr  string
	rootSeen bool
}

// visit is fs.WalkDir's callback. It never returns an error except to abort on
// a context cancellation or a store failure: a failure to read one node is
// data, not a reason to stop.
func (inst *walk) visit(path string, d fs.DirEntry, walkErr error) error {
	if err := inst.ctx.Err(); err != nil {
		return err
	}
	if path == "." {
		inst.rootSeen = true
		if walkErr != nil {
			inst.rootErr = walkErr.Error()
			inst.res.Errors++
			return nil
		}
		info, err := inst.lstat(path, d)
		if err != nil {
			inst.rootErr = err.Error()
			inst.res.Errors++
			return nil
		}
		inst.root = info
		return nil
	}
	if walkErr != nil {
		// A ReadDir failure arrives after the directory's own row; record it
		// against the directory and skip its contents.
		inst.res.Errors++
		return inst.writeEntry(path, ladingmeta.LadingEntry{
			Kind: "entry", NodeKind: kindOther, Content: contentNone,
			Err: walkErr.Error(),
		})
	}
	return inst.node(path, d)
}

// node writes one non-root node.
func (inst *walk) node(path string, d fs.DirEntry) error {
	info, err := inst.lstat(path, d)
	if err != nil {
		inst.res.Errors++
		return inst.writeEntry(path, ladingmeta.LadingEntry{
			Kind: "entry", NodeKind: kindOther, Content: contentNone,
			Err: err.Error(),
		})
	}
	row := ladingmeta.LadingEntry{
		Kind:     "entry",
		NodeKind: nodeKindOf(info.Mode()),
		Content:  contentNone,
		Mode:     uint32(info.Mode()),
		Size:     uint64(max(info.Size(), 0)),
		Mtime:    info.ModTime().UTC(),
	}
	inst.res.Bytes += row.Size

	switch {
	case row.NodeKind == kindSymlink:
		target, lerr := inst.readLink(path)
		if lerr != nil {
			row.Err = lerr.Error()
			inst.res.Errors++
		}
		row.LinkTarget = target
		inst.res.Skipped++
	case row.NodeKind != kindFile || inst.policy.MetaOnly:
		inst.res.Skipped++
	default:
		err = inst.content(path, &row)
		if err != nil {
			return err
		}
	}
	return inst.writeEntry(path, row)
}

// content stores or references one regular file's bytes and fills the fields
// that describe them.
//
// The size the policy compares against is the one Lstat reported. A file that
// grows past InlineMax between the stat and the read is stored as it was read
// — the row's Size is restated from what arrived, so the row and the blocks
// agree even when the source did not hold still.
func (inst *walk) content(path string, row *ladingmeta.LadingEntry) (err error) {
	if row.Size > inst.policy.InlineMax {
		// Referenced, not stored — but still hashed, so `identical content`
		// and change detection work across the whole mount rather than only
		// its small half. Streamed: a ref file is not held.
		sum, herr := inst.hashOnly(path)
		if herr != nil {
			row.Err = herr.Error()
			row.Content = contentNone
			inst.res.Errors++
			inst.res.Skipped++
			return nil
		}
		row.Content = contentRef
		row.ContentHash = sum
		inst.res.Referenced++
		return nil
	}

	data, rerr := fs.ReadFile(inst.fsys, path)
	if rerr != nil {
		row.Err = rerr.Error()
		row.Content = contentNone
		inst.res.Errors++
		inst.res.Skipped++
		return nil
	}
	sum := blake3.Sum256(data)
	row.Content = contentBlocks
	row.ContentHash = sum[:]
	row.Size = uint64(len(data))
	row.BlockSize = inst.policy.Profile.BlockSize

	blocks, text := cut(data, inst.policy.Profile.BlockSize, inst.policy.Text)
	row.Text = text
	row.Blocks = uint32(len(blocks))
	inst.res.Stored++

	for seq, b := range blocks {
		br := ladingdata.LadingBlock{Kind: "block", Data: b.data, Line0: b.line0}
		if inst.policy.Profile.PerBlockHash {
			// A standalone digest, not a subtree chaining value: the audit
			// this column exists for is `BLAKE3(data) != hash` in SQL, and
			// only a standalone digest satisfies it (ADR-0198 §SD5 as
			// corrected 2026-08-20).
			h := blake3.Sum256(b.data)
			br.Hash = h[:]
		}
		err = inst.st.Data.Begin(inst.mount, inst.res.Snap, ladingdata.DataEnvelope{
			NaturalKey: blockKey(path, uint32(seq)),
			ExpiresAt:  inst.res.ExpiresAt,
		}).AddLadingBlock(br).Commit()
		if err != nil {
			return eb.Build().Str("path", path).Int("seq", seq).
				Errorf("ladingingest: buffer block: %w", err)
		}
		inst.res.Blocks++
	}
	return inst.maybeFlush()
}

// hashOnly streams a file through BLAKE3 without holding it.
func (inst *walk) hashOnly(path string) (sum []byte, err error) {
	f, err := inst.fsys.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	h := blake3.New(32, nil)
	_, err = io.Copy(h, f)
	if err != nil {
		return
	}
	return h.Sum(nil), nil
}

// writeEntry buffers one entry row.
func (inst *walk) writeEntry(path string, row ladingmeta.LadingEntry) (err error) {
	err = inst.st.Meta.Begin(inst.mount, inst.res.Snap, ladingmeta.MetaEnvelope{
		NaturalKey: []byte(path),
		ExpiresAt:  inst.res.ExpiresAt,
	}).AddLadingEntry(row).Commit()
	if err != nil {
		return eb.Build().Str("path", path).Errorf("ladingingest: buffer entry: %w", err)
	}
	inst.res.Entries++
	return inst.maybeFlush()
}

// maybeFlush ships what is buffered once either store has enough. Both are
// flushed together so a block never outlives the walk's memory of its entry.
func (inst *walk) maybeFlush() (err error) {
	if inst.st.Meta.Buffered() < flushEvery &&
		(inst.st.Data == nil || inst.st.Data.Buffered() < flushEvery) {
		return
	}
	return inst.flush()
}

// flush ships both stores. Blocks first: an entry claiming N blocks is worth
// more when its blocks are already there than the other way round, and the
// root row (which flush never carries) is what makes either visible.
func (inst *walk) flush() (err error) {
	if inst.st.Data != nil {
		_, err = inst.st.Data.Flush(inst.ctx)
		if err != nil {
			return eh.Errorf("ladingingest: flush blocks: %w", err)
		}
	}
	_, err = inst.st.Meta.Flush(inst.ctx)
	if err != nil {
		return eh.Errorf("ladingingest: flush entries: %w", err)
	}
	return
}

// commit writes the root row: the tree's own stat, plus the totals and the
// policy applied. It is one row, in one insert of its own, after everything
// else — which is what makes "complete" a fact about the data rather than a
// promise about the writer.
func (inst *walk) commit() (err error) {
	if !inst.rootSeen {
		return eh.Errorf("ladingingest: the walk never reached the root; no snapshot was written")
	}
	entry := ladingmeta.LadingEntry{
		Kind: "entry", NodeKind: kindDir, Content: contentNone, Err: inst.rootErr,
	}
	if inst.root != nil {
		entry.Mode = uint32(inst.root.Mode())
		entry.Mtime = inst.root.ModTime().UTC()
	}
	inst.res.Entries++
	err = inst.st.Meta.Begin(inst.mount, inst.res.Snap, ladingmeta.MetaEnvelope{
		NaturalKey: []byte("."),
		ExpiresAt:  inst.res.ExpiresAt,
	}).AddLadingEntry(entry).AddLadingSnapshot(ladingmeta.LadingSnapshot{
		Kind:      "snapshot",
		Entries:   inst.res.Entries,
		Bytes:     inst.res.Bytes,
		TtlClass:  inst.policy.Ttl.String(),
		TextRule:  inst.policy.Text.String(),
		InlineMax: inst.policy.InlineMax,
	}).Commit()
	if err != nil {
		return eh.Errorf("ladingingest: buffer root row: %w", err)
	}
	_, err = inst.st.Meta.Flush(inst.ctx)
	if err != nil {
		return eh.Errorf("ladingingest: flush root row: %w", err)
	}
	return
}

// lstat stats a node without following it. fs.DirEntry.Info is already
// link-aware, so it is the first choice; the ReadLinkFS path is for a walk
// entered without one.
func (inst *walk) lstat(path string, d fs.DirEntry) (fs.FileInfo, error) {
	if d != nil {
		return d.Info()
	}
	if rl, ok := inst.fsys.(fs.ReadLinkFS); ok {
		return rl.Lstat(path)
	}
	return fs.Stat(inst.fsys, path)
}

// readLink reads a symlink's target verbatim, unresolved. A source that has
// symlinks but no way to read them is a source whose links this store records
// as links with no target, which is what the error on the row says.
func (inst *walk) readLink(path string) (string, error) {
	rl, ok := inst.fsys.(fs.ReadLinkFS)
	if !ok {
		return "", errors.New("source does not implement fs.ReadLinkFS; symlink target unavailable")
	}
	return rl.ReadLink(path)
}

// nodeKindOf is the stored spelling of a mode's type bits.
func nodeKindOf(m fs.FileMode) string {
	switch {
	case m&fs.ModeSymlink != 0:
		return kindSymlink
	case m.IsDir():
		return kindDir
	case m.IsRegular():
		return kindFile
	}
	return kindOther
}

// blockKey is the block ordinal encoding (ADR-0198 SD11): `path ‖ 0x00 ‖
// be32(seq)`.
//
// 0x00 cannot occur in an io/fs name, so the split is unambiguous and a
// prefix of `path ‖ 0x00` selects exactly one file's blocks — "a.txt" cannot
// reach "a.txt.bak". Big-endian so the bytes sort in ordinal order, which is
// what makes a file's blocks one contiguous key range in ordinal order.
func blockKey(path string, seq uint32) []byte {
	k := make([]byte, 0, len(path)+5)
	k = append(k, path...)
	k = append(k, 0)
	return binary.BigEndian.AppendUint32(k, seq)
}
