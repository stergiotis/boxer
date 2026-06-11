// Seam-based unit tests for the text backend: a scripted RunnerI fake
// replaces the pijul binary, so the backend's own logic — first-write
// add, record flow, state parsing, credit attribution, conflict
// passthrough, error propagation — is exercised hermetically. The
// binary-gated differential test covers fidelity against real pijul;
// these cover the paths that test cannot reach (error injections) and
// run everywhere.
package pijul

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type fakeRunner struct {
	calls []string

	latestHash    string
	latestHashErr error
	logEntries    []LogEntry
	creditOut     string
	pullConflict  bool
	changeFile    string
}

var _ RunnerI = (*fakeRunner)(nil)

func (f *fakeRunner) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeRunner) Init(ctx context.Context, repoDir string) (string, error) {
	f.record("init")
	return "[fake] init", nil
}

func (f *fakeRunner) Clone(ctx context.Context, srcRepo, parentDir, name string) (string, error) {
	f.record("clone")
	return "[fake] clone", nil
}

func (f *fakeRunner) Add(ctx context.Context, repoDir, file string) (string, error) {
	f.record("add " + file)
	return "[fake] add", nil
}

func (f *fakeRunner) Record(ctx context.Context, repoDir, author, message string) (string, error) {
	f.record("record " + message)
	return "[fake] record", nil
}

func (f *fakeRunner) Push(ctx context.Context, repoDir, remoteRepo string) (string, error) {
	f.record("push")
	return "[fake] push", nil
}

func (f *fakeRunner) Pull(ctx context.Context, repoDir, remoteRepo string) (string, bool, error) {
	f.record("pull")
	return "[fake] pull", f.pullConflict, nil
}

func (f *fakeRunner) ApplyPatch(ctx context.Context, repoDir, patchPath string) (string, error) {
	f.record("apply " + filepath.Base(patchPath))
	return "[fake] apply", nil
}

func (f *fakeRunner) Log(ctx context.Context, repoDir string) ([]LogEntry, string, error) {
	f.record("log")
	return f.logEntries, "[fake] log", nil
}

func (f *fakeRunner) LatestHash(ctx context.Context, repoDir string) (string, string, error) {
	f.record("latest-hash")
	return f.latestHash, "[fake] latest-hash", f.latestHashErr
}

func (f *fakeRunner) Credit(ctx context.Context, repoDir, file string) (string, string, error) {
	f.record("credit")
	return f.creditOut, "[fake] credit", nil
}

func (f *fakeRunner) LatestChangeFile(ctx context.Context, repoDir string) (string, error) {
	f.record("latest-change-file")
	if f.changeFile == "" {
		return "", eh.Errorf("no change file scripted")
	}
	return f.changeFile, nil
}

func newFakeTextRepo(tt *testing.T, f *fakeRunner) RepoI {
	tt.Helper()
	repo := NewPijulTextBackend(f, "customer.txt").NewRepo("alice", filepath.Join(tt.TempDir(), "repo"))
	if _, err := repo.Init(context.Background()); err != nil {
		tt.Fatal(err)
	}
	return repo
}

func calls(f *fakeRunner, name string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, name) {
			n++
		}
	}
	return n
}

// First SetAndRecord must `pijul add` the tracked file; later ones must
// not. The recorded id comes from LatestHash.
func TestTextBackend_FirstWriteAddsTrackedFile(tt *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{latestHash: "FAKEHASH1234"}
	repo := newFakeTextRepo(tt, f)

	id, _, err := repo.SetAndRecord(ctx, []KVLine{{Path: "k", Value: "v"}}, "alice", "first")
	if err != nil {
		tt.Fatal(err)
	}
	if id.Hex != "FAKEHASH1234" {
		tt.Fatalf("id: %q", id.Hex)
	}
	if _, _, err := repo.SetAndRecord(ctx, []KVLine{{Path: "k", Value: "w"}}, "alice", "second"); err != nil {
		tt.Fatal(err)
	}
	if got := calls(f, "add"); got != 1 {
		tt.Fatalf("expected exactly one pijul add, got %d (%v)", got, f.calls)
	}
	if got := calls(f, "record"); got != 2 {
		tt.Fatalf("expected two records, got %d", got)
	}
}

// A failing LatestHash after a successful record must surface as an
// error, not a silent empty/sentinel id (review finding L5).
func TestTextBackend_LatestHashErrorPropagates(tt *testing.T) {
	f := &fakeRunner{latestHashErr: eh.Errorf("boom")}
	repo := newFakeTextRepo(tt, f)
	_, _, err := repo.SetAndRecord(context.Background(), []KVLine{{Path: "k", Value: "v"}}, "alice", "m")
	if err == nil || !strings.Contains(err.Error(), "reading the new hash failed") {
		tt.Fatalf("expected hash-read failure, got: %v", err)
	}
}

// Quoted values must survive the write→parse round trip through the
// real tracked file, and credit attribution must match on the QUOTED
// content key (the unquoted key was a review regression).
func TestTextBackend_StateParsesQuotedValuesWithCredit(tt *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{
		latestHash: "AAAABBBBCCCC",
		logEntries: []LogEntry{{
			Hash:      "AAAABBBBCCCC",
			Authors:   []string{"alice"},
			Timestamp: "2026-06-12T00:00:00.000000000Z",
			Message:   "quoted",
		}},
		creditOut: "AAAABBBBCCCC\n" +
			`> len "2\""` + "\n",
	}
	repo := newFakeTextRepo(tt, f)
	if _, _, err := repo.SetAndRecord(ctx, []KVLine{{Path: "len", Value: `2"`}}, "alice", "quoted"); err != nil {
		tt.Fatal(err)
	}
	cells, log, _, err := repo.State(ctx)
	if err != nil {
		tt.Fatal(err)
	}
	if len(cells) != 1 || cells[0].Value != `2"` {
		tt.Fatalf("quoted value round-trip: %+v", cells)
	}
	if cells[0].Credit == nil || cells[0].Credit.Author() != "alice" {
		tt.Fatalf("credit not attributed via quoted content key: %+v", cells[0].Credit)
	}
	if len(log) != 1 || log[0].Message != "quoted" {
		tt.Fatalf("log: %+v", log)
	}
}

// Conflict markers in the working copy short-circuit credit and carry
// both sides out.
func TestTextBackend_StateParsesConflictMarkers(tt *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{}
	repo := newFakeTextRepo(tt, f)
	raw := ">>>>>>> 1\nk \"alice\"\n=======\nk \"bob\"\n<<<<<<< 2\n"
	if err := os.WriteFile(filepath.Join(repo.Path(), "customer.txt"), []byte(raw), 0o644); err != nil {
		tt.Fatal(err)
	}
	cells, _, _, err := repo.State(ctx)
	if err != nil {
		tt.Fatal(err)
	}
	if len(cells) != 1 || cells[0].Conflict == nil {
		tt.Fatalf("expected one conflict cell: %+v", cells)
	}
	if cells[0].Conflict.AliceValue != "alice" || cells[0].Conflict.BobValue != "bob" {
		tt.Fatalf("conflict sides: %+v", cells[0].Conflict)
	}
	if got := calls(f, "credit"); got != 0 {
		tt.Fatal("credit must be skipped while the working copy is conflicted")
	}
}

// A pathologically long line must surface the scanner error instead of
// returning a silently truncated cell list (review finding M5).
func TestTextBackend_StateSurfacesScannerError(tt *testing.T) {
	ctx := context.Background()
	repo := newFakeTextRepo(tt, &fakeRunner{})
	huge := "k \"" + strings.Repeat("x", 2<<20) + "\"\n"
	if err := os.WriteFile(filepath.Join(repo.Path(), "customer.txt"), []byte(huge), 0o644); err != nil {
		tt.Fatal(err)
	}
	_, _, _, err := repo.State(ctx)
	if err == nil || !strings.Contains(err.Error(), "scan record text") {
		tt.Fatalf("expected scanner error, got: %v", err)
	}
}

// Pull passes the conflict classification through untouched.
func TestTextBackend_PullConflictPassthrough(tt *testing.T) {
	ctx := context.Background()
	f := &fakeRunner{pullConflict: true}
	backend := NewPijulTextBackend(f, "customer.txt")
	a := backend.NewRepo("alice", filepath.Join(tt.TempDir(), "a"))
	b := backend.NewRepo("bob", filepath.Join(tt.TempDir(), "b"))
	_, hadConflict, err := a.Pull(ctx, b)
	if err != nil || !hadConflict {
		tt.Fatalf("hadConflict=%v err=%v", hadConflict, err)
	}
}

// ExportLatest ships the latest change file's bytes with the latest hash.
func TestTextBackend_ExportLatest(tt *testing.T) {
	ctx := context.Background()
	dir := tt.TempDir()
	change := filepath.Join(dir, "deadbeef.change")
	if err := os.WriteFile(change, []byte("BINARY"), 0o644); err != nil {
		tt.Fatal(err)
	}
	f := &fakeRunner{latestHash: "DEADBEEF1234", changeFile: change}
	repo := newFakeTextRepo(tt, f)
	env, _, err := repo.ExportLatest(ctx)
	if err != nil {
		tt.Fatal(err)
	}
	if env.ID.Hex != "DEADBEEF1234" || string(env.Bytes) != "BINARY" {
		tt.Fatalf("envelope: %+v", env)
	}
}
