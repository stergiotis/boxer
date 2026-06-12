// Snapshot codec tests: a byte-golden pins the GRG1 format (the
// persistence format is a compatibility surface — old snapshots must
// keep loading), a rapid property drives random graggles through
// encode/decode and demands observable-state equality plus rebuilt-
// derived-state invariants, and a fuzzer guards the decoder against
// hostile bytes.
package store

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

var updateSnapshotGolden = flag.Bool("update", false, "rewrite golden files from current output")

// goldenGraggle builds a deterministic graggle exercising every encoded
// feature: live nodes (incl. empty content), a multi-deleter tombstone,
// a purged tombstone with deterministic timestamps, and live/deleted
// edges from distinct patches.
func goldenGraggle(tt testing.TB) *Graggle {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0), time.Unix(2000, 0)))

	a, b, c := nid("snapA", 0), nid("snapA", 1), nid("snapA", 2)
	if err := g.AddNode(a, []byte("alpha\n"), ph("snapA"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(b, []byte(""), ph("snapA"), []t.NodeID{a}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(c, []byte("gamma\n"), ph("snapA"), []t.NodeID{b}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddEdge(a, c, ph("snapB")); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(b, ph("del1")); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(b, ph("del2")); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(c, ph("del1")); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()
	if n, _ := g.SweepTombstones(time.Unix(5000, 0), 3*time.Second); n == 0 {
		tt.Fatal("setup: expected the older tombstone purged")
	}
	return g
}

func TestSnapshot_Golden(tt *testing.T) {
	g := goldenGraggle(tt)
	data, err := g.EncodeSnapshot()
	if err != nil {
		tt.Fatal(err)
	}
	path := filepath.Join("testdata", "golden_graggle.grg1")
	if *updateSnapshotGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			tt.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			tt.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		tt.Fatalf("read golden (regenerate with -args -update): %v", err)
	}
	if !bytes.Equal(data, want) {
		tt.Fatalf("snapshot format drift — old persisted snapshots would stop loading; if intentional, bump the magic and keep a loader for GRG1.\n got %d bytes, want %d", len(data), len(want))
	}
	// And the golden must keep DECODING — the actual compatibility claim.
	dec, err := DecodeSnapshot(want)
	if err != nil {
		tt.Fatalf("golden snapshot no longer decodes: %v", err)
	}
	if errs := qc.CheckInvariants(dec); len(errs) != 0 {
		tt.Fatalf("decoded golden violates invariants: %v", errs)
	}
}

func TestSnapshot_Deterministic(tt *testing.T) {
	g := goldenGraggle(tt)
	a, err := g.EncodeSnapshot()
	if err != nil {
		tt.Fatal(err)
	}
	b, err := g.Clone().EncodeSnapshot()
	if err != nil {
		tt.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		tt.Fatal("encode is not deterministic across clones")
	}
}

// snapshotState extends the observable projection with the retention
// bookkeeping the snapshot must preserve exactly.
func snapshotState(g *Graggle) string {
	var sb bytes.Buffer
	sb.WriteString(canonicalObservableState(g))
	for _, id := range g.deletedNodes.Items() {
		sb.WriteString(id.String())
		sb.WriteString(" at=")
		sb.WriteString(g.tombstoneAt[id].UTC().Format(time.RFC3339Nano))
		if _, p := g.contentPurged[id]; p {
			sb.WriteString(" purged")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestSnapshot_RoundTripProperty(tt *testing.T) {
	lineVals := []string{"a\n", "b\n", "c\n", "d\n"}
	rapid.Check(tt, func(rt *rapid.T) {
		// Random history via real patches, with a deterministic clock so
		// tombstoneAt comparisons are exact.
		g := New()
		tick := time.Unix(10_000, 0)
		g.SetClock(func() time.Time { tick = tick.Add(time.Minute); return tick })

		var patches []*patch.Patch
		steps := rapid.IntRange(1, 8).Draw(rt, "steps")
		for i := 0; i < steps; i++ {
			liveIDs := []t.NodeID{}
			for id := range g.AllLiveNodes() {
				if id != t.RootNodeID {
					liveIDs = append(liveIDs, id)
				}
			}
			if len(liveIDs) > 0 && rapid.Bool().Draw(rt, "delete") {
				victim := liveIDs[rapid.IntRange(0, len(liveIDs)-1).Draw(rt, "victim")]
				ch := []patch.Change{{Kind: patch.ChangeKindDeleteNode, NodeID: victim}}
				p := patch.NewPatch("snap", "del", patch.ComputeDependencies(ch), ch)
				if err := p.Apply(g); err != nil {
					rt.Fatalf("del apply: %v", err)
				}
				patches = append(patches, p)
				continue
			}
			up := t.RootNodeID
			if len(liveIDs) > 0 {
				up = liveIDs[rapid.IntRange(0, len(liveIDs)-1).Draw(rt, "up")]
			}
			ch := []patch.Change{{
				Kind:      patch.ChangeKindNewNode,
				NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(i)},
				Content:   []byte(rapid.SampledFrom(lineVals).Draw(rt, "val")),
				UpContext: []t.NodeID{up},
			}}
			p := patch.NewPatch("snap", "ins", patch.ComputeDependencies(ch), ch)
			if err := p.Apply(g); err != nil {
				rt.Fatalf("ins apply: %v", err)
			}
			patches = append(patches, p)
		}
		if rapid.Bool().Draw(rt, "sweep") {
			g.SweepTombstones(tick.Add(time.Hour), time.Duration(rapid.IntRange(0, 5).Draw(rt, "horizon"))*time.Minute)
		}

		data, err := g.EncodeSnapshot()
		if err != nil {
			rt.Fatalf("encode: %v", err)
		}
		dec, err := DecodeSnapshot(data)
		if err != nil {
			rt.Fatalf("decode: %v", err)
		}
		if errs := qc.CheckInvariants(dec); len(errs) != 0 {
			rt.Fatalf("decoded graggle violates invariants: %v", errs)
		}
		if got, want := snapshotState(dec), snapshotState(g); got != want {
			rt.Fatalf("state diverged across snapshot round-trip:\n got:\n%s\nwant:\n%s", got, want)
		}
		// Idempotence: re-encoding the decoded graggle is byte-identical.
		again, err := dec.EncodeSnapshot()
		if err != nil {
			rt.Fatalf("re-encode: %v", err)
		}
		if !bytes.Equal(again, data) {
			rt.Fatal("encode∘decode is not byte-idempotent")
		}
	})
}

func FuzzDecodeSnapshot(f *testing.F) {
	g := New()
	seed, _ := g.EncodeSnapshot()
	f.Add(seed)
	gg := goldenGraggle(f)
	rich, _ := gg.EncodeSnapshot()
	f.Add(rich)
	f.Add([]byte("GRG1"))
	f.Add([]byte{})
	f.Fuzz(func(tt *testing.T, data []byte) {
		dec, err := DecodeSnapshot(data)
		if err != nil {
			return // rejected hostile/corrupt bytes are the success case
		}
		// Anything accepted must be structurally consistent and re-encode
		// idempotently. Connectivity is deliberately NOT demanded:
		// orphaned live nodes are representable engine state (reported
		// as "orphan" conflicts, see algo.DetectConflicts), so the
		// decoder must accept snapshots containing them.
		for _, e := range qc.CheckInvariants(dec) {
			if strings.Contains(e.Error(), "unreachable from root") {
				continue
			}
			tt.Fatalf("decoder accepted bytes yielding invariant violation: %v", e)
		}
		enc, err := dec.EncodeSnapshot()
		if err != nil {
			tt.Fatalf("re-encode of accepted snapshot failed: %v", err)
		}
		dec2, err := DecodeSnapshot(enc)
		if err != nil {
			tt.Fatalf("re-decode failed: %v", err)
		}
		enc2, err := dec2.EncodeSnapshot()
		if err != nil {
			tt.Fatal(err)
		}
		if !bytes.Equal(enc, enc2) {
			tt.Fatal("encode not idempotent on accepted input")
		}
	})
}
