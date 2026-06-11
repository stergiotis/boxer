// Wire-format goldens: the exact BLAKE3 hash of a canonical patch and
// the exact envelope bytes are pinned. Property and differential tests
// verify behavior relative to other code; these pin it against an
// immutable constant, so ANY change to the hash payload shape, the
// canonicalization, or the JSON encoding fails loudly here first.
//
// If a failure is intentional (a deliberate wire-format change), update
// the constants AND note that every persisted envelope file in every
// repo becomes invalid — peers on the old format cannot exchange
// patches with peers on the new one.
package envelope

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// goldenPatch exercises every change kind, both context slices, byte
// content, deps canonicalization (declared out of order, with a
// duplicate), and a placeholder self-reference chain.
func goldenPatch() *patch.Patch {
	depA := t.PatchHash{0x01, 0x02}
	depB := t.PatchHash{0xAA}
	foreign := t.NodeID{Patch: depA, Index: 7}
	return patch.NewPatch("golden-author", "golden description", []t.PatchHash{depB, depA, depB}, []patch.Change{
		{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("first line\n"),
			UpContext: []t.NodeID{t.RootNodeID},
		},
		{
			Kind:        patch.ChangeKindNewNode,
			NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: 1},
			Content:     []byte("second \"quoted\" line\n"),
			UpContext:   []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}},
			DownContext: []t.NodeID{foreign},
		},
		{Kind: patch.ChangeKindDeleteNode, NodeID: foreign},
		{Kind: patch.ChangeKindNewEdge, Src: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Dest: foreign},
	})
}

const goldenPatchHashHex = "f37bb5a9be25a4f0ed7c3466742c30a86c6df297b4b8594d38a1ad6a0507d03f"

func TestGolden_PatchHash(tt *testing.T) {
	p := goldenPatch()
	got, _ := p.Hash.MarshalText()
	if string(got) != goldenPatchHashHex {
		tt.Fatalf("canonical patch hash changed — this breaks every persisted envelope.\n got: %s\nwant: %s", got, goldenPatchHashHex)
	}
}

var updateGolden = flag.Bool("update", false, "rewrite golden files from current output")

func TestGolden_EnvelopeBytes(tt *testing.T) {
	env := EnvelopeV1{
		Patch:     goldenPatch(),
		Producer:  "golden-producer",
		Timestamp: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
	data, err := Encode(env)
	if err != nil {
		tt.Fatal(err)
	}
	path := filepath.Join("testdata", "golden_envelope.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			tt.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			tt.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		tt.Fatalf("read golden (run with -args -update to regenerate): %v", err)
	}
	if !bytes.Equal(data, want) {
		tt.Fatalf("canonical envelope bytes changed — wire format drift; if intentional, regenerate with -args -update and note that persisted envelopes invalidate.\n got:\n%s\nwant:\n%s", data, want)
	}
	if _, err := Decode(data); err != nil {
		tt.Fatalf("golden envelope no longer decodes: %v", err)
	}
}
