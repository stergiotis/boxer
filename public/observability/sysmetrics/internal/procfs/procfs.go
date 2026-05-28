//go:build llm_generated_opus47

package procfs

import (
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultRoot is the conventional /proc mount path.
const DefaultRoot = "/proc"

// Reader reads files under a /proc-shaped root.
type Reader struct {
	root string
}

// New returns a Reader rooted at rootDir. An empty rootDir defaults to
// [DefaultRoot] ("/proc").
func New(rootDir string) (inst *Reader) {
	if rootDir == "" {
		rootDir = DefaultRoot
	}
	return &Reader{root: rootDir}
}

// Root returns the root the Reader resolves rel paths against.
func (inst *Reader) Root() (root string) {
	return inst.root
}

// ReadFile reads the file at rel under the Reader's root and returns its
// bytes. Errors are wrapped with the resolved path for diagnostics; ENOENT
// remains detectable via [errors.Is].
func (inst *Reader) ReadFile(rel string) (data []byte, err error) {
	p := filepath.Join(inst.root, rel)
	data, err = os.ReadFile(p)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("read procfs file: %w", err)
		return
	}
	return
}

// ReadFileInto reads the file at rel under the Reader's root into dst,
// returning a slice aliasing dst's backing array (extended as needed). When
// dst has enough capacity to hold the file's contents no heap allocation
// occurs — the intended use is a caller-owned scratch buffer reused across
// many reads of the small /proc files, which is what makes a per-tick walk
// of every PID allocation-flat.
//
// dst is treated as a clean scratch buffer: its existing contents are
// overwritten (the returned slice starts at index 0). Pass dst == nil on
// the first call; pass the previous return value (or its prefix) on
// subsequent calls.
//
// Errors are wrapped with the resolved path for diagnostics; ENOENT remains
// detectable via [errors.Is].
func (inst *Reader) ReadFileInto(rel string, dst []byte) (out []byte, err error) {
	p := filepath.Join(inst.root, rel)
	f, oerr := os.Open(p)
	if oerr != nil {
		err = eb.Build().Str("path", p).Errorf("open procfs file: %w", oerr)
		return
	}
	defer func() { _ = f.Close() }()

	// /proc files report Size()=0 from Stat, so we cannot pre-size; grow on
	// demand. Initial capacity matches the os.ReadFile default (512 B); most
	// /proc files (stat, comm, cmdline) fit; status is ~2 KB and triggers
	// one growth on the first read of the calling Collector's lifetime.
	out = dst[:0]
	if cap(out) < 512 {
		out = make([]byte, 0, 512)
	}
	for {
		if len(out) == cap(out) {
			// Grow geometrically (double) so per-read cost stays amortised.
			grown := make([]byte, len(out), 2*cap(out))
			copy(grown, out)
			out = grown
		}
		n, rerr := f.Read(out[len(out):cap(out)])
		out = out[:len(out)+n]
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return
			}
			err = eb.Build().Str("path", p).Errorf("read procfs file: %w", rerr)
			return
		}
	}
}

// IterLines yields each LF-terminated line in content, including any final
// unterminated line. Yielded slices alias content.
func IterLines(content []byte) (seq iter.Seq[[]byte]) {
	return func(yield func([]byte) bool) {
		i := 0
		for j := 0; j < len(content); j++ {
			if content[j] == '\n' {
				if !yield(content[i:j]) {
					return
				}
				i = j + 1
			}
		}
		if i < len(content) {
			yield(content[i:])
		}
	}
}

// IterFields yields whitespace-separated fields of line. Whitespace runs
// (spaces and tabs) are collapsed; leading and trailing whitespace is
// dropped. Yielded slices alias line.
func IterFields(line []byte) (seq iter.Seq[[]byte]) {
	return func(yield func([]byte) bool) {
		i := 0
		for i < len(line) {
			for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
				i++
			}
			if i >= len(line) {
				return
			}
			j := i
			for j < len(line) && line[j] != ' ' && line[j] != '\t' {
				j++
			}
			if !yield(line[i:j]) {
				return
			}
			i = j
		}
	}
}

// IterKV iterates key/value pairs from a /proc file shaped like
//
//	Key:    value
//	Other:  another value with spaces
//
// Yielded slices alias content. The key is the substring before the first
// colon (with surrounding whitespace trimmed); the value is everything
// after it (also trimmed). Lines without a colon are skipped silently.
func IterKV(content []byte) (seq iter.Seq2[[]byte, []byte]) {
	return func(yield func([]byte, []byte) bool) {
		for line := range IterLines(content) {
			if len(line) == 0 {
				continue
			}
			colon := -1
			for j := 0; j < len(line); j++ {
				if line[j] == ':' {
					colon = j
					break
				}
			}
			if colon < 0 {
				continue
			}
			if !yield(trimWS(line[:colon]), trimWS(line[colon+1:])) {
				return
			}
		}
	}
}

func trimWS(b []byte) (out []byte) {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\t') {
		i++
	}
	j := len(b)
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}
