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

var (
	hostOnce sync.Once
	hostPath string
	hostRoot string
	hostErr  error
	hostOut  []byte
)

func buildHost() {
	if hostRoot, hostErr = scene.FindRepoRoot("."); hostErr != nil {
		return
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		hostErr = err
		return
	}
	dir := filepath.Join(cache, "boxer-launcher",
		"imzero2-scenetest-"+strconv.FormatUint(uint64(crc32.ChecksumIEEE([]byte(hostRoot))), 10))
	if hostErr = os.MkdirAll(dir, 0o755); hostErr != nil {
		return
	}
	tags, _ := os.ReadFile(filepath.Join(hostRoot, "tags"))
	tagList := strings.TrimSpace(string(tags))
	if tagList != "" {
		tagList += ","
	}
	tagList += "binary_log"
	// Built under a unique name and renamed into place, so a concurrent test
	// process never launches a half-written binary.
	tmp := filepath.Join(dir, "build."+strconv.Itoa(os.Getpid()))
	env := append(os.Environ(), "CGO_ENABLED=0") //boxer:lint disable=CS011 reason="forwards the ambient process environment into the go build of the host"
	hostOut, hostErr = extbin.Go.CombinedOutput(context.Background(), extbin.Opts{Dir: hostRoot, Env: env},
		"build", "-tags", tagList, "-o", tmp, "./public/thestack/cmd/imzero2/")
	if hostErr != nil {
		_ = os.Remove(tmp)
		return
	}
	hostPath = filepath.Join(dir, "app")
	hostErr = os.Rename(tmp, hostPath)
}

// Launch starts a scene for a test and tears it down with the test. It skips
// the test when the machine has no headless client to run against — that says
// something about the machine, not about the widget.
func Launch(t testing.TB, spec scene.Spec) (s *scene.Session) {
	t.Helper()
	hostOnce.Do(buildHost)
	if hostErr != nil {
		t.Fatalf("unable to build the imzero2 host: %v\n%s", hostErr, hostOut)
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
	s, err := scene.Launch(spec, scene.Options{
		Name:       strings.NewReplacer("/", "-", " ", "-").Replace(t.Name()),
		OutDir:     out,
		HostBinary: hostPath,
		RepoRoot:   hostRoot,
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
