package tabhost

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost/web"
)

// The bundle subcommand (ADR-0278 SD4, proposed): a servable directory built
// from whichever module the binary runs in, so a consumer bundles its own tab
// without boxer's scripts. It writes the Go module, the Rust browser host and
// the fonts; the page, worker and shim are served out of the binary by
// `serve`, and written too only with --withAssets, for a host that is not
// this binary.

// boxerModule is the module that carries the Rust browser host's sources and
// the font resolver.
const boxerModule = "github.com/stergiotis/boxer"

// goBuildFlags and goBuildEnv are what scripts/dev/go-build-env.sh sets for a
// shipped binary (ADR-0215); TestBuildFlagsMatchTheShellEnv keeps the two in
// step. The tags and the toolchain pin are read from the module being built.
var goBuildFlags = []string{"-trimpath", "-buildvcs=auto"}

var goBuildEnv = []string{"CGO_ENABLED=0"}

// fontSlots are the faces the worker fetches from ./fonts/<slot>.ttf, in the
// order font-resolve.sh fills MAIN_FONT, MONO_FONT, PHOSPHOR_FONT and
// FALLBACK_FONT.
var fontSlots = []string{"main", "mono", "phosphor", "fallback"}

func bundleCommand() (cmd *cli.Command) {
	return &cli.Command{
		Name:  "bundle",
		Usage: "build a servable tab bundle from the module this runs in: the Go module as a wasip1 reactor, the Rust browser host, the fonts",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "out", Required: true, Usage: "the bundle directory to write"},
			&cli.StringFlag{Name: "pkg", Usage: "the main package to build for the tab; empty is this binary's own"},
			&cli.StringFlag{Name: "host", Usage: "a prebuilt imzero2_browser.wasm; empty builds it from the boxer module's sources (needs cargo and the wasm32-unknown-unknown target)"},
			&cli.StringFlag{Name: "fonts", Usage: "a directory holding main.ttf, mono.ttf, phosphor.ttf and fallback.ttf (each optional); empty resolves them as boxer's launchers do"},
			&cli.BoolFlag{Name: "withAssets", Usage: "also write the page, worker and shim, for serving the bundle with something other than this binary"},
		},
		Action: bundle,
	}
}

func bundle(ctx *cli.Context) (err error) {
	out := ctx.String("out")
	if err = os.MkdirAll(filepath.Join(out, "fonts"), 0o755); err != nil {
		return eh.Errorf("bundle: output directory: %w", err)
	}
	pkg := ctx.String("pkg")
	if pkg == "" {
		bi, ok := debug.ReadBuildInfo()
		if !ok || bi.Path == "" || bi.Path == "command-line-arguments" {
			return eh.Errorf("bundle: cannot tell this binary's package; pass --pkg")
		}
		pkg = bi.Path
	}
	mainDir, err := packageModuleDir(pkg)
	if err != nil {
		return
	}
	boxerDir, err := moduleDir(boxerModule)
	if err != nil {
		return
	}
	if err = buildGoModule(mainDir, pkg, filepath.Join(out, "imzero2tab.wasm")); err != nil {
		return
	}
	if err = placeHost(ctx.String("host"), boxerDir, mainDir, filepath.Join(out, "imzero2_browser.wasm")); err != nil {
		return
	}
	placeFonts(ctx.String("fonts"), boxerDir, filepath.Join(out, "fonts"))
	if ctx.Bool("withAssets") {
		for name, b := range web.Assets() {
			if err = os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
				return eb.Build().Str("asset", name).Errorf("bundle: write asset: %w", err)
			}
		}
	}
	log.Info().Str("out", out).Str("pkg", pkg).Msg("bundle: written")
	return
}

// moduleDir is the directory of module path as the go command resolves it
// from the working directory.
func moduleDir(path string) (dir string, err error) {
	return goListDir(path, "list", "-m", "-f", "{{.Dir}}", path)
}

// packageModuleDir is the directory of the module that contains pkg: the
// module whose tags and go.mod the build reads. Asking for the package rather
// than for "the main module" keeps the answer single under a go.work, where
// every workspace module is a main module.
func packageModuleDir(pkg string) (dir string, err error) {
	return goListDir(pkg, "list", "-f", "{{.Module.Dir}}", pkg)
}

func goListDir(subject string, args ...string) (dir string, err error) {
	o, err := extbin.Go.Output(context.Background(), extbin.Opts{}, args...)
	if err != nil {
		return "", eb.Build().Str("subject", subject).Errorf("bundle: go list: %w", err)
	}
	dir = strings.TrimSpace(string(o))
	if dir == "" || strings.Contains(dir, "\n") {
		return "", eb.Build().Str("subject", subject).Errorf("bundle: no single module directory (run inside a module that requires boxer)")
	}
	return
}

// buildGoModule builds pkg as a wasip1 reactor with the shipped-binary flags,
// the module's own tags and its go.mod toolchain, and shrinks it with
// wasm-opt when that is installed.
func buildGoModule(mainDir string, pkg string, dst string) (err error) {
	args := append([]string{"build"}, goBuildFlags...)
	args = append(args, "-buildmode=c-shared", "-ldflags=-s -w", "-tags", readTags(mainDir), "-o", dst, pkg)
	env := append(append(os.Environ(), goBuildEnv...), "GOOS=wasip1", "GOARCH=wasm") //boxer:lint disable=CS011 reason="forwards the ambient process environment into the go build of the tab module"
	if !hasVar(env, "GOTOOLCHAIN") {
		if v := goModVersion(mainDir); v != "" {
			env = append(env, "GOTOOLCHAIN=go"+v)
		}
	}
	cmd, err := extbin.Go.Command(context.Background(), extbin.Opts{Dir: mainDir, Env: env}, args...)
	if err != nil {
		return eh.Errorf("bundle: go: %w", err)
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	log.Info().Str("pkg", pkg).Msg("bundle: the Go tab host (wasip1 reactor)")
	if err = cmd.Run(); err != nil {
		return eb.Build().Str("pkg", pkg).Errorf("bundle: go build: %w", err)
	}
	if _, available := extbin.WasmOpt.Resolve(); available {
		opt := dst + ".opt"
		if woErr := extbin.WasmOpt.Run(context.Background(), extbin.Opts{}, "-Oz", "--enable-bulk-memory", "--enable-sign-ext", "--enable-mutable-globals", "--enable-nontrapping-float-to-int", dst, "-o", opt); woErr != nil {
			log.Warn().Err(woErr).Msg("bundle: wasm-opt failed; the unoptimised module stands")
			_ = os.Remove(opt)
		} else if err = os.Rename(opt, dst); err != nil {
			return eh.Errorf("bundle: wasm-opt output: %w", err)
		}
	}
	return
}

// placeHost copies a prebuilt browser host, or builds one from the boxer
// module's sources. Building inside boxer itself keeps the script's own
// target directory; from another module, boxer's directory is the read-only
// module cache, so the build goes to the user's cache. A target directory the
// caller set in IMZERO2_BROWSER_TARGET_DIR wins over both.
func placeHost(prebuilt string, boxerDir string, mainDir string, dst string) (err error) {
	if prebuilt != "" {
		return copyFile(prebuilt, dst)
	}
	script := filepath.Join(boxerDir, "rust", "imzero2", "build_rust_browser.sh")
	targetDir := filepath.Join(boxerDir, "rust", "imzero2", "target", "browser")
	env := os.Environ() //boxer:lint disable=CS011 reason="forwards the ambient process environment into the cargo build of the browser host"
	if d := BrowserTargetDir.Get(); d != "" {
		targetDir = d
	} else if filepath.Clean(boxerDir) != filepath.Clean(mainDir) {
		cache, cErr := os.UserCacheDir()
		if cErr != nil {
			return eh.Errorf("bundle: no cache directory for the Rust build: %w", cErr)
		}
		targetDir = filepath.Join(cache, "boxer", "tabhost", "rust-target")
		env = append(env, "IMZERO2_BROWSER_TARGET_DIR="+targetDir)
	}
	cmd, err := extbin.Bash.Command(context.Background(), extbin.Opts{Env: env}, script)
	if err != nil {
		return eh.Errorf("bundle: bash: %w", err)
	}
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	log.Info().Str("targetDir", targetDir).Msg("bundle: the Rust browser host (wasm32 cdylib), from source")
	if err = cmd.Run(); err != nil {
		return eh.Errorf("bundle: build the browser host (or pass --host): %w", err)
	}
	return copyFile(filepath.Join(targetDir, "wasm32-unknown-unknown", "release", "imzero2_browser.wasm"), dst)
}

// placeFonts copies the four faces from dir, or from what boxer's resolver
// finds. A face that is missing is left to egui's default and said so.
func placeFonts(dir string, boxerDir string, dst string) {
	paths := make([]string, len(fontSlots))
	if dir != "" {
		for i, slot := range fontSlots {
			paths[i] = filepath.Join(dir, slot+".ttf")
		}
	} else {
		lib := filepath.Join(boxerDir, "rust", "imzero2", "font-resolve.sh")
		o, err := extbin.Bash.Output(context.Background(), extbin.Opts{}, "-c", `source "$1" && imzero2_resolve_fonts >/dev/null 2>&1; printf '%s\n' "$MAIN_FONT" "$MONO_FONT" "$PHOSPHOR_FONT" "$FALLBACK_FONT"`, "bash", lib)
		if err != nil {
			log.Warn().Err(err).Msg("bundle: the font resolver failed; egui's default faces")
			return
		}
		sc := bufio.NewScanner(bytes.NewReader(o))
		for i := 0; i < len(fontSlots) && sc.Scan(); i++ {
			paths[i] = strings.TrimSpace(sc.Text())
		}
	}
	for i, slot := range fontSlots {
		if paths[i] == "" {
			log.Warn().Str("slot", slot).Msg("bundle: no font; egui's default face")
			continue
		}
		if err := copyFile(paths[i], filepath.Join(dst, slot+".ttf")); err != nil {
			log.Warn().Err(err).Str("slot", slot).Msg("bundle: font not copied; egui's default face")
		}
	}
}

// hasVar reports whether env sets name to a non-empty value. GOTOOLCHAIN is
// the go command's own variable, not a boxer setting: a toolchain the caller
// chose is honoured, as go-build-env.sh does, by looking at what is forwarded.
func hasVar(env []string, name string) (yes bool) {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok && v != "" {
			yes = true
		}
	}
	return
}

// readTags is the module's ./tags, newline-stripped, as go-build-env.sh reads
// it; empty when the module has none.
func readTags(dir string) (tags string) {
	b, err := os.ReadFile(filepath.Join(dir, "tags"))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", "")
}

// goModVersion is the `go` directive of the module's go.mod.
func goModVersion(dir string) (v string) {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	return ""
}

func copyFile(src string, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return eb.Build().Str("src", src).Errorf("bundle: copy: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return eb.Build().Str("dst", dst).Errorf("bundle: copy: %w", err)
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return eb.Build().Str("dst", dst).Errorf("bundle: copy: %w", err)
	}
	return out.Close()
}
