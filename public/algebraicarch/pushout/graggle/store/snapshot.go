package store

import (
	"bytes"
	"encoding/binary"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// Snapshot codec: a deterministic binary serialization of the graggle's
// ESSENTIAL state — live nodes with contents, tombstones with deleter
// sets, retention bookkeeping (tombstoneAt, contentPurged), and all
// live/deleted edges. Derived state (the deleted partition, pseudo-edges
// and their reason maps, the dirty set) is deliberately NOT serialized:
// DecodeSnapshot rebuilds it from the essential state and resolves, so
// the format surface stays small, persisted drift cannot survive a
// round-trip, and the qc invariants validate the rebuilt result.
//
// Layout (version GRG1; all counts/lengths uvarint, node ids as 32-byte
// patch hash + uvarint index, every section sorted for determinism):
//
//	"GRG1"
//	liveCount    { id contentFlag [len bytes] }*
//	deletedCount { id contentFlag [len bytes] svarint(tombstoneAtUnixNano)
//	               purgedFlag deleterCount { hash32 }* }*
//	sourceCount  { id edgeCount { destID kindByte introducedBy32 }* }*
//
// Pseudo-edges are filtered out at encode (kind EdgeKindPseudo is
// derived); back-edges are mirrored from forward edges at decode. The
// clock is not part of the snapshot — callers re-inject via SetClock.
const snapshotMagic = "GRG1"

// EncodeSnapshot serializes the graggle. The output is deterministic:
// equal observable state yields equal bytes (maps are emitted in
// CompareNodeID / bytewise order).
func (inst *Graggle) EncodeSnapshot() (data []byte, err error) {
	var buf bytes.Buffer
	buf.WriteString(snapshotMagic)

	writeUvarint := func(v uint64) {
		buf.Write(binary.AppendUvarint(nil, v))
	}
	writeNodeID := func(id t.NodeID) {
		buf.Write(id.Patch[:])
		writeUvarint(id.Index)
	}
	writeContent := func(id t.NodeID) {
		content, present := inst.contents[id]
		if !present {
			buf.WriteByte(0)
			return
		}
		buf.WriteByte(1)
		writeUvarint(uint64(len(content)))
		buf.Write(content)
	}

	live := inst.nodes.Items()
	writeUvarint(uint64(len(live)))
	for _, id := range live {
		writeNodeID(id)
		writeContent(id)
	}

	deleted := inst.deletedNodes.Items()
	writeUvarint(uint64(len(deleted)))
	for _, id := range deleted {
		writeNodeID(id)
		writeContent(id)
		buf.Write(binary.AppendVarint(nil, inst.tombstoneAt[id].UnixNano()))
		if _, purged := inst.contentPurged[id]; purged {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
		deleters := make([]t.PatchHash, 0, len(inst.deleters[id]))
		for h := range inst.deleters[id] {
			deleters = append(deleters, h)
		}
		slices.SortFunc(deleters, func(a, b t.PatchHash) int { return bytes.Compare(a[:], b[:]) })
		writeUvarint(uint64(len(deleters)))
		for _, h := range deleters {
			buf.Write(h[:])
		}
	}

	type edgeRec struct {
		dest t.NodeID
		kind t.EdgeKindE
		by   t.PatchHash
	}
	sources := inst.edges.Sources()
	// Sources with only pseudo-edges are omitted entirely.
	persisted := make(map[string][]edgeRec, len(sources))
	var srcOrder []t.NodeID
	for _, src := range sources {
		var recs []edgeRec
		for _, e := range inst.edges.Get(src) {
			if e.Kind == t.EdgeKindPseudo {
				continue
			}
			recs = append(recs, edgeRec{dest: e.Dest, kind: e.Kind, by: e.IntroducedBy})
		}
		if len(recs) == 0 {
			continue
		}
		slices.SortFunc(recs, func(a, b edgeRec) int {
			if c := t.CompareNodeID(a.dest, b.dest); c != 0 {
				return c
			}
			if a.kind != b.kind {
				return int(a.kind) - int(b.kind)
			}
			return bytes.Compare(a.by[:], b.by[:])
		})
		persisted[src.String()] = recs
		srcOrder = append(srcOrder, src)
	}
	writeUvarint(uint64(len(srcOrder)))
	for _, src := range srcOrder {
		writeNodeID(src)
		recs := persisted[src.String()]
		writeUvarint(uint64(len(recs)))
		for _, r := range recs {
			writeNodeID(r.dest)
			buf.WriteByte(byte(r.kind))
			buf.Write(r.by[:])
		}
	}

	data = buf.Bytes()
	return
}

// snapReader is a bounds-checked cursor over snapshot bytes.
type snapReader struct {
	data []byte
	pos  int
}

func (r *snapReader) take(n int) (out []byte, err error) {
	if n < 0 || r.pos+n > len(r.data) {
		err = eh.Errorf("truncated at offset %d (need %d bytes): %w", r.pos, n, ErrBadSnapshot)
		return
	}
	out = r.data[r.pos : r.pos+n]
	r.pos += n
	return
}

func (r *snapReader) uvarint() (v uint64, err error) {
	v, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		err = eh.Errorf("bad uvarint at offset %d: %w", r.pos, ErrBadSnapshot)
		return
	}
	r.pos += n
	return
}

func (r *snapReader) varint() (v int64, err error) {
	v, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		err = eh.Errorf("bad varint at offset %d: %w", r.pos, ErrBadSnapshot)
		return
	}
	r.pos += n
	return
}

func (r *snapReader) byte() (b byte, err error) {
	out, err := r.take(1)
	if err != nil {
		return
	}
	b = out[0]
	return
}

func (r *snapReader) hash() (h t.PatchHash, err error) {
	out, err := r.take(32)
	if err != nil {
		return
	}
	copy(h[:], out)
	return
}

func (r *snapReader) nodeID() (id t.NodeID, err error) {
	if id.Patch, err = r.hash(); err != nil {
		return
	}
	id.Index, err = r.uvarint()
	return
}

// maxSnapshotCount bounds per-section counts so a corrupt or hostile
// snapshot cannot trigger huge allocations before content checks fail.
const maxSnapshotCount = 1 << 28

// DecodeSnapshot reconstructs a graggle from EncodeSnapshot bytes and
// rebuilds all derived state: the deleted partition is re-formed from
// tombstone adjacency, every component is marked dirty, and pseudo-edges
// are resolved. The returned graggle uses the default clock
// (time.Now) — callers needing a deterministic clock re-inject it via
// SetClock.
func DecodeSnapshot(data []byte) (g *Graggle, err error) {
	r := &snapReader{data: data}
	magic, err := r.take(len(snapshotMagic))
	if err != nil {
		return
	}
	if string(magic) != snapshotMagic {
		err = eh.Errorf("magic %q: %w", magic, ErrBadSnapshot)
		return
	}

	g = New()
	g.nodes = t.NewNodeSet() // the root arrives via the live section

	readContent := func(id t.NodeID) (cerr error) {
		flag, cerr := r.byte()
		if cerr != nil {
			return
		}
		switch flag {
		case 0:
			return
		case 1:
			n, cerr2 := r.uvarint()
			if cerr2 != nil {
				return cerr2
			}
			raw, cerr2 := r.take(int(n))
			if cerr2 != nil {
				return cerr2
			}
			g.contents[id] = bytes.Clone(raw)
			return nil
		default:
			return eh.Errorf("content flag %d: %w", flag, ErrBadSnapshot)
		}
	}

	nLive, err := r.uvarint()
	if err != nil {
		return
	}
	if nLive > maxSnapshotCount {
		err = eh.Errorf("live count %d: %w", nLive, ErrBadSnapshot)
		return
	}
	for i := uint64(0); i < nLive; i++ {
		id, e := r.nodeID()
		if e != nil {
			err = e
			return
		}
		if g.nodes.Contains(id) {
			err = eh.Errorf("duplicate live node %v: %w", id, ErrBadSnapshot)
			return
		}
		g.nodes.Add(id)
		if err = readContent(id); err != nil {
			return
		}
	}

	nDeleted, err := r.uvarint()
	if err != nil {
		return
	}
	if nDeleted > maxSnapshotCount {
		err = eh.Errorf("deleted count %d: %w", nDeleted, ErrBadSnapshot)
		return
	}
	for i := uint64(0); i < nDeleted; i++ {
		id, e := r.nodeID()
		if e != nil {
			err = e
			return
		}
		if g.nodes.Contains(id) || g.deletedNodes.Contains(id) {
			err = eh.Errorf("node %v in both sections or duplicated: %w", id, ErrBadSnapshot)
			return
		}
		g.deletedNodes.Add(id)
		if err = readContent(id); err != nil {
			return
		}
		nanos, e := r.varint()
		if e != nil {
			err = e
			return
		}
		g.tombstoneAt[id] = time.Unix(0, nanos)
		purged, e := r.byte()
		if e != nil {
			err = e
			return
		}
		if purged == 1 {
			if _, present := g.contents[id]; present {
				// SweepTombstones destroys the bytes when it sets the
				// marker; purged-with-content is engine-impossible.
				err = eh.Errorf("tombstone %v purged but carrying content: %w", id, ErrBadSnapshot)
				return
			}
			g.contentPurged[id] = struct{}{}
		} else if purged != 0 {
			err = eh.Errorf("purged flag %d: %w", purged, ErrBadSnapshot)
			return
		}
		nDel, e := r.uvarint()
		if e != nil {
			err = e
			return
		}
		if nDel == 0 {
			// Engine states always record at least one deleter per
			// tombstone (DeleteNode records, the last UndeleteNode
			// resurrects); a zero-deleter tombstone is corruption.
			err = eh.Errorf("tombstone %v with no deleters: %w", id, ErrBadSnapshot)
			return
		}
		if nDel > maxSnapshotCount {
			err = eh.Errorf("deleter count %d: %w", nDel, ErrBadSnapshot)
			return
		}
		for j := uint64(0); j < nDel; j++ {
			h, e := r.hash()
			if e != nil {
				err = e
				return
			}
			g.addDeleter(id, h)
		}
	}

	nSources, err := r.uvarint()
	if err != nil {
		return
	}
	if nSources > maxSnapshotCount {
		err = eh.Errorf("source count %d: %w", nSources, ErrBadSnapshot)
		return
	}
	for i := uint64(0); i < nSources; i++ {
		src, e := r.nodeID()
		if e != nil {
			err = e
			return
		}
		nEdges, e := r.uvarint()
		if e != nil {
			err = e
			return
		}
		if nEdges > maxSnapshotCount {
			err = eh.Errorf("edge count %d: %w", nEdges, ErrBadSnapshot)
			return
		}
		for j := uint64(0); j < nEdges; j++ {
			dest, e := r.nodeID()
			if e != nil {
				err = e
				return
			}
			kind, e := r.byte()
			if e != nil {
				err = e
				return
			}
			if t.EdgeKindE(kind) == t.EdgeKindPseudo || kind > byte(t.EdgeKindPseudo) {
				err = eh.Errorf("edge kind %d: %w", kind, ErrBadSnapshot)
				return
			}
			by, e := r.hash()
			if e != nil {
				err = e
				return
			}
			if !g.HasNode(src) || !g.HasNode(dest) {
				err = eh.Errorf("edge %v->%v references unknown node: %w", src, dest, ErrBadSnapshot)
				return
			}
			// Edge kinds always reflect endpoint liveness in engine
			// states (retagging maintains this on delete/undelete);
			// a kind-inconsistent edge is corruption.
			anyDeleted := g.deletedNodes.Contains(src) || g.deletedNodes.Contains(dest)
			switch t.EdgeKindE(kind) {
			case t.EdgeKindLive:
				if anyDeleted {
					err = eh.Errorf("live edge %v->%v with tombstoned endpoint: %w", src, dest, ErrBadSnapshot)
					return
				}
			case t.EdgeKindDeleted:
				if !anyDeleted {
					err = eh.Errorf("deleted-kind edge %v->%v with both endpoints live: %w", src, dest, ErrBadSnapshot)
					return
				}
			}
			g.addEdgeInternal(src, dest, t.EdgeKindE(kind), by)
		}
	}
	if r.pos != len(r.data) {
		err = eh.Errorf("%d trailing bytes: %w", len(r.data)-r.pos, ErrBadSnapshot)
		return
	}
	if !g.nodes.Contains(t.RootNodeID) {
		err = eh.Errorf("root node missing from live section: %w", ErrBadSnapshot)
		return
	}

	// Rebuild derived state: partition from tombstone adjacency, then
	// mark every component dirty and resolve pseudo-edges.
	for _, id := range g.deletedNodes.Items() {
		g.deletedPartition.Add(id)
	}
	for _, id := range g.deletedNodes.Items() {
		g.mergeAdjacentDeleted(id)
	}
	for _, rep := range g.deletedPartition.Representatives() {
		g.dirtyReps[rep] = struct{}{}
	}
	g.ResolvePseudoEdges()
	return
}
