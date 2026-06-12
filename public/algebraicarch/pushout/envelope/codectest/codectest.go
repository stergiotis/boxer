// Package codectest is the executable conformance contract for
// [envelope.CodecI] implementations. A codec passes when Run is green;
// each requirement is also exposed as a Check* function returning an
// error so suites can be meta-tested against deliberately broken codecs.
//
// Implementors: a codec must be a PURE, DETERMINISTIC serialization of
// [envelope.EnvelopeV1]. It must not validate semantics (the Registry
// does), must not mutate the envelope, and must round-trip every field
// it claims to carry — at minimum the full patch (hash, author,
// description, dependencies, changes incl. contents and contexts) and
// the envelope provenance (Producer, Timestamp). Equal envelopes must
// encode to equal bytes; names are wire-frozen (see CodecI.Name docs).
package codectest

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/envelope"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// CanonicalEnvelope builds the conformance fixture: a patch exercising
// every change kind, both context slices, empty and quoted contents, a
// canonicalized multi-entry dependency set, and fixed provenance.
func CanonicalEnvelope() envelope.EnvelopeV1 {
	depA := t.PatchHash{0x01, 0x02}
	depB := t.PatchHash{0xAA}
	foreign := t.NodeID{Patch: depA, Index: 7}
	p := patch.NewPatch("codec-author", "codec description", []t.PatchHash{depB, depA, depB}, []patch.Change{
		{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("first line\n"),
			UpContext: []t.NodeID{t.RootNodeID},
		},
		{
			Kind:        patch.ChangeKindNewNode,
			NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: 1},
			Content:     []byte(""),
			UpContext:   []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}},
			DownContext: []t.NodeID{foreign},
		},
		{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 2},
			Content:   []byte("quoted \"x\" \\ multi\nline\n"),
			UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}},
		},
		{Kind: patch.ChangeKindDeleteNode, NodeID: foreign},
		{Kind: patch.ChangeKindNewEdge, Src: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Dest: foreign},
	})
	return envelope.EnvelopeV1{
		Patch:     p,
		Producer:  "codec-producer",
		Timestamp: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
	}
}

// CheckRoundTrip: decode∘encode must reproduce the envelope — identical
// patch identity, full logical equality, and Validate-clean output.
func CheckRoundTrip(c envelope.CodecI) (err error) {
	env := CanonicalEnvelope()
	payload, err := c.Encode(env)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	got, err := c.Decode(payload)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if got.Patch == nil {
		return fmt.Errorf("decoded envelope lost its patch")
	}
	if got.Patch.Hash != env.Patch.Hash {
		return fmt.Errorf("identity changed: %s -> %s", env.Patch.Hash, got.Patch.Hash)
	}
	if err = envelope.Validate(got); err != nil {
		return fmt.Errorf("decoded envelope fails validation: %w", err)
	}
	if !reflect.DeepEqual(normalize(env), normalize(got)) {
		return fmt.Errorf("logical round-trip mismatch:\n in: %#v\nout: %#v", env, got)
	}
	return
}

// normalize maps an envelope to a comparable shape: timestamps to UTC
// (codecs may carry zone differently as long as the instant survives)
// and nil/empty slices folded.
func normalize(env envelope.EnvelopeV1) envelope.EnvelopeV1 {
	env.Timestamp = env.Timestamp.UTC()
	p := *env.Patch
	if len(p.Dependencies) == 0 {
		p.Dependencies = nil
	}
	for i := range p.Changes {
		if len(p.Changes[i].Content) == 0 {
			p.Changes[i].Content = nil
		}
		if len(p.Changes[i].UpContext) == 0 {
			p.Changes[i].UpContext = nil
		}
		if len(p.Changes[i].DownContext) == 0 {
			p.Changes[i].DownContext = nil
		}
	}
	env.Patch = &p
	return env
}

// CheckDeterminism: equal envelopes must encode to equal bytes.
func CheckDeterminism(c envelope.CodecI) (err error) {
	a, err := c.Encode(CanonicalEnvelope())
	if err != nil {
		return fmt.Errorf("encode #1: %w", err)
	}
	b, err := c.Encode(CanonicalEnvelope())
	if err != nil {
		return fmt.Errorf("encode #2: %w", err)
	}
	if !bytes.Equal(a, b) {
		return fmt.Errorf("encoding is not deterministic (%d vs %d bytes)", len(a), len(b))
	}
	return
}

// CheckRegistry: the codec must compose with the frame layer — a
// registry round-trip preserves identity and reports the codec's name.
func CheckRegistry(c envelope.CodecI) (err error) {
	reg, err := envelope.NewRegistry(c)
	if err != nil {
		return fmt.Errorf("registry rejects codec: %w", err)
	}
	env := CanonicalEnvelope()
	framed, err := reg.Encode(c.Name(), env)
	if err != nil {
		return fmt.Errorf("registry encode: %w", err)
	}
	got, name, err := reg.Decode(framed)
	if err != nil {
		return fmt.Errorf("registry decode: %w", err)
	}
	if name != c.Name() {
		return fmt.Errorf("frame carried name %q, codec says %q", name, c.Name())
	}
	if got.Patch.Hash != env.Patch.Hash {
		return fmt.Errorf("identity changed through registry: %s -> %s", env.Patch.Hash, got.Patch.Hash)
	}
	return
}

// Run executes the full conformance suite as subtests.
func Run(tt *testing.T, c envelope.CodecI) {
	tt.Helper()
	checks := []struct {
		name  string
		check func(envelope.CodecI) error
	}{
		{"RoundTrip", CheckRoundTrip},
		{"Determinism", CheckDeterminism},
		{"Registry", CheckRegistry},
	}
	for _, c2 := range checks {
		tt.Run(c2.name, func(tt *testing.T) {
			if err := c2.check(c); err != nil {
				tt.Fatal(err)
			}
		})
	}
}
