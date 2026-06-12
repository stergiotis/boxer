package wasmsurvey

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
)

func TestClassifyProbeOutput(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want ReasonKind
	}{
		{"missing stdlib", "package os/exec is not in std (...)", ReasonUnsupportedStdlib},
		{"could not import", "main.go:3:8: could not import net (...)", ReasonUnsupportedStdlib},
		{"reflect", "panic: unimplemented: (reflect.Value).Method", ReasonReflect},
		{"unsafe", "error: unsafe.Pointer conversion not supported", ReasonUnsafe},
		{"goexperiment", "GOEXPERIMENT=jsonv2 is not supported by this toolchain", ReasonGoexperimentJSONv2},
		{"toolchain ceiling", "requires go version 1.19 through 1.25, got go1.26", ReasonToolchain},
		{"linker", "wasm-ld: error: undefined symbol: foo", ReasonLinker},
		{"linkname", "//go:linkname requires a definition", ReasonLinker},
		{"cgo", "cgo: C source files not allowed", ReasonCgo},
		{"syscall", "syscall.Mmap not implemented on wasm", ReasonSyscall},
		{"other", "some unfamiliar internal compiler error", ReasonProbeOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyProbeOutput(tc.out); got != tc.want {
				t.Errorf("classifyProbeOutput(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

func TestFirstMeaningfulLine(t *testing.T) {
	out := "# command-line-arguments\n\n   ./main.go:3: could not import net   \n"
	if got := firstMeaningfulLine(out); got != "./main.go:3: could not import net" {
		t.Errorf("got %q", got)
	}
	if got := firstMeaningfulLine(strings.Repeat("x", 300)); len(got) > 205 {
		t.Errorf("line not capped: len=%d", len(got))
	}
}

func TestWriteProbeMain(t *testing.T) {
	dir := t.TempDir()

	// Blank-import form when no exported funcs.
	if err := writeProbeMainE(dir, "example.com/pkg", nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(data), `import _ "example.com/pkg"`) {
		t.Errorf("blank-import form missing:\n%s", data)
	}

	// Exported-func reference form.
	if err := writeProbeMainE(dir, "example.com/pkg", []string{"Foo", "Bar"}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	s := string(data)
	if !strings.Contains(s, `import probe "example.com/pkg"`) || !strings.Contains(s, "probe.Foo,") || !strings.Contains(s, "probe.Bar,") {
		t.Errorf("func-ref form missing bindings:\n%s", s)
	}
}

func TestExportedFuncs_OwnPackage(t *testing.T) {
	// Enumerate this package's own exported funcs (the test runs in the
	// package dir). NewCliCommand and Run are exported; runWasmSurvey is not.
	names, err := exportedFuncs(".", "wasip1", nil)
	if err != nil {
		t.Fatal(err)
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	if !set["NewCliCommand"] || !set["Run"] {
		t.Errorf("expected NewCliCommand and Run among exports, got %v", names)
	}
	if set["runWasmSurvey"] {
		t.Error("unexported runWasmSurvey must not be listed")
	}
}

// TestProbeBuild_Smoke exercises the real `tinygo build` path. It is skipped
// when tinygo is absent, so the suite stays green without the toolchain.
func TestProbeBuild_Smoke(t *testing.T) {
	if !tinygoAvailable() {
		t.Skip("tinygo not on PATH; skipping empirical probe smoke")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, ok := godepcollect.ModuleRoot(wd)
	if !ok {
		t.Skip("module root not found")
	}
	// tdigest is a small leaf; whatever its verdict, the probe must produce a
	// real tier (Green or Red) without a harness error. Probe with the repo's
	// load-bearing tags, as the real survey does — without them, tag-gated
	// deps trip "build constraints exclude all Go files" (an inconclusive
	// harness error, not a verdict).
	tags := readTagsFile(filepath.Join(root, "tags"))
	pkg := "github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	dir := filepath.Join(root, "public/analytics/stats/tdigest")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tier, reason, millis, perr := probePackageE(ctx, root, pkg, dir, TargetWASI, tags, 3*time.Minute)
	if perr != nil {
		t.Fatalf("probe harness error: %v", perr)
	}
	if tier != TierGreen && tier != TierRed {
		t.Fatalf("expected a real verdict, got %v", tier)
	}
	t.Logf("tdigest wasi probe: tier=%v reason=%v build=%dms", tier, reason.Kind, millis)
}
