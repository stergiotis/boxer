package sysfs

import (
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultRoot is the conventional /sys mount path.
const DefaultRoot = "/sys"

// Reader reads files under a /sys-shaped root.
type Reader struct {
	root string
}

// New returns a Reader rooted at rootDir. An empty rootDir defaults to
// [DefaultRoot] ("/sys").
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

// Resolve returns the absolute on-disk path for rel under the Reader's root.
// Useful for error messages and for callers that need to pass a path to a
// non-Reader API such as [filepath.EvalSymlinks].
func (inst *Reader) Resolve(rel string) (path string) {
	return filepath.Join(inst.root, rel)
}

// ReadFile reads /sys/<rel> and returns the file bytes.
func (inst *Reader) ReadFile(rel string) (data []byte, err error) {
	p := filepath.Join(inst.root, rel)
	data, err = os.ReadFile(p)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("read sysfs file: %w", err)
		return
	}
	return
}

// ReadString reads /sys/<rel> and returns its contents as a string with
// trailing whitespace (newline, carriage return, tab, space) trimmed.
// Sysfs leaf files almost always end with a newline; this is the right
// default.
func (inst *Reader) ReadString(rel string) (s string, err error) {
	var data []byte
	data, err = inst.ReadFile(rel)
	if err != nil {
		return
	}
	s = strings.TrimRight(string(data), "\n\r\t ")
	return
}

// ListDir returns the names of entries in /sys/<rel>, sorted (os.ReadDir
// returns entries already sorted). When the directory does not exist, the
// returned error wraps [fs.ErrNotExist].
func (inst *Reader) ListDir(rel string) (names []string, err error) {
	p := filepath.Join(inst.root, rel)
	var entries []os.DirEntry
	entries, err = os.ReadDir(p)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("list sysfs dir: %w", err)
		return
	}
	names = make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return
}

// IterPrefix yields entries under /sys/<base> whose names begin with
// prefix, in sorted order. Used for /sys/class/hwmon/hwmon*-style class
// enumeration. The first call to ListDir failing yields a single
// ("", err) and stops; subsequent yields carry nil errors.
func (inst *Reader) IterPrefix(base, prefix string) (seq iter.Seq2[string, error]) {
	return func(yield func(string, error) bool) {
		names, err := inst.ListDir(base)
		if err != nil {
			yield("", err)
			return
		}
		for _, n := range names {
			if !strings.HasPrefix(n, prefix) {
				continue
			}
			if !yield(n, nil) {
				return
			}
		}
	}
}

// EvalSymlink returns the absolute target of the symlink at /sys/<rel>.
// Sysfs nodes use canonical symlinks heavily (e.g. /sys/class/net/eth0 →
// ../../devices/...); callers occasionally need the resolved path to
// disambiguate which physical device an alias refers to.
func (inst *Reader) EvalSymlink(rel string) (target string, err error) {
	p := filepath.Join(inst.root, rel)
	target, err = filepath.EvalSymlinks(p)
	if err != nil {
		err = eb.Build().Str("path", p).Errorf("eval sysfs symlink: %w", err)
		return
	}
	return
}
