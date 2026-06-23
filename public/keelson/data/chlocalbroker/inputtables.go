package chlocalbroker

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// maxInputTables bounds how many TEMPORARY tables one request may
// inject (ADR-0094 §SD5). Keeps the generated prelude and the worker's
// open-file count sane; introspection joins need a handful, not
// hundreds.
const maxInputTables = 64

// validInputTableName reports whether name is safe to interpolate into
// `CREATE TEMPORARY TABLE <name>` and to use as a file basename. It is
// deliberately narrower than ClickHouse's quoted-identifier grammar —
// `[A-Za-z_][A-Za-z0-9_]*`, up to 64 bytes — so the name needs neither
// quoting nor escaping and cannot smuggle SQL or path separators
// (ADR-0094 §SD5).
func validInputTableName(name string) (ok bool) {
	if name == "" || len(name) > 64 {
		return
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		valid := c == '_' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(i > 0 && c >= '0' && c <= '9')
		if !valid {
			return
		}
	}
	ok = true
	return
}

// materializeInputTables writes each input table's Arrow IPC bytes (the
// `Arrow` file format, i.e. with footer — not `ArrowStream`) to a fresh
// directory under baseTmpDir and returns a multi-statement prelude that
// binds them as TEMPORARY tables. The prelude reads each file by
// ABSOLUTE path: clickhouse-local resolves a bare relative file() path
// against its CWD, which a pre-spawned pool worker does not control, so
// an absolute path is the only reliable reference (ADR-0094 §SD5,
// verified against clickhouse-local 26.5).
//
// cleanup removes the directory and is always safe to call (it is a
// no-op when no directory was created). The caller MUST defer it and
// must keep the files alive until the worker has finished — a
// CREATE TEMPORARY TABLE ... AS SELECT reads the file eagerly, but
// deferring to handler return keeps that invariant without reasoning
// about clickhouse-local's internals.
func materializeInputTables(baseTmpDir string, tables map[string][]byte) (prelude string, cleanup func(), err error) {
	cleanup = func() {}
	if len(tables) == 0 {
		return
	}
	if len(tables) > maxInputTables {
		err = eh.Errorf("chlocalbroker: too many input tables: %d (max %d)", len(tables), maxInputTables)
		return
	}

	names := make([]string, 0, len(tables))
	for name := range tables {
		if !validInputTableName(name) {
			err = eh.Errorf("chlocalbroker: invalid input table name %q", name)
			return
		}
		names = append(names, name)
	}
	sort.Strings(names) // deterministic prelude order (also stabilises the cache key fold)

	dir, mkErr := os.MkdirTemp(baseTmpDir, "chlocal-in-*")
	if mkErr != nil {
		err = eh.Errorf("chlocalbroker: mktemp input dir: %w", mkErr)
		return
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	var b strings.Builder
	for _, name := range names {
		path := filepath.Join(dir, name+".arrow")
		if wErr := os.WriteFile(path, tables[name], 0o600); wErr != nil {
			err = eh.Errorf("chlocalbroker: write input table %q: %w", name, wErr)
			return
		}
		b.WriteString("CREATE TEMPORARY TABLE ")
		b.WriteString(name)
		b.WriteString(" AS SELECT * FROM file(")
		b.WriteString(sqlQuoteString(path))
		b.WriteString(", 'Arrow');\n")
	}
	prelude = b.String()
	return
}

// sqlQuoteString single-quotes a string literal for ClickHouse SQL,
// escaping embedded backslashes and single quotes. Used for the
// file() path; the path comes from os.MkdirTemp so quotes are not
// expected, but escaping keeps the interpolation honest.
func sqlQuoteString(s string) (q string) {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	q = "'" + r.Replace(s) + "'"
	return
}
