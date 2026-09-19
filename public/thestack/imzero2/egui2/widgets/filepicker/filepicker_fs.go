package filepicker

import (
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsbrowser"
)

// entryAsDirEntry presents a browser [fsbrowser.Entry] as the
// [fs.DirEntry] the With*Filter predicates are written against, so the
// option signatures did not change when the listing became the
// widget's. Everything answers from the entry the browser cached when
// it read the directory; nothing here touches the file system.
type entryAsDirEntry struct {
	e fsbrowser.Entry
}

var _ fs.DirEntry = entryAsDirEntry{}
var _ fs.FileInfo = entryAsDirEntry{}

func (inst entryAsDirEntry) Name() string       { return inst.e.Name }
func (inst entryAsDirEntry) IsDir() bool        { return inst.e.IsDir }
func (inst entryAsDirEntry) Type() fs.FileMode  { return inst.e.Mode.Type() }
func (inst entryAsDirEntry) Size() int64        { return inst.e.Size }
func (inst entryAsDirEntry) Mode() fs.FileMode  { return inst.e.Mode }
func (inst entryAsDirEntry) ModTime() time.Time { return inst.e.ModTime }
func (inst entryAsDirEntry) Sys() any           { return nil }

// Info reports the error the listing met when it asked for this entry's
// info, as [fs.DirEntry.Info] would have.
func (inst entryAsDirEntry) Info() (info fs.FileInfo, err error) {
	if inst.e.InfoErr != nil {
		err = inst.e.InfoErr
		return
	}
	info = inst
	return
}

// normalizeExtensions lower-cases and strips leading dots from a list
// of user-supplied extensions. Empty entries are dropped. Returns nil
// for effectively-empty input so callers can treat nil as "no filter".
func normalizeExtensions(exts []string) (out []string) {
	if len(exts) == 0 {
		return
	}
	out = make([]string, 0, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		e = strings.TrimPrefix(e, ".")
		if e == "" {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		out = nil
	}
	return
}

// passesExtFilter reports whether de is shown given the active filter.
// Directories always pass. With nil/empty filter every file passes.
func passesExtFilter(de fs.DirEntry, filter []string) (ok bool) {
	if de.IsDir() || len(filter) == 0 {
		ok = true
		return
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(de.Name()), "."))
	if slices.Contains(filter, ext) {
		ok = true
		return
	}
	return
}
