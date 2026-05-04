//go:build llm_generated_opus47

package envelope

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

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
// is non-canonical and any signature / content-hash story over envelopes
// would be meaningless.
func TestEnvelope_RoundTripByteIdentical(tt *testing.T) {
	env := sampleEnvelope(tt)

	first, err := Encode(env)
	if err != nil {
		tt.Fatalf("first encode: %v", err)
	}

	decoded, err := Decode(first)
	if err != nil {
		tt.Fatalf("decode: %v", err)
	}

	second, err := Encode(decoded)
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
	env := sampleEnvelope(tt)

	// Apply the original to graggle A.
	gA := store.New()
	if err := env.Patch.Apply(gA); err != nil {
		tt.Fatalf("original apply: %v", err)
	}
	wantRender := gA.Render()

	// Encode + decode, then apply to graggle B.
	data, err := Encode(env)
	if err != nil {
		tt.Fatalf("encode: %v", err)
	}
	decoded, err := Decode(data)
	if err != nil {
		tt.Fatalf("decode: %v", err)
	}
	gB := store.New()
	if err := decoded.Patch.Apply(gB); err != nil {
		tt.Fatalf("decoded apply: %v", err)
	}
	gotRender := gB.Render()

	if !bytes.Equal(wantRender, gotRender) {
		tt.Fatalf("render mismatch:\nwant: %q\ngot:  %q", wantRender, gotRender)
	}
	if decoded.Patch.Hash != env.Patch.Hash {
		tt.Fatalf("hash mismatch after round-trip: %v vs %v", decoded.Patch.Hash, env.Patch.Hash)
	}
}

// A tampered envelope must be rejected at Decode time. We mutate the
// description (which doesn't enter the hash) — the stored Hash stays put,
// but ComputeHash on the decoded patch must equal the stored hash, so
// that's not the right knob. Instead, mutate Changes (which does enter
// the hash) and reset Hash back to the original; the recomputed hash will
// no longer match.
func TestEnvelope_DecodeRejectsTamperedHash(tt *testing.T) {
	env := sampleEnvelope(tt)
	original := env.Patch.Hash

	data, err := Encode(env)
	if err != nil {
		tt.Fatalf("encode: %v", err)
	}

	// Round-trip into a generic map so we can edit one node's content
	// (which feeds ComputeHash) without touching the stored Hash.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		tt.Fatalf("unmarshal raw: %v", err)
	}
	patchObj := raw["patch"].(map[string]any)
	changes := patchObj["Changes"].([]any)
	firstChange := changes[0].(map[string]any)
	firstChange["Content"] = "dGFtcGVyZWQK" // base64("tampered\n"); valid base64 but different bytes
	tampered, err := json.Marshal(raw)
	if err != nil {
		tt.Fatalf("re-marshal: %v", err)
	}

	_, err = Decode(tampered)
	if err == nil {
		tt.Fatal("expected Decode to reject tampered envelope, got nil error")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		tt.Fatalf("expected hash mismatch error, got: %v", err)
	}
	// Sanity: the stored hash in the tampered raw bytes is unchanged.
	if storedHex, _ := original.MarshalText(); !bytes.Contains(tampered, storedHex) {
		tt.Fatal("test setup did not preserve stored hash in the tampered bytes")
	}
}

// Decode must reject envelopes that omit the patch entirely.
func TestEnvelope_DecodeRejectsMissingPatch(tt *testing.T) {
	data := []byte(`{"producer":"alice","timestamp":"2026-05-01T00:00:00Z"}`)
	_, err := Decode(data)
	if err == nil {
		tt.Fatal("expected error for missing patch, got nil")
	}
	if !strings.Contains(err.Error(), "missing patch") {
		tt.Fatalf("expected missing patch error, got: %v", err)
	}
}

// Encode must refuse a nil-patch envelope so we never produce a file that
// would only fail at decode time.
func TestEnvelope_EncodeRejectsNilPatch(tt *testing.T) {
	_, err := Encode(EnvelopeV1{Producer: "alice"})
	if err == nil {
		tt.Fatal("expected error for nil patch, got nil")
	}
}