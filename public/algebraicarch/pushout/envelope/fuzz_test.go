// Fuzzer for the envelope codec: arbitrary bytes must never panic, and
// anything Decode accepts must re-encode/re-decode to the same patch
// identity with the hash, dependency-declaration, and placeholder guards
// holding throughout.
package envelope

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func fuzzSeedEnvelope() []byte {
	dep := patch.NewPatch("alice", "dep", nil, []patch.Change{{
		Kind:      patch.ChangeKindNewNode,
		NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:   []byte("base\n"),
		UpContext: []t.NodeID{t.RootNodeID},
	}})
	p := patch.NewPatch("alice", "edit", []t.PatchHash{dep.Hash}, []patch.Change{{
		Kind: patch.ChangeKindDeleteNode, NodeID: t.NodeID{Patch: dep.Hash, Index: 0},
	}})
	data, err := Encode(EnvelopeV1{Patch: p, Producer: "alice", Timestamp: time.Unix(0, 0).UTC()})
	if err != nil {
		panic(err)
	}
	return data
}

func FuzzDecode(f *testing.F) {
	seed := fuzzSeedEnvelope()
	f.Add(seed)
	f.Add([]byte("{}"))
	f.Add([]byte(`{"patch":null}`))
	// Single-byte corruptions of the valid envelope steer the fuzzer
	// toward the interesting boundary: almost-valid inputs.
	for _, i := range []int{0, len(seed) / 2, len(seed) - 2} {
		mutated := append([]byte(nil), seed...)
		mutated[i] ^= 0x20
		f.Add(mutated)
	}
	f.Fuzz(func(tt *testing.T, data []byte) {
		env, err := Decode(data)
		if err != nil {
			return
		}
		// Accepted envelopes must round-trip with identity intact.
		again, eerr := Encode(env)
		if eerr != nil {
			tt.Fatalf("re-encode of accepted envelope failed: %v", eerr)
		}
		env2, derr := Decode(again)
		if derr != nil {
			tt.Fatalf("re-decode of re-encoded envelope failed: %v", derr)
		}
		if env2.Patch.Hash != env.Patch.Hash {
			tt.Fatalf("identity changed across round-trip: %s -> %s", env.Patch.Hash, env2.Patch.Hash)
		}
	})
}
