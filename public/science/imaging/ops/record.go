package ops

import (
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"runtime"
	"sync"
	"weak"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// Digest is a BLAKE3 digest: of an image's samples ([imaging.Image.Hash]),
// or of a request. In JSON it is 64 lower-case hex digits.
type Digest [32]byte

// IsZero reports whether the digest is all zeros, which no digest is.
func (inst Digest) IsZero() bool { return inst == Digest{} }

func (inst Digest) String() string { return hex.EncodeToString(inst[:]) }

// MarshalText writes the digest as hex.
func (inst Digest) MarshalText() ([]byte, error) { return []byte(inst.String()), nil }

// UnmarshalText reads 64 lower-case hex digits.
func (inst *Digest) UnmarshalText(text []byte) error {
	if len(text) != 2*len(inst) {
		return eb.Build().Int("length", len(text)).Errorf("a digest is 64 hex digits")
	}
	var d Digest
	if _, err := hex.Decode(d[:], text); err != nil {
		return eb.Build().Errorf("a digest is 64 hex digits: %w", err)
	}
	if string(text) != d.String() {
		return eb.Build().Errorf("a digest is written in lower case")
	}
	*inst = d
	return nil
}

// RecordSchema names the shape of a record's JSON document. It changes when
// a member's meaning does; [DecodeRecord] reads this one only.
const RecordSchema = "boxer.imaging.ops.record/1"

// Record is what an operation did (ADR-0400 §SD8): the operator and its
// version, the parameters as given and in canonical form, the inputs by
// digest — with the records of those that are results themselves — the
// first-pass quantities, and the output's digest.
type Record struct {
	Schema   string `json:"schema"`
	Operator string `json:"operator"`
	Version  uint32 `json:"version"`
	// Request is [RequestIdentity] of the request.
	Request Digest `json:"request"`
	// Params are the parameters as given, Canonical as validated.
	Params    Params `json:"params"`
	Canonical Params `json:"canonical"`
	// Pixels are the parameters with a unit other than pixels, resolved to
	// pixels of the input grid; empty while no operator has one.
	Pixels     map[string]float64 `json:"pixels,omitzero"`
	Inputs     []RecordInput      `json:"inputs"`
	Quantities []Quantity         `json:"quantities,omitzero"`
	Notes      []string           `json:"notes,omitzero"`
	Output     RecordOutput       `json:"output"`
}

// RecordInput is an input as a record names it. Besides the digest it
// carries what changes the values of the samples and is not part of a
// digest: the value transforms and the declared invalid value.
type RecordInput struct {
	Name       string      `json:"name"`
	Digest     Digest      `json:"digest"`
	Model      string      `json:"model"`
	Kind       string      `json:"kind"`
	Transforms []Transform `json:"transforms"`
	// Invalid is the stored value declared invalid; nil when none is.
	Invalid   *float64   `json:"invalid,omitzero"`
	Positions []Position `json:"positions,omitzero"`
	// Record is the record of the operation that made this input.
	Record *Record `json:"record,omitzero"`
}

// Transform is a value transform as a record writes it.
type Transform struct {
	Scale  float64 `json:"scale"`
	Offset float64 `json:"offset"`
}

// RecordOutput describes the result.
type RecordOutput struct {
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Model       string   `json:"model"`
	Kind        string   `json:"kind"`
	PlaneLabels []string `json:"planeLabels"`
	Digest      Digest   `json:"digest"`
}

// Encode writes the record as its JSON document: members in a fixed order,
// parameters by name, indented, so that one record gives one document.
func (inst Record) Encode() ([]byte, error) {
	return json.Marshal(inst, json.Deterministic(true), jsontext.Multiline(true), jsontext.WithIndent("  "))
}

// DecodeRecord reads a record's JSON document strictly: an unknown or
// repeated member, another schema at any depth, and a value of the wrong
// type are refused.
func DecodeRecord(data []byte) (rec Record, err error) {
	if err = json.Unmarshal(data, &rec, json.RejectUnknownMembers(true)); err != nil {
		return Record{}, eb.Build().Errorf("the operation record does not decode: %w", err)
	}
	if err = rec.check(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (inst Record) check() error {
	if inst.Schema != RecordSchema {
		return eb.Build().Str("schema", inst.Schema).Str("want", RecordSchema).Errorf("the operation record is of another schema")
	}
	for _, in := range inst.Inputs {
		if in.Record != nil {
			if err := in.Record.check(); err != nil {
				return eb.Build().Str("input", in.Name).Errorf("the record of an input: %w", err)
			}
		}
	}
	return nil
}

// The records of results still held: a store's record lives as long as the
// store. They are keyed by the store and not by its digest, since two
// operations — "a" and "a + 0" — can give one digest.
var results = struct {
	mu sync.Mutex
	m  map[weak.Pointer[tiles.Store]]*Record
}{m: make(map[weak.Pointer[tiles.Store]]*Record)}

func remember(s *tiles.Store, rec *Record) {
	wp := weak.Make(s)
	results.mu.Lock()
	results.m[wp] = rec
	results.mu.Unlock()
	runtime.AddCleanup(s, func(wp weak.Pointer[tiles.Store]) {
		results.mu.Lock()
		delete(results.m, wp)
		results.mu.Unlock()
	}, wp)
}

// RecordOf returns the record of the operation whose result s is, for as
// long as s is held; ok false for a store no operation of this process made.
func RecordOf(s *tiles.Store) (rec Record, ok bool) {
	if s == nil {
		return rec, false
	}
	results.mu.Lock()
	defer results.mu.Unlock()
	r, ok := results.m[weak.Make(s)]
	if !ok {
		return rec, false
	}
	return *r, true
}

func recordInput(d InputDesc, rec *Record) RecordInput {
	in := RecordInput{Name: d.Name, Digest: d.Digest, Model: d.Model.String(), Kind: d.Kind.String(),
		Transforms: make([]Transform, len(d.Transforms)), Positions: d.Positions, Record: rec}
	for i, t := range d.Transforms {
		in.Transforms[i] = Transform{Scale: t.Scale, Offset: t.Offset}
	}
	if d.HasInvalid {
		v := d.Invalid
		in.Invalid = &v
	}
	return in
}

func recordOutput(p *Plan, digest Digest) RecordOutput {
	return RecordOutput{Width: p.Width, Height: p.Height, Model: p.Model.String(), Kind: p.Kind.String(),
		PlaneLabels: p.PlaneLabels, Digest: digest}
}

// identityDocument is what a request's identity is the digest of.
type identityDocument struct {
	Operator  string          `json:"operator"`
	Version   uint32          `json:"version"`
	Canonical Params          `json:"canonical"`
	Inputs    []identityInput `json:"inputs"`
}

type identityInput struct {
	Name       string      `json:"name"`
	Digest     Digest      `json:"digest"`
	Transforms []Transform `json:"transforms"`
	Invalid    *float64    `json:"invalid,omitzero"`
}

func identityOf(d Declaration, canonical Params, inputs []InputDesc) (id Digest, err error) {
	doc := identityDocument{Operator: d.Id, Version: d.Version, Canonical: canonical, Inputs: make([]identityInput, len(inputs))}
	for i, in := range inputs {
		r := recordInput(in, nil)
		doc.Inputs[i] = identityInput{Name: r.Name, Digest: r.Digest, Transforms: r.Transforms, Invalid: r.Invalid}
	}
	data, err := json.Marshal(doc, json.Deterministic(true))
	if err != nil {
		return id, err
	}
	return Digest(blake3Sum(data)), nil
}
