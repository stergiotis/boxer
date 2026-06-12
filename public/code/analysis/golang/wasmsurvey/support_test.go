package wasmsurvey

import (
	"testing"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
)

func TestLeafSeed(t *testing.T) {
	cases := []struct {
		name       string
		importPath string
		class      string
		target     TargetID
		wantTier   Tier
		wantKind   ReasonKind
	}{
		{"unsupported stdlib", "os/exec", godep.ClassStdlib, TargetWASI, TierRed, ReasonUnsupportedStdlib},
		{"net is red", "net", godep.ClassStdlib, TargetJS, TierRed, ReasonUnsupportedStdlib},
		{"reflect partial", "reflect", godep.ClassStdlib, TargetWASI, TierYellow, ReasonReflect},
		{"json on reflect subset", "encoding/json", godep.ClassStdlib, TargetWASI, TierYellow, ReasonReflect},
		{"json v2 experiment", "encoding/json/v2", godep.ClassStdlib, TargetWASI, TierYellow, ReasonGoexperimentJSONv2},
		{"unsafe", "unsafe", godep.ClassStdlib, TargetWASI, TierYellow, ReasonUnsafe},
		{"plain stdlib green", "fmt", godep.ClassStdlib, TargetWASI, TierGreen, ReasonNone},
		{"os green on wasi", "os", godep.ClassStdlib, TargetWASI, TierGreen, ReasonNone},
		{"os yellow on wasm-unknown", "os", godep.ClassStdlib, TargetWasmUnknown, TierYellow, ReasonUnsupportedStdlib},
		{"syscall yellow on wasm-unknown", "syscall", godep.ClassStdlib, TargetWasmUnknown, TierYellow, ReasonUnsupportedStdlib},
		{"allowed external", "github.com/rs/zerolog/log", godep.ClassExternal, TargetWASI, TierGreen, ReasonNone},
		{"denied external", "golang.org/x/tools/go/packages", godep.ClassExternal, TargetWASI, TierRed, ReasonUnsupportedExternal},
		{"unknown external", "github.com/foo/bar", godep.ClassExternal, TargetWASI, TierYellow, ReasonUnknownExternal},
		{"internal seeds green", "github.com/stergiotis/boxer/public/x", godep.ClassInternal, TargetWASI, TierGreen, ReasonNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTier, gotKind := leafSeed(tc.importPath, tc.class, tc.target)
			if gotTier != tc.wantTier {
				t.Errorf("tier: got %v, want %v", gotTier, tc.wantTier)
			}
			if gotKind != tc.wantKind {
				t.Errorf("kind: got %v, want %v", gotKind, tc.wantKind)
			}
		})
	}
}

func TestParseTargetAndMode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want TargetID
		ok   bool
	}{
		{"wasi", TargetWASI, true},
		{"js", TargetJS, true},
		{"wasm-unknown", TargetWasmUnknown, true},
		{"bogus", 0, false},
	} {
		got, ok := ParseTargetE(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("ParseTargetE(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	for _, tc := range []struct {
		in   string
		want Mode
		ok   bool
	}{
		{"", ModeBoth, true},
		{"both", ModeBoth, true},
		{"static", ModeStatic, true},
		{"empirical", ModeEmpirical, true},
		{"nope", ModeBoth, false},
	} {
		got, ok := ParseModeE(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseModeE(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestWorstTier(t *testing.T) {
	if worstTier(TierGreen, TierRed) != TierRed {
		t.Error("red should dominate green")
	}
	if worstTier(TierYellow, TierGreen) != TierYellow {
		t.Error("yellow should dominate green")
	}
	if worstTier(TierUnknown, TierGreen) != TierGreen {
		t.Error("unknown must not dominate a real verdict")
	}
}
