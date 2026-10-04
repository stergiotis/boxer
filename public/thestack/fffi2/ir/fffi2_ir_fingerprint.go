package ir

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"

	"lukechampine.com/blake3"
)

// Fingerprint digests what the two ends of an FFFI2 pipe must agree on: the
// top-level nodes in the order their opcodes are numbered, each node's
// builder methods in method-id order, and the names and types of everything
// read or written on the wire — identity flags, plain and evaluated
// arguments, return types, deferred block keys, feature flags. Code
// templates are left out: they change what a call does, not what crosses.
//
// The generator writes the value into both sides' generated enums, so two
// halves built from one generation carry the same number and halves from
// different IDLs almost surely do not (ADR-0278 SD6, proposed).
func Fingerprint(tls []NodeI) (fp uint64) {
	h := blake3.New(32, nil)
	for _, tl := range tls {
		switch n := tl.(type) {
		case *BuilderFactoryNode:
			fpLine(h, "factory", n.Name.String(), strconv.FormatBool(n.IdentityArguments.HasId), strconv.FormatBool(n.IdentityArguments.IsReference),
				fmt.Sprintf("%+v", n.Settings), fpTypeName(n.ReturnType))
			fpArguments(h, n.Arguments)
			for _, d := range n.DeferredBlockMaps {
				fpLine(h, "blocks", d.Name)
				for _, k := range d.KeyTypes {
					fpLine(h, "key", k.String())
				}
			}
			for _, m := range n.BuilderMethods {
				fpLine(h, "method", m.Spec.Name.String())
				fpArguments(h, ArgumentSpec{PlainArguments: m.Spec.PlainArguments, EvaluatedArguments: m.Spec.EvaluatedArguments})
			}
		case *ProceduralNode:
			fpLine(h, "procedure", n.Name.String(), strconv.FormatBool(n.IdentityArguments.HasId), strconv.FormatBool(n.IdentityArguments.IsReference),
				fmt.Sprintf("%+v", n.Settings), fpTypeName(n.ReturnType))
			fpArguments(h, n.Arguments)
		case *FetcherNode:
			fpLine(h, "fetcher", n.Name.String())
			fpArguments(h, ArgumentSpec{PlainArguments: n.ReturnTypes})
		default:
			fpLine(h, "node", fmt.Sprintf("%T", tl), tl.GetName().String())
		}
	}
	return binary.BigEndian.Uint64(h.Sum(nil)[:8])
}

func fpArguments(w io.Writer, a ArgumentSpec) {
	for name, t := range a.PlainArguments.Iterate() {
		fpLine(w, "plain", name.String(), t.String())
	}
	for name, t := range a.EvaluatedArguments.Iterate() {
		fpLine(w, "evaluated", name.String(), fpTypeName(t))
	}
}

func fpTypeName(t TypeI) string {
	if t == nil {
		return "-"
	}
	return t.GetName().String()
}

// fpLine writes one record: fields separated by a unit separator, ended by a
// newline, so no field's content can be mistaken for a boundary.
func fpLine(w io.Writer, fields ...string) {
	for i, f := range fields {
		if i > 0 {
			_, _ = io.WriteString(w, "\x1f")
		}
		_, _ = io.WriteString(w, f)
	}
	_, _ = io.WriteString(w, "\n")
}
