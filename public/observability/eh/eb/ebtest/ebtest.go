// Package ebtest reads back the structured payload an eb.Build() chain
// attaches to an error, so a test can assert on a field instead of on the
// message text.
//
// It exists because CS013 moves values out of error messages and onto the
// builder, which silently invalidates every test that asserted the value
// appeared in Error(). Without a way to read a field back, such a test has no
// replacement assertion and the call site cannot be fixed at all.
//
// This is test-support code, not a general reader: it takes a *testing.T and
// fails the test rather than returning an error, and it depends on an
// independent CBOR implementation so that a bug in boxer's own encoder cannot
// hide behind a matching bug in its decoder. A production consumer that needs
// the same data should read eh.WalkStreams's Data facts instead.
package ebtest

import (
	"testing"

	fxcbor "github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
)

var decMode = func() fxcbor.DecMode {
	m, err := fxcbor.DecOptions{
		DefaultMapType: nil,
		UTF8:           fxcbor.UTF8DecodeInvalid,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	return m
}()

// Fields decodes the structured payload carried by err and by every error it
// wraps, and returns them as one map.
//
// The chain is walked outermost first and an already-seen key is not
// overwritten, so where two wrapping levels use the same key the value belongs
// to the outer one — the error the caller is actually holding. Use FieldsByLevel
// when that collision is the thing under test.
//
// Fails the test if err is nil, if no error in the chain carries a payload, or
// if a payload does not decode.
func Fields(t *testing.T, err error) (fields map[string]any) {
	t.Helper()
	levels := FieldsByLevel(t, err)
	fields = make(map[string]any, len(levels)*4)
	for _, lvl := range levels {
		for k, v := range lvl {
			if _, seen := fields[k]; seen {
				continue
			}
			fields[k] = v
		}
	}
	return
}

// FieldsByLevel decodes one map per error in the chain that carries a payload,
// outermost first. An error without structured data contributes no entry, so
// the result is not positionally aligned with the wrap chain.
func FieldsByLevel(t *testing.T, err error) (levels []map[string]any) {
	t.Helper()
	if err == nil {
		t.Fatal("ebtest: error is nil")
		return
	}
	for _, data := range payloads(err) {
		levels = append(levels, decodeMap(t, data))
	}
	if len(levels) == 0 {
		t.Fatalf("ebtest: no error in the chain carries structured data (outermost is %T); "+
			"was it built with eb.Build() rather than eh.Errorf?", err)
	}
	return
}

// payloads collects the non-empty CBOR payloads in the wrap chain, outermost
// first, descending into both Unwrap() error and Unwrap() []error.
func payloads(err error) (out [][]byte) {
	seen := make(map[error]struct{}, 8)
	var walk func(e error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if _, dup := seen[e]; dup {
			return
		}
		seen[e] = struct{}{}
		if esd, ok := e.(eh.ErrorWithStructuredDataI); ok {
			if p := esd.GetCBORStructuredData(); len(p) > 0 {
				out = append(out, p)
			}
		}
		switch u := e.(type) {
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		case interface{ Unwrap() []error }:
			for _, inner := range u.Unwrap() {
				walk(inner)
			}
		}
	}
	walk(err)
	return
}

// decodeMap turns one payload into a string-keyed map, failing the test on
// malformed bytes or a non-string key. eb only ever writes string keys, so a
// non-string key means the payload is not what it claims to be.
func decodeMap(t *testing.T, data []byte) (out map[string]any) {
	t.Helper()
	if cerr := fxcbor.Wellformed(data); cerr != nil {
		t.Fatalf("ebtest: payload is not well-formed CBOR: %v\nbytes: % x", cerr, data)
		return
	}
	var raw map[any]any
	if derr := decMode.Unmarshal(data, &raw); derr != nil {
		t.Fatalf("ebtest: payload does not decode: %v\nbytes: % x", derr, data)
		return
	}
	out = make(map[string]any, len(raw))
	for k, v := range raw {
		ks, ok := k.(string)
		if !ok {
			t.Fatalf("ebtest: payload carries a non-string key %#v", k)
			return
		}
		out[ks] = v
	}
	return
}
