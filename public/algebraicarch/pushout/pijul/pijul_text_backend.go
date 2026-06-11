//go:build llm_generated_opus47

package pijul

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// pijulTextBackend is the realisation of [BackendI] that drives a
// real `pijul` binary by serialising domain cells to pijul's textual
// flat-KV working-copy format. It owns the [RunnerI] for the
// whole demo run — every repo it produces shares the same runner.
type pijulTextBackend struct {
	runner      RunnerI
	trackedFile string
}

var _ BackendI = (*pijulTextBackend)(nil)

// NewPijulTextBackend wires a backend over the given runner. The
// trackedFile is the flat-KV record file the backend reads/writes
// inside each repo (e.g. "customer.txt" in the demo).
func NewPijulTextBackend(runner RunnerI, trackedFile string) (b *pijulTextBackend) {
	b = &pijulTextBackend{
		runner:      runner,
		trackedFile: trackedFile,
	}
	return
}

func (inst *pijulTextBackend) Name() (n string) {
	n = "pijul-text"
	return
}

func (inst *pijulTextBackend) NewRepo(actor string, path string) (repo RepoI) {
	repo = &pijulTextRepo{
		runner:      inst.runner,
		actor:       actor,
		path:        path,
		trackedFile: inst.trackedFile,
	}
	return
}

// Clone delegates to `pijul clone` and returns a fresh repo handle for
// the destination. Both src and dest must use this backend.
func (inst *pijulTextBackend) Clone(ctx context.Context, src RepoI, destPath string, destActor string) (dest RepoI, audit string, err error) {
	srcRepo, ok := src.(*pijulTextRepo)
	if !ok {
		err = eh.Errorf("pijul-text backend cannot clone from a %T", src)
		return
	}
	parentDir := filepath.Dir(destPath)
	name := filepath.Base(destPath)
	merr := os.MkdirAll(parentDir, 0755)
	if merr != nil {
		err = eh.Errorf("create parent dir %s: %w", parentDir, merr)
		return
	}
	audit, err = inst.runner.Clone(ctx, srcRepo.path, parentDir, name)
	if err != nil {
		return
	}
	dest = &pijulTextRepo{
		runner:      inst.runner,
		actor:       destActor,
		path:        destPath,
		trackedFile: inst.trackedFile,
	}
	return
}

// pijulTextRepo is one actor's working copy on the text backend.
// State lives entirely on disk (the pijul repo dir + the rendered
// tracked file); the struct itself is just a handle.
type pijulTextRepo struct {
	runner      RunnerI
	actor       string
	path        string
	trackedFile string
}

var _ RepoI = (*pijulTextRepo)(nil)

func (inst *pijulTextRepo) Path() (p string) {
	p = inst.path
	return
}

// Init creates the repo directory and runs `pijul init`. The tracked
// file is *not* added here — the first [pijulTextRepo.SetAndRecord]
// does the `pijul add` and the initial `pijul record` together.
func (inst *pijulTextRepo) Init(ctx context.Context) (audit string, err error) {
	merr := os.MkdirAll(inst.path, 0755)
	if merr != nil {
		err = eh.Errorf("create repo dir %s: %w", inst.path, merr)
		return
	}
	audit, err = inst.runner.Init(ctx, inst.path)
	return
}

// State reads the working copy, parses it, and decorates the cells
// with patch-level provenance from `pijul credit`. The combined log
// is also returned — the demo always wants both at once.
func (inst *pijulTextRepo) State(ctx context.Context) (cells []KVLine, log []PatchMetadata, audit string, err error) {
	content, rerr := os.ReadFile(filepath.Join(inst.path, inst.trackedFile))
	if rerr != nil {
		// Missing file = pre-init state; not an error.
		if errors.Is(rerr, os.ErrNotExist) {
			return
		}
		err = eh.Errorf("read tracked file: %w", rerr)
		return
	}

	parsed, hasConflict, perr := ParseRecordText(string(content))
	if perr != nil {
		err = perr
		return
	}
	cells = parsed

	entries, logAudit, lerr := inst.runner.Log(ctx, inst.path)
	audit = appendAuditLine(audit, logAudit)
	if lerr != nil {
		err = lerr
		return
	}
	log = make([]PatchMetadata, 0, len(entries))
	for _, e := range entries {
		log = append(log, e.toPatchMetadata())
	}

	if !hasConflict {
		creditOut, credAudit, cerr := inst.runner.Credit(ctx, inst.path, inst.trackedFile)
		audit = appendAuditLine(audit, credAudit)
		if cerr != nil {
			err = cerr
			return
		}
		cells, err = ApplyCreditToCells(creditOut, cells, entries)
		if err != nil {
			return
		}
	}
	return
}

// SetAndRecord serialises cells back to pijul's textual format,
// writes them to the working copy, performs `pijul add` on the very
// first call, and commits a new patch. The latest hash is fetched
// after the record so the caller can refer to the new patch.
func (inst *pijulTextRepo) SetAndRecord(ctx context.Context, cells []KVLine, author string, message string) (id PatchID, audit string, err error) {
	raw := SerializeRecordText(cells)
	file := filepath.Join(inst.path, inst.trackedFile)

	_, statErr := os.Stat(file)
	firstWrite := errors.Is(statErr, os.ErrNotExist)

	werr := os.WriteFile(file, raw, 0644)
	if werr != nil {
		err = eh.Errorf("write %s: %w", file, werr)
		return
	}

	if firstWrite {
		var addAudit string
		addAudit, err = inst.runner.Add(ctx, inst.path, inst.trackedFile)
		audit = appendAuditLine(audit, addAudit)
		if err != nil {
			err = eh.Errorf("pijul add: %w", err)
			return
		}
	}

	recAudit, recErr := inst.runner.Record(ctx, inst.path, author, message)
	audit = appendAuditLine(audit, recAudit)
	if recErr != nil {
		err = eh.Errorf("pijul record: %w", recErr)
		return
	}

	hash, hashAudit, herr := inst.runner.LatestHash(ctx, inst.path)
	audit = appendAuditLine(audit, hashAudit)
	if herr != nil {
		err = eh.Errorf("pijul record succeeded but reading the new hash failed: %w", herr)
		return
	}
	id = PatchID{Hex: hash}
	return
}

// Apply materialises the envelope's bytes to a temporary file and
// runs `pijul apply` against it. Pijul wants a path; the demo
// abstracts envelopes as bytes, so the round-trip happens here.
func (inst *pijulTextRepo) Apply(ctx context.Context, env PatchEnvelope) (audit string, err error) {
	tmp, terr := os.CreateTemp("", "pijul-apply-*.bin")
	if terr != nil {
		err = eh.Errorf("create temp patch file: %w", terr)
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, werr := tmp.Write(env.Bytes)
	cerr := tmp.Close()
	if werr != nil {
		err = eh.Errorf("write temp patch file: %w", werr)
		return
	}
	if cerr != nil {
		err = eh.Errorf("close temp patch file: %w", cerr)
		return
	}
	audit, err = inst.runner.ApplyPatch(ctx, inst.path, tmpName)
	return
}

func (inst *pijulTextRepo) Push(ctx context.Context, dest RepoI) (audit string, err error) {
	other, ok := dest.(*pijulTextRepo)
	if !ok {
		err = eh.Errorf("pijul-text Push requires a pijul-text destination, got %T", dest)
		return
	}
	audit, err = inst.runner.Push(ctx, inst.path, other.path)
	return
}

func (inst *pijulTextRepo) Pull(ctx context.Context, src RepoI) (audit string, hadConflict bool, err error) {
	other, ok := src.(*pijulTextRepo)
	if !ok {
		err = eh.Errorf("pijul-text Pull requires a pijul-text source, got %T", src)
		return
	}
	audit, hadConflict, err = inst.runner.Pull(ctx, inst.path, other.path)
	return
}

// ExportLatest reads the most recent change file under
// .pijul/changes/ and returns its raw bytes plus the latest hash.
// The demo's "Email Patch" feature uses this to seed the shared
// inbox with a transmittable envelope.
func (inst *pijulTextRepo) ExportLatest(ctx context.Context) (env PatchEnvelope, audit string, err error) {
	srcFile, ferr := inst.runner.LatestChangeFile(ctx, inst.path)
	if ferr != nil {
		err = ferr
		return
	}
	hash, hashAudit, herr := inst.runner.LatestHash(ctx, inst.path)
	audit = hashAudit
	if herr != nil {
		err = eh.Errorf("read latest hash: %w", herr)
		return
	}
	bytes, rerr := os.ReadFile(srcFile)
	if rerr != nil {
		err = eh.Errorf("read patch %s: %w", srcFile, rerr)
		return
	}
	env = PatchEnvelope{
		ID:       PatchID{Hex: hash},
		Producer: inst.actor,
		Bytes:    bytes,
	}
	return
}

func appendAuditLine(existing string, line string) (out string) {
	line = strings.TrimSpace(line)
	if line == "" {
		out = existing
		return
	}
	if existing == "" {
		out = line
		return
	}
	out = existing + "\n" + line
	return
}
