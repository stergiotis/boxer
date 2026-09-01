package store

import (
	"bytes"
	"encoding/binary"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// Snapshot codec: a deterministic binary serialization of the pushoutgraph's
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

// EncodeSnapshot serializes the pushoutgraph. The output is deterministic:
// equal observable state yields equal bytes (maps are emitted in
// CompareNodeID / bytewise order).
func (inst *PushoutGraph) EncodeSnapshot() (data []byte, err error) {
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
		err = eb.Build().Int("offset", r.pos).Int("need", n).Errorf("truncated snapshot: %w", ErrBadSnapshot)
		return
	}
	out = r.data[r.pos : r.pos+n]
	r.pos += n
	return
}

func (r *snapReader) uvarint() (v uint64, err error) {
	v, n := binary.Uvarint(r.data[r.pos:])
	if n <= 0 {
		err = eb.Build().Int("pos", r.pos).Errorf("bad uvarint at offset: %w", ErrBadSnapshot)
		return
	}
	r.pos += n
	return
}

func (r *snapReader) varint() (v int64, err error) {
	v, n := binary.Varint(r.data[r.pos:])
	if n <= 0 {
		err = eb.Build().Int("pos", r.pos).Errorf("bad varint at offset: %w", ErrBadSnapshot)
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

// DecodeSnapshot reconstructs a pushoutgraph from EncodeSnapshot bytes and
// rebuilds all derived state: the deleted partition is re-formed from
// tombstone adjacency, every component is marked dirty, and pseudo-edges
// are resolved. The returned pushoutgraph uses the default clock
// (time.Now) — callers needing a deterministic clock re-inject it via
// SetClock.
func DecodeSnapshot(data []byte) (g *PushoutGraph, err error) {
	r := &snapReader{data: data}
	magic, err := r.take(len(snapshotMagic))
	if err != nil {
		return
	}
	if string(magic) != snapshotMagic {
		err = eb.Build().Bytes("magic", magic).Errorf("magic: %w", ErrBadSnapshot)
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
			return eb.Build().Uint8("flag", flag).Errorf("content flag: %w", ErrBadSnapshot)
		}
	}

	nLive, err := r.uvarint()
	if err != nil {
		return
	}
	if nLive > maxSnapshotCount {
		err = eb.Build().Uint64("nLive", nLive).Errorf("live count: %w", ErrBadSnapshot)
		return
	}
	for range nLive {
		id, e := r.nodeID()
		if e != nil {
			err = e
			return
		}
		if g.nodes.Contains(id) {
			err = eb.Build().Stringer("id", id).Errorf("duplicate live node: %w", ErrBadSnapshot)
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
		err = eb.Build().Uint64("nDeleted", nDeleted).Errorf("deleted count: %w", ErrBadSnapshot)
		return
	}
	for range nDeleted {
		id, e := r.nodeID()
		if e != nil {
			err = e
			return
		}
		if g.nodes.Contains(id) || g.deletedNodes.Contains(id) {
			err = eb.Build().Stringer("id", id).Errorf("node in both sections or duplicated: %w", ErrBadSnapshot)
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
				err = eb.Build().Stringer("id", id).Errorf("tombstone purged but carrying content: %w", ErrBadSnapshot)
				return
			}
			g.contentPurged[id] = struct{}{}
		} else if purged != 0 {
			err = eb.Build().Uint8("purged", purged).Errorf("purged flag: %w", ErrBadSnapshot)
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
			err = eb.Build().Stringer("id", id).Errorf("tombstone with no deleters: %w", ErrBadSnapshot)
			return
		}
		if nDel > maxSnapshotCount {
			err = eb.Build().Uint64("nDel", nDel).Errorf("deleter count: %w", ErrBadSnapshot)
			return
		}
		for range nDel {
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
		err = eb.Build().Uint64("nSources", nSources).Errorf("source count: %w", ErrBadSnapshot)
		return
	}
	for range nSources {
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
			err = eb.Build().Uint64("nEdges", nEdges).Errorf("edge count: %w", ErrBadSnapshot)
			return
		}
		for range nEdges {
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
				err = eb.Build().Uint8("kind", kind).Errorf("edge kind: %w", ErrBadSnapshot)
				return
			}
			by, e := r.hash()
			if e != nil {
				err = e
				return
			}
			if !g.HasNode(src) || !g.HasNode(dest) {
				err = eb.Build().Stringer("src", src).Stringer("dest", dest).Errorf("edge references an unknown node: %w", ErrBadSnapshot)
				return
			}
			// Edge kinds always reflect endpoint liveness in engine
			// states (retagging maintains this on delete/undelete);
			// a kind-inconsistent edge is corruption.
			anyDeleted := g.deletedNodes.Contains(src) || g.deletedNodes.Contains(dest)
			switch t.EdgeKindE(kind) {
			case t.EdgeKindLive:
				if anyDeleted {
					err = eb.Build().Stringer("src", src).Stringer("dest", dest).Errorf("live edge has a tombstoned endpoint: %w", ErrBadSnapshot)
					return
				}
			case t.EdgeKindDeleted:
				if !anyDeleted {
					err = eb.Build().Stringer("src", src).Stringer("dest", dest).Errorf("deleted-kind edge has both endpoints live: %w", ErrBadSnapshot)
					return
				}
			}
			g.addEdgeInternal(src, dest, t.EdgeKindE(kind), by)
		}
	}
	if r.pos != len(r.data) {
		err = eb.Build().Int("trailing", len(r.data)-r.pos).Errorf("snapshot has trailing bytes: %w", ErrBadSnapshot)
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
