// Mixed-codec fleet: a json1 repo and a custom-codec repo exchange
// patches in both directions. Envelopes ship as received (the frame
// names the codec), identity is wire-independent, and both registries
// know both codecs — the interop property the framed design exists for,
// and the shape of the upcoming custom-format demonstrator.
package exchange_test

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/exchange/inproc"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/repo/filestore"
)

// xor1 is a toy custom codec: the jsonv1 payload XORed with a constant —
// trivially reversible, deliberately not JSON on the wire.
type xor1 struct{ inner envelope.JSONV1 }

func (xor1) Name() string { return "xor1" }

func xorBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, x := range b {
		out[i] = x ^ 0x5A
	}
	return out
}

func (c xor1) Encode(env envelope.EnvelopeV1) ([]byte, error) {
	payload, err := c.inner.Encode(env)
	if err != nil {
		return nil, err
	}
	return xorBytes(payload), nil
}

func (c xor1) Decode(payload []byte) (envelope.EnvelopeV1, error) {
	return c.inner.Decode(xorBytes(payload))
}

func openMixed(tt *testing.T, producer, wire string) *repo.Repo {
	tt.Helper()
	st, err := filestore.Open(tt.TempDir())
	if err != nil {
		tt.Fatal(err)
	}
	reg, err := envelope.NewRegistry(envelope.JSONV1{}, xor1{})
	if err != nil {
		tt.Fatal(err)
	}
	tick := time.Unix(1_500_000_000, 0).UTC()
	r, err := repo.Open(context.Background(), repo.Options{
		Storage: st, Codecs: reg, Wire: wire, Producer: producer,
		Clock: func() time.Time { tick = tick.Add(time.Minute); return tick },
	})
	if err != nil {
		tt.Fatal(err)
	}
	tt.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func TestMixedCodecFleetConverges(tt *testing.T) {
	ctx := context.Background()
	jsonRepo := openMixed(tt, "alice", envelope.JSONV1Name)
	xorRepo := openMixed(tt, "bob", "xor1")

	hA, err := jsonRepo.Record(ctx, "alice", "from json side", []patch.Change{{
		Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content: []byte("alpha\n"), UpContext: []t.NodeID{t.RootNodeID},
	}})
	if err != nil {
		tt.Fatal(err)
	}
	if _, err := exchange.Pull(ctx, xorRepo, inproc.New(jsonRepo)); err != nil {
		tt.Fatal(err)
	}
	// Bob extends ON TOP of alice's patch, recording in HIS codec.
	hB, err := xorRepo.Record(ctx, "bob", "from xor side", []patch.Change{{
		Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content: []byte("beta\n"), UpContext: []t.NodeID{{Patch: hA, Index: 0}},
	}})
	if err != nil {
		tt.Fatal(err)
	}
	if _, err := exchange.Pull(ctx, jsonRepo, inproc.New(xorRepo)); err != nil {
		tt.Fatal(err)
	}

	aApplied, _ := jsonRepo.Applied(ctx)
	bApplied, _ := xorRepo.Applied(ctx)
	if len(aApplied) != 2 || len(bApplied) != 2 || aApplied[0] != bApplied[0] || aApplied[1] != bApplied[1] {
		tt.Fatalf("mixed-codec fleet diverged: %v vs %v", aApplied, bApplied)
	}
	// Identity is wire-independent and each envelope kept its origin codec.
	infoA, err := jsonRepo.PatchInfo(ctx, hB)
	if err != nil || infoA.Codec != "xor1" {
		tt.Fatalf("bob's patch on alice: codec=%q err=%v", infoA.Codec, err)
	}
	infoB, err := xorRepo.PatchInfo(ctx, hA)
	if err != nil || infoB.Codec != envelope.JSONV1Name {
		tt.Fatalf("alice's patch on bob: codec=%q err=%v", infoB.Codec, err)
	}
}
