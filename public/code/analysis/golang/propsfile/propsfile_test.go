package propsfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/packageprops"
)

// TestCapabilityTokenRoundTrip pins the derived identifier vocabulary: every
// capability must render to a Go identifier and parse back to itself. It is the
// guard that lets CapabilityToken derive from String() instead of a hand-written
// table — if a future capability's token breaks the derivation, this fails
// rather than silently generating an identifier that does not compile.
func TestCapabilityTokenRoundTrip(t *testing.T) {
	for _, c := range packageprops.AllCapabilities() {
		tok := CapabilityToken(c)
		if !strings.HasPrefix(tok, "Capability") {
			t.Errorf("%v: token %q lacks the Capability prefix", c, tok)
		}
		if strings.ContainsAny(tok, "-_ ") {
			t.Errorf("%v: token %q is not a Go identifier", c, tok)
		}
		got, ok := ParseCapabilityToken(tok)
		if !ok || got != c {
			t.Errorf("%v: round trip via %q gave (%v, %v)", c, tok, got, ok)
		}
	}
	if _, ok := ParseCapabilityToken("CapabilityNotAThing"); ok {
		t.Error("unknown token parsed as known")
	}
}

// TestKindTokenRoundTrip pins the curated Kind vocabulary, which unlike the
// capability tokens is switched over by hand.
func TestKindTokenRoundTrip(t *testing.T) {
	for _, k := range []packageprops.Kind{
		packageprops.KindUnspecified,
		packageprops.KindDemo,
		packageprops.KindExample,
		packageprops.KindIntegrationTest,
	} {
		if got := ParseKindToken(KindToken(k)); got != k {
			t.Errorf("round-trip %v: token %q parsed back as %v", k, KindToken(k), got)
		}
	}
	if got := ParseKindToken("bogus"); got != packageprops.KindUnspecified {
		t.Errorf("unknown token should parse to KindUnspecified, got %v", got)
	}
}

// TestStateTokenRoundTrip does the same for the wasm state vocabulary.
func TestStateTokenRoundTrip(t *testing.T) {
	for _, s := range []packageprops.WASMState{
		packageprops.WASMUnknown,
		packageprops.WASMCompiles,
		packageprops.WASMBlocked,
	} {
		if got := ParseStateToken(StateToken(s)); got != s {
			t.Errorf("round-trip %v: token %q parsed back as %v", s, StateToken(s), got)
		}
	}
	if got := ParseStateToken("bogus"); got != packageprops.WASMUnknown {
		t.Errorf("unknown token should parse to WASMUnknown, got %v", got)
	}
}

// TestCapabilityTokenSpelling pins a few identifiers exactly, so the derivation
// cannot quietly start emitting a different spelling than the vocabulary
// declares.
func TestCapabilityTokenSpelling(t *testing.T) {
	for _, tc := range []struct {
		c    packageprops.Capability
		want string
	}{
		{packageprops.CapabilitySafe, "CapabilitySafe"},
		{packageprops.CapabilityExec, "CapabilityExec"},
		{packageprops.CapabilityCgo, "CapabilityCgo"},
		{packageprops.CapabilityReadSystemState, "CapabilityReadSystemState"},
		{packageprops.CapabilityArbitraryExecution, "CapabilityArbitraryExecution"},
		{packageprops.CapabilityUnsafePointer, "CapabilityUnsafePointer"},
	} {
		if got := CapabilityToken(tc.c); got != tc.want {
			t.Errorf("CapabilityToken(%v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

// TestRenderParseRoundTrip is the core contract: whatever Render writes, Parse
// reads back identically. Generation depends on it — a regeneration reads the
// existing file to preserve the fields it does not own.
func TestRenderParseRoundTrip(t *testing.T) {
	for name, want := range map[string]packageprops.Props{
		"zero": {},
		"wasm only": {
			WASMWASI:         packageprops.WASMCompiles,
			WASMJS:           packageprops.WASMBlocked,
			WASMFreestanding: packageprops.WASMUnknown,
		},
		"curated kind": {
			WASMWASI: packageprops.WASMCompiles,
			Kind:     packageprops.KindIntegrationTest,
		},
		"caps safe": {
			CapsDirect:    packageprops.Caps(packageprops.CapabilitySafe),
			CapsReachable: packageprops.Caps(packageprops.CapabilitySafe),
		},
		"caps rich": {
			WASMWASI:      packageprops.WASMBlocked,
			Kind:          packageprops.KindExample,
			CapsDirect:    packageprops.Caps(packageprops.CapabilityExec, packageprops.CapabilityFiles),
			CapsReachable: packageprops.Caps(packageprops.CapabilityNetwork, packageprops.CapabilityReflect, packageprops.CapabilityCgo),
		},
		"every capability": {
			CapsDirect: packageprops.Caps(packageprops.AllCapabilities()...),
		},
	} {
		t.Run(name, func(t *testing.T) {
			src, err := Render("mypkg", "example.com/m/mypkg", want)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			path := filepath.Join(t.TempDir(), FileName)
			if err := os.WriteFile(path, src, 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := Parse(path)
			if err != nil {
				t.Fatalf("parse: %v\n--- source ---\n%s", err, src)
			}
			if got != want {
				t.Errorf("round trip mismatch\n got: %+v\nwant: %+v\n--- source ---\n%s", got, want, src)
			}
		})
	}
}

// TestRenderOmitsZeroCaps keeps an unsurveyed declaration terse: an absent field
// and a zero field mean the same thing, so the zero one is not written.
func TestRenderOmitsZeroCaps(t *testing.T) {
	src, err := Render("mypkg", "example.com/m/mypkg", packageprops.Props{WASMWASI: packageprops.WASMCompiles})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(src), "CapsDirect") || strings.Contains(string(src), "CapsReachable") {
		t.Errorf("zero caps should be omitted, got:\n%s", src)
	}
}

// TestMergePreservesUnownedFields is ADR-0120 SD7: a survey overwrites only the
// fields it computes, and everything else survives. This is what lets the wasm
// and capability surveys write the same file without clobbering each other, and
// what keeps a curated Kind alive across a re-seed.
func TestMergePreservesUnownedFields(t *testing.T) {
	base := packageprops.Props{
		WASMWASI:      packageprops.WASMCompiles,
		WASMJS:        packageprops.WASMCompiles,
		Kind:          packageprops.KindIntegrationTest,
		CapsDirect:    packageprops.Caps(packageprops.CapabilityExec),
		CapsReachable: packageprops.Caps(packageprops.CapabilityNetwork),
	}

	// The wasm survey owns only the WASM* fields.
	gotWASM := Merge(base, packageprops.Props{
		WASMWASI:   packageprops.WASMBlocked,
		WASMJS:     packageprops.WASMBlocked,
		Kind:       packageprops.KindDemo,                          // must be ignored
		CapsDirect: packageprops.Caps(packageprops.CapabilitySafe), // must be ignored
	}, FieldsWASM)
	if gotWASM.WASMWASI != packageprops.WASMBlocked || gotWASM.WASMJS != packageprops.WASMBlocked {
		t.Errorf("wasm survey failed to write its own fields: %+v", gotWASM)
	}
	if gotWASM.Kind != packageprops.KindIntegrationTest {
		t.Errorf("wasm survey clobbered curated Kind: %v", gotWASM.Kind)
	}
	if gotWASM.CapsDirect != packageprops.Caps(packageprops.CapabilityExec) {
		t.Errorf("wasm survey clobbered the capability verdict: %v", gotWASM.CapsDirect)
	}

	// The capability survey owns only the Caps* fields.
	gotCaps := Merge(base, packageprops.Props{
		WASMWASI:      packageprops.WASMUnknown, // must be ignored
		CapsDirect:    packageprops.Caps(packageprops.CapabilitySafe),
		CapsReachable: packageprops.Caps(packageprops.CapabilityFiles),
	}, FieldsCaps)
	if gotCaps.CapsDirect != packageprops.Caps(packageprops.CapabilitySafe) {
		t.Errorf("caps survey failed to write its own field: %v", gotCaps.CapsDirect)
	}
	if gotCaps.WASMWASI != packageprops.WASMCompiles {
		t.Errorf("caps survey clobbered the wasm verdict: %v", gotCaps.WASMWASI)
	}
	if gotCaps.Kind != packageprops.KindIntegrationTest {
		t.Errorf("caps survey clobbered curated Kind: %v", gotCaps.Kind)
	}
}

// TestParseTolerance covers the degrade-rather-than-error rule: a declaration
// this parser cannot read yields zero values, which assert nothing.
func TestParseTolerance(t *testing.T) {
	for name, src := range map[string]string{
		"unknown tokens":  "package p\nvar PackageProps = struct{}{}\n",
		"caps not a call": "package p\nimport \"x\"\nvar PackageProps = x.Props{CapsDirect: 7}\n",
		"caps wrong func": "package p\nimport \"x\"\nvar PackageProps = x.Props{CapsDirect: x.Other(x.CapabilityExec)}\n",
		"no declaration":  "package p\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := Parse(path)
			if err != nil {
				t.Fatalf("parse should tolerate this, got: %v", err)
			}
			if got != (packageprops.Props{}) {
				t.Errorf("unreadable declaration should assert nothing, got %+v", got)
			}
		})
	}
}
