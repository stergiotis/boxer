package scene

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// client.go decides which Rust client a scene runs against, and refuses one
// that cannot do what the scene needs (ADR-0248 §SD4).

// NeedRaster is the one client capability a scene can ask for: pixels. A
// mesh-only appliance build passes every tree assertion and then fails the
// capture.
const NeedRaster = "raster"

// meshOnlyMarker is a string only the no-raster build carries.
const meshOnlyMarker = "mesh-only appliance host"

// FindRepoRoot walks up from dir to the checkout that holds the Rust client.
func FindRepoRoot(dir string) (root string, err error) {
	if dir, err = filepath.Abs(dir); err != nil {
		return "", eh.Errorf("unable to resolve the working directory: %w", err)
	}
	for cur := dir; ; cur = filepath.Dir(cur) {
		if st, e := os.Stat(filepath.Join(cur, "rust", "imzero2")); e == nil && st.IsDir() {
			return cur, nil
		}
		if filepath.Dir(cur) == cur {
			return "", eb.Build().Str("from", dir).Errorf("no checkout with rust/imzero2 above the working directory — pass the repository root")
		}
	}
}

func isMeshOnly(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(b, []byte(meshOnlyMarker))
}

// chooseClient picks the client binary. An explicit one is taken as given but
// still checked; otherwise the CPU rasterizer is preferred over the wgpu build,
// since it needs no GPU and both rasterize.
func chooseClient(root string, explicit string, needs []string) (client string, err error) {
	raster := false
	for _, n := range needs {
		switch n {
		case NeedRaster:
			raster = true
		default:
			return "", eb.Build().Str("need", n).Errorf("unknown client capability (known: " + NeedRaster + ")")
		}
	}
	cands := []string{explicit}
	if explicit == "" {
		cands = []string{
			filepath.Join(root, "rust", "imzero2", "target", "headless-soft", "release", "imzero2"),
			filepath.Join(root, "rust", "imzero2", "target", "headless", "release", "imzero2"),
		}
	}
	var rejected []string
	for _, c := range cands {
		st, e := os.Stat(c)
		if e != nil || st.IsDir() {
			rejected = append(rejected, c+": not there")
			continue
		}
		if raster && isMeshOnly(c) {
			rejected = append(rejected, c+": mesh-only, cannot capture")
			continue
		}
		client = c
		break
	}
	if client == "" {
		return "", eb.Build().Str("tried", strings.Join(rejected, "; ")).
			Errorf("no usable headless client — build one with rust/imzero2/build_rust_headless_soft.sh")
	}
	// Always fatal. A client older than the generated interpreter desyncs the
	// FFFI wire on whichever opcode moved, and that reads like an app bug.
	cst, _ := os.Stat(client)
	for _, gen := range []string{"enums_out.rs", "interpreter.rs"} {
		gst, e := os.Stat(filepath.Join(root, "rust", "imzero2", "src", "imzero2", gen))
		if e == nil && gst.ModTime().After(cst.ModTime()) {
			return "", eb.Build().Str("client", client).Str("newer", gen).
				Errorf("the headless client is older than the generated interpreter sources — rebuild it")
		}
	}
	return client, nil
}

// resolveFont asks fontconfig for a family and takes the answer only if it is
// that family: fc-match always answers, with a fallback when it has no match,
// and a scene rendered in a fallback font is a different picture.
func resolveFont(pattern, family string) string {
	out, err := extbin.FcMatch.Output(context.Background(), extbin.Opts{}, "-f", "%{file}\t%{family}\n", pattern)
	if err != nil {
		return ""
	}
	file, fam, ok := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if !ok || !strings.Contains(fam, family) {
		return ""
	}
	if _, e := os.Stat(file); e != nil {
		return ""
	}
	return file
}
