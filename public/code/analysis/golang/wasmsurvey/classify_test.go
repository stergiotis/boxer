package wasmsurvey

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
)

// mkNode builds a PackageNode with an explicit id and import edges (by id).
func mkNode(id uint64, path string, class string, imports ...uint64) godep.PackageNode {
	return godep.PackageNode{Id: id, ImportPath: path, Class: class, Imports: imports}
}

// fixtureClosure builds a small hand-wired graph exercising every tier:
//
//	app(1,int) → mid(2,int) → net/smtp(3,std,RED)
//	pure(4,int) → fmt(5,std,green)
//	refl(6,int) → reflect(7,std,YELLOW)
//	denied(8,int) → x/tools(9,ext,RED)
//	unk(10,int) → github.com/foo/bar(11,ext,YELLOW)
func fixtureClosure(target TargetID) (tc targetClosure) {
	nodes := []godep.PackageNode{
		mkNode(1, "example.com/app", godep.ClassInternal, 2),
		mkNode(2, "example.com/mid", godep.ClassInternal, 3),
		mkNode(3, "net/smtp", godep.ClassStdlib),
		mkNode(4, "example.com/pure", godep.ClassInternal, 5),
		mkNode(5, "fmt", godep.ClassStdlib),
		mkNode(6, "example.com/refl", godep.ClassInternal, 7),
		mkNode(7, "reflect", godep.ClassStdlib),
		mkNode(8, "example.com/denied", godep.ClassInternal, 9),
		mkNode(9, "golang.org/x/tools/go/packages", godep.ClassExternal),
		mkNode(10, "example.com/unk", godep.ClassInternal, 11),
		mkNode(11, "github.com/foo/bar", godep.ClassExternal),
	}
	tc.target = target
	tc.manifest = godep.Manifest{Packages: nodes}
	tc.index = tc.manifest.BuildIndex()
	return
}

func internalOnly(p *godep.PackageNode) bool { return p.Class == godep.ClassInternal }

func TestClassifyStatic_Tiers(t *testing.T) {
	tc := fixtureClosure(TargetWASI)
	v := classifyStatic(tc, internalOnly, nil)

	want := map[uint64]Tier{
		1:  TierRed,    // app → mid → net/smtp
		2:  TierRed,    // mid → net/smtp
		4:  TierGreen,  // pure → fmt
		6:  TierYellow, // refl → reflect
		8:  TierRed,    // denied → x/tools
		10: TierYellow, // unk → unknown external
	}
	for id, wantTier := range want {
		got, ok := v[id]
		if !ok {
			t.Errorf("id %d: missing verdict", id)
			continue
		}
		if got.Static != wantTier {
			t.Errorf("id %d (%s): got %v, want %v", id, idPath(tc, id), got.Static, wantTier)
		}
	}
	// Stdlib/external leaves must not receive a reported verdict under internalOnly.
	if _, ok := v[3]; ok {
		t.Error("net/smtp (stdlib) should be filtered out of reported verdicts")
	}
}

func TestClassifyStatic_Blame(t *testing.T) {
	tc := fixtureClosure(TargetWASI)
	v := classifyStatic(tc, internalOnly, nil)

	// app is Red and the blame must be the shortest path to net/smtp.
	app := v[1]
	if len(app.Reasons) == 0 {
		t.Fatal("app: expected a blame reason")
	}
	r := app.Reasons[0]
	if r.Kind != ReasonUnsupportedStdlib || r.Leaf != "net/smtp" {
		t.Errorf("app blame: got kind=%v leaf=%q, want unsupported-stdlib/net/smtp", r.Kind, r.Leaf)
	}
	wantPath := "example.com/app→example.com/mid→net/smtp"
	if got := strings.Join(r.Path, "→"); got != wantPath {
		t.Errorf("app blame path: got %q, want %q", got, wantPath)
	}

	// Green package carries no blame.
	if len(v[4].Reasons) != 0 {
		t.Errorf("pure: expected no reasons, got %v", v[4].Reasons)
	}

	// Yellow unknown-external names the external leaf.
	unk := v[10]
	if len(unk.Reasons) == 0 || unk.Reasons[0].Kind != ReasonUnknownExternal || unk.Reasons[0].Leaf != "github.com/foo/bar" {
		t.Errorf("unk blame: got %v, want unknown-external/github.com/foo/bar", unk.Reasons)
	}
}

func TestClassifyStatic_WasmUnknownStricter(t *testing.T) {
	// A package whose only "blocker" is importing os is Green on wasi but
	// Yellow on wasm-unknown (no host).
	nodes := []godep.PackageNode{
		mkNode(1, "example.com/usesos", godep.ClassInternal, 2),
		mkNode(2, "os", godep.ClassStdlib),
	}
	mk := func(target TargetID) Tier {
		tc := targetClosure{target: target, manifest: godep.Manifest{Packages: nodes}}
		tc.index = tc.manifest.BuildIndex()
		return classifyStatic(tc, internalOnly, nil)[1].Static
	}
	if got := mk(TargetWASI); got != TierGreen {
		t.Errorf("usesos on wasi: got %v, want green", got)
	}
	if got := mk(TargetWasmUnknown); got != TierYellow {
		t.Errorf("usesos on wasm-unknown: got %v, want yellow", got)
	}
}

func TestClassifyStatic_AssumeClean(t *testing.T) {
	tc := fixtureClosure(TargetWASI)
	// Baseline: app(1) is Red through mid(2)→net/smtp(3).
	if got := classifyStatic(tc, internalOnly, nil)[1].Static; got != TierRed {
		t.Fatalf("baseline: app should be red, got %v", got)
	}
	// Counterfactual: assume mid is clean → app no longer reaches net/smtp
	// through it → Green, and mid itself is reported Green.
	v := classifyStatic(tc, internalOnly, []string{"example.com/mid"})
	if got := v[1].Static; got != TierGreen {
		t.Errorf("app with mid assumed-clean: got %v, want green", got)
	}
	if len(v[1].Reasons) != 0 {
		t.Errorf("app should carry no blame when its only path is cleaned: %v", v[1].Reasons)
	}
	if got := v[2].Static; got != TierGreen {
		t.Errorf("mid (clean sink): got %v, want green", got)
	}
	// A package blocked by an *independent* path stays Red: denied(8)→x/tools(9)
	// is untouched by cleaning mid.
	if got := v[8].Static; got != TierRed {
		t.Errorf("denied still red (independent blocker): got %v, want red", got)
	}
}

func idPath(tc targetClosure, id uint64) string {
	if n, ok := tc.index.Node(id); ok {
		return n.ImportPath
	}
	return "?"
}
