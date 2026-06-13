package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func testRegistry(tt *testing.T) *Registry {
	tt.Helper()
	reg, err := NewRegistry(JSONV1{})
	if err != nil {
		tt.Fatal(err)
	}
	return reg
}

// samplePatch builds a small patch with two new nodes chained off root, so
// the hash is non-trivial and the apply-equivalence test has something to
// render.
func samplePatch(tt *testing.T) *patch.Patch {
	tt.Helper()
	return patch.NewPatch("alice", "two-line insert", nil, []patch.Change{
		{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("hello\n"),
			UpContext: []t.NodeID{t.RootNodeID},
		},
		{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 1},
			Content:   []byte("world\n"),
			UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}},
		},
	})
}

func sampleEnvelope(tt *testing.T) EnvelopeV1 {
	tt.Helper()
	return EnvelopeV1{
		Patch:     samplePatch(tt),
		Producer:  "alice@example",
		Timestamp: time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC),
	}
}

// Encode → Decode → re-Encode must be byte-identical, otherwise the codec
// path is non-canonical and any signature / content-hash story over framed
// envelopes would be meaningless.
func TestEnvelope_RoundTripByteIdentical(tt *testing.T) {
	reg := testRegistry(tt)
	env := sampleEnvelope(tt)

	first, err := reg.Encode(JSONV1Name, env)
	if err != nil {
		tt.Fatalf("first encode: %v", err)
	}
	decoded, name, err := reg.Decode(first)
	if err != nil {
		tt.Fatalf("decode: %v", err)
	}
	if name != JSONV1Name {
		tt.Fatalf("frame name: %q", name)
	}
	second, err := reg.Encode(JSONV1Name, decoded)
	if err != nil {
		tt.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(first, second) {
		tt.Fatalf("byte-identity violated:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// The decoded patch, applied to a fresh graggle, must reproduce the same
// rendered output as the original — semantic round-trip on top of the
// byte-level one.
func TestEnvelope_DecodedPatchAppliesEquivalently(tt *testing.T) {
	reg := testRegistry(tt)
	env := sampleEnvelope(tt)

	gA := store.New()
	if err := env.Patch.Apply(gA); err != nil {
		tt.Fatalf("original apply: %v", err)
	}
	wantRender := gA.Render()

	data, err := reg.Encode(JSONV1Name, env)
	if err != nil {
		tt.Fatalf("encode: %v", err)
	}
	decoded, _, err := reg.Decode(data)
	if err != nil {
		tt.Fatalf("decode: %v", err)
	}
	gB := store.New()
	if err := decoded.Patch.Apply(gB); err != nil {
		tt.Fatalf("decoded apply: %v", err)
	}
	if got := gB.Render(); !bytes.Equal(wantRender, got) {
		tt.Fatalf("render mismatch:\nwant: %q\ngot:  %q", wantRender, got)
	}
	if decoded.Patch.Hash != env.Patch.Hash {
		tt.Fatalf("hash mismatch after round-trip: %v vs %v", decoded.Patch.Hash, env.Patch.Hash)
	}
}

// mutatePayload unframes, applies a JSON-level edit to the payload, and
// re-frames — the standard tamper vehicle for these tests.
func mutatePayload(tt *testing.T, framed []byte, edit func(patchObj map[string]any)) []byte {
	tt.Helper()
	name, payload, err := Unframe(framed)
	if err != nil {
		tt.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		tt.Fatal(err)
	}
	edit(raw["patch"].(map[string]any))
	mutated, err := json.Marshal(raw)
	if err != nil {
		tt.Fatal(err)
	}
	reframed, err := Frame(name, mutated)
	if err != nil {
		tt.Fatal(err)
	}
	return reframed
}

// Content tampering must fail the hash check.
func TestEnvelope_DecodeRejectsTamperedContent(tt *testing.T) {
	reg := testRegistry(tt)
	framed, err := reg.Encode(JSONV1Name, sampleEnvelope(tt))
	if err != nil {
		tt.Fatal(err)
	}
	tampered := mutatePayload(tt, framed, func(po map[string]any) {
		po["Changes"].([]any)[0].(map[string]any)["Content"] = "dGFtcGVyZWQK" // base64("tampered\n")
	})
	_, _, err = reg.Decode(tampered)
	if !errors.Is(err, ErrTampered) {
		tt.Fatalf("expected ErrTampered, got: %v", err)
	}
}

// Dependency tampering (strip or extend) must fail the hash check: the
// dependency set is part of patch identity.
func TestEnvelope_DecodeRejectsTamperedDependencies(tt *testing.T) {
	reg := testRegistry(tt)
	dep := patch.NewPatch("alice", "dep", nil, []patch.Change{{
		Kind:      patch.ChangeKindNewNode,
		NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:   []byte("base\n"),
		UpContext: []t.NodeID{t.RootNodeID},
	}})
	p := patch.NewPatch("alice", "edit", []t.PatchHash{dep.Hash}, []patch.Change{{
		Kind: patch.ChangeKindDeleteNode, NodeID: t.NodeID{Patch: dep.Hash, Index: 0},
	}})
	framed, err := reg.Encode(JSONV1Name, EnvelopeV1{Patch: p, Producer: "alice", Timestamp: time.Unix(0, 0).UTC()})
	if err != nil {
		tt.Fatal(err)
	}

	stripped := mutatePayload(tt, framed, func(po map[string]any) { po["Dependencies"] = []any{} })
	if _, _, err := reg.Decode(stripped); !errors.Is(err, ErrTampered) {
		tt.Fatalf("stripped dependencies: expected ErrTampered, got: %v", err)
	}

	bogus := t.PatchHash{9, 9, 9}
	bogusHex, _ := bogus.MarshalText()
	extended := mutatePayload(tt, framed, func(po map[string]any) {
		po["Dependencies"] = append(po["Dependencies"].([]any), string(bogusHex))
	})
	if _, _, err := reg.Decode(extended); !errors.Is(err, ErrTampered) {
		tt.Fatalf("extended dependencies: expected ErrTampered, got: %v", err)
	}
}

// A patch that hashes consistently but whose changes reference a patch the
// dependency list does not declare was authored broken — Validate rejects
// it on encode AND decode.
func TestEnvelope_RejectsUndeclaredDependency(tt *testing.T) {
	reg := testRegistry(tt)
	foreign := t.PatchHash{42}
	p := patch.NewPatch("mallory", "deps stripped at authoring time", nil, []patch.Change{{
		Kind: patch.ChangeKindDeleteNode, NodeID: t.NodeID{Patch: foreign, Index: 0},
	}})
	env := EnvelopeV1{Patch: p, Producer: "mallory", Timestamp: time.Unix(0, 0).UTC()}
	if _, err := reg.Encode(JSONV1Name, env); !errors.Is(err, ErrUndeclaredDependency) {
		tt.Fatalf("encode: expected ErrUndeclaredDependency, got: %v", err)
	}
	// Bypass the write-path guard via the raw codec, then decode.
	payload, err := JSONV1{}.Encode(env)
	if err != nil {
		tt.Fatal(err)
	}
	framed, err := Frame(JSONV1Name, payload)
	if err != nil {
		tt.Fatal(err)
	}
	if _, _, err := reg.Decode(framed); !errors.Is(err, ErrUndeclaredDependency) {
		tt.Fatalf("decode: expected ErrUndeclaredDependency, got: %v", err)
	}
}

// Placeholder NodeIDs exist only in the pre-fixup form and are rejected.
func TestEnvelope_RejectsPlaceholderNodeIDs(tt *testing.T) {
	reg := testRegistry(tt)
	p := &patch.Patch{
		Author: "mallory",
		Changes: []patch.Change{{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("x\n"),
			UpContext: []t.NodeID{t.RootNodeID},
		}},
	}
	p.Hash = p.ComputeHash() // self-consistent hash over the pre-fixup form
	env := EnvelopeV1{Patch: p, Producer: "mallory", Timestamp: time.Unix(0, 0).UTC()}
	if _, err := reg.Encode(JSONV1Name, env); !errors.Is(err, ErrPlaceholderNodeID) {
		tt.Fatalf("expected ErrPlaceholderNodeID, got: %v", err)
	}
}

// Frame-level failures: missing patch, unknown codec, bad frame bytes.
func TestEnvelope_FrameAndRegistryRejections(tt *testing.T) {
	reg := testRegistry(tt)

	if _, err := reg.Encode(JSONV1Name, EnvelopeV1{Producer: "alice"}); !errors.Is(err, ErrMissingPatch) {
		tt.Fatalf("nil patch: expected ErrMissingPatch, got: %v", err)
	}
	payload, err := JSONV1{}.Encode(sampleEnvelope(tt))
	if err != nil {
		tt.Fatal(err)
	}
	framed, err := Frame("nope1", payload)
	if err != nil {
		tt.Fatal(err)
	}
	if _, _, err := reg.Decode(framed); !errors.Is(err, ErrUnknownCodec) {
		tt.Fatalf("unknown codec: got %v", err)
	}
	for _, bad := range [][]byte{nil, []byte("XXXX"), []byte("PXE1"), []byte("PXE1\xff")} {
		if _, _, err := reg.Decode(bad); !errors.Is(err, ErrBadFrame) {
			tt.Fatalf("bad frame %q: got %v", bad, err)
		}
	}
	if _, err := Frame(strings.Repeat("x", 64), nil); !errors.Is(err, ErrCodecName) {
		tt.Fatal("overlong codec name must be rejected")
	}
}
