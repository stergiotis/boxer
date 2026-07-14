package providers

import (
	"encoding/hex"
	"io"
	"os"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// extbinProvider exposes the extbin external-program registry (ADR-0118) as
// keelson.extbin — the audited surface of every host binary this process is
// wired to invoke (git, clickhouse-local, scc, the profilers, built
// artifacts, …), plus where each currently resolves on this host and a blake3
// digest of the resolved binary. That turns "what can this box execute, and is
// it the binary I expect" into a query rather than a filesystem audit.
//
// Live: resolution and the digest are PATH/host dependent, read per query. The
// blake3 column is computed lazily (only when projected) and cached per
// (path, size, mtime), so repeated queries re-hash a binary only when it
// changes on disk; a first SELECT that projects blake3 pays one read per
// present binary.
type extbinProvider struct{}

func (extbinProvider) Name() string                         { return "extbin" }
func (extbinProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (extbinProvider) Schema() *arrow.Schema                { return extbinTable(nil).Schema() }

func (extbinProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	progs := extbin.Registry()
	rows := make([]extbinRow, len(progs))
	for i, p := range progs {
		path, available := p.Resolve() // cheap; no hashing here
		rows[i] = extbinRow{prog: p, resolved: path, available: available}
	}
	return extbinTable(rows).Build(proj, len(rows)), nil
}

type extbinRow struct {
	prog      *extbin.Program
	resolved  string
	available bool
}

func extbinTable(rows []extbinRow) *introspect.Table {
	return introspect.NewTable().
		String("name", func(i int) string { return rows[i].prog.Name }).
		String("kind", func(i int) string { return rows[i].prog.Kind.String() }).
		String("module", func(i int) string { return rows[i].prog.Module }).
		String("override_env", func(i int) string { return rows[i].prog.OverrideEnv }).
		String("install_hint", func(i int) string { return rows[i].prog.InstallHint }).
		Bool("available", func(i int) bool { return rows[i].available }).
		String("resolved_path", func(i int) string { return rows[i].resolved }).
		// Lazy: only hashed when this column is projected (in-process path);
		// url() mode has no pushdown and always materialises it.
		String("blake3", func(i int) string { return resolvedBinaryHash(rows[i].resolved) })
}

type binHashKey struct {
	path  string
	size  int64
	mtime int64
}

var (
	binHashMu    sync.Mutex
	binHashCache = map[binHashKey]string{}
)

// resolvedBinaryHash returns the blake3-256 digest of the file at path, prefixed
// "blake3:", cached per (path, size, mtime) so a frequently-queried table
// re-hashes a binary only when it changes on disk. Best-effort: an empty path
// (an unresolved or Local program) or any read error yields "".
func resolvedBinaryHash(path string) (digest string) {
	if path == "" {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	key := binHashKey{path: path, size: fi.Size(), mtime: fi.ModTime().UnixNano()}
	binHashMu.Lock()
	if d, ok := binHashCache[key]; ok {
		binHashMu.Unlock()
		return d
	}
	binHashMu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	h := blake3.New(32, nil)
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	digest = "blake3:" + hex.EncodeToString(h.Sum(nil))

	binHashMu.Lock()
	binHashCache[key] = digest
	binHashMu.Unlock()
	return digest
}
