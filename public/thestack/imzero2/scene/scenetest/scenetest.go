// Package scenetest launches scenes from Go tests (ADR-0248 §SD5): the scenes
// whose oracle is a computation, which live in the integration lane beside the
// widget they check.
//
// A test is not the host binary, so unlike `imzero2 scene` it has to build
// one. That happens once per test process, into the per-checkout launcher
// cache, and Go's build cache makes every run after the first a relink.
package scenetest

import (
	"context"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
)

// Host is the imzero2 host a test launches: the main package that links the
// apps under test. The zero Host is boxer's own.
type Host struct {
	// Package is the host's main package as `go build` takes it, relative to
	// ModuleDir. Empty means boxer's ./public/thestack/cmd/imzero2/.
	Package string
	// ModuleDir is where the host is built and the directory it runs in, so
	// apps that read checkout-relative paths find them. Empty means ClientRoot.
	ModuleDir string
	// ClientRoot is the checkout holding rust/imzero2 — the headless client
	// and the fonts. Empty means found by walking up from the test's directory.
	ClientRoot string
	// Tags are the build tags, comma-separated; binary_log is always added.
	// Empty means the contents of ModuleDir/tags, when that file exists.
	Tags string
}

type builtHost struct {
	once sync.Once
	host Host
	path string
	err  error
	out  []byte
}

var (
	builtMu sync.Mutex
	built   = map[Host]*builtHost{}
)

// resolve fills in a Host's defaults.
func (inst Host) resolve() (h Host, err error) {
	h = inst
	if h.ClientRoot == "" {
		if h.ClientRoot, err = scene.FindRepoRoot("."); err != nil {
			return h, err
		}
	}
	if h.ModuleDir == "" {
		h.ModuleDir = h.ClientRoot
	}
	if h.Package == "" {
		h.Package = "./public/thestack/cmd/imzero2/"
	}
	if h.Tags == "" {
		tags, _ := os.ReadFile(filepath.Join(h.ModuleDir, "tags"))
		h.Tags = strings.TrimSpace(string(tags))
	}
	return h, nil
}

func (inst *builtHost) build() {
	h := inst.host
	cache, err := os.UserCacheDir()
	if err != nil {
		inst.err = err
		return
	}
	key := h.ModuleDir + "\x00" + h.Package + "\x00" + h.Tags
	dir := filepath.Join(cache, "boxer-launcher",
		"imzero2-scenetest-"+strconv.FormatUint(uint64(crc32.ChecksumIEEE([]byte(key))), 10))
	if inst.err = os.MkdirAll(dir, 0o755); inst.err != nil {
		return
	}
	tagList := h.Tags
	if tagList != "" {
		tagList += ","
	}
	tagList += "binary_log"
	// Built under a unique name and renamed into place, so a concurrent test
	// process never launches a half-written binary.
	tmp := filepath.Join(dir, "build."+strconv.Itoa(os.Getpid()))
	env := append(os.Environ(), "CGO_ENABLED=0") //boxer:lint disable=CS011 reason="forwards the ambient process environment into the go build of the host"
	inst.out, inst.err = extbin.Go.CombinedOutput(context.Background(), extbin.Opts{Dir: h.ModuleDir, Env: env},
		"build", "-tags", tagList, "-o", tmp, h.Package)
	if inst.err != nil {
		_ = os.Remove(tmp)
		return
	}
	inst.path = filepath.Join(dir, "app")
	inst.err = os.Rename(tmp, inst.path)
}

// buildHost builds each distinct host once per test process.
func buildHost(host Host) (b *builtHost, err error) {
	h, err := host.resolve()
	if err != nil {
		return nil, err
	}
	builtMu.Lock()
	b = built[h]
	if b == nil {
		b = &builtHost{host: h}
		built[h] = b
	}
	builtMu.Unlock()
	b.once.Do(b.build)
	return b, nil
}

// Launch starts a scene on boxer's own host for a test and tears it down with
// the test. It skips the test when the machine has no headless client to run
// against — that says something about the machine, not about the widget.
func Launch(t testing.TB, spec scene.Spec) (s *scene.Session) {
	t.Helper()
	return LaunchOn(t, Host{}, spec)
}

// LaunchOn is Launch on a host of the caller's choosing: a downstream module's
// own main package, which links apps boxer's host does not.
func LaunchOn(t testing.TB, host Host, spec scene.Spec) (s *scene.Session) {
	t.Helper()
	b, err := buildHost(host)
	if err != nil {
		t.Fatal(err)
	}
	if b.err != nil {
		t.Fatalf("unable to build the imzero2 host: %v\n%s", b.err, b.out)
	}
	for _, name := range spec.Requires {
		unmet, err := scene.CheckRequire(name)
		if err != nil {
			t.Fatal(err)
		}
		if unmet != "" {
			t.Skip(unmet)
		}
	}
	out := t.TempDir()
	s, err = scene.Launch(spec, scene.Options{
		Name:       strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()),
		OutDir:     out,
		HostBinary: b.path,
		RepoRoot:   b.host.ClientRoot,
		HostDir:    b.host.ModuleDir,
		Timeout:    90 * time.Second,
		SettleMs:   300,
		Out:        os.Stderr,
		Logger:     zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel),
	})
	if err != nil {
		if strings.Contains(err.Error(), "no usable headless client") {
			t.Skip(err.Error())
		}
		t.Fatalf("unable to launch the scene: %s", scene.PlainError(err))
	}
	t.Cleanup(func() {
		_ = s.Close()
		if t.Failed() {
			if b, e := os.ReadFile(filepath.Join(out, "logs", s.Name()+".host.log")); e == nil {
				t.Logf("host log:\n%s", b)
			}
		}
	})
	return s
}

// Run executes a trace fragment and fails the test with the rendered error —
// fields included, since what a step read is one of them.
func Run(t testing.TB, s *scene.Session, trace string) {
	t.Helper()
	if err := s.RunJSONL(trace); err != nil {
		t.Fatalf("scene step failed:\n%s", scene.PlainError(err))
	}
}

// Number returns a name a `read` step bound, as a number.
func Number(t testing.TB, s *scene.Session, name string) float64 {
	t.Helper()
	v, err := s.Vars.Number(name)
	if err != nil {
		t.Fatal(scene.PlainError(err))
	}
	return v
}
