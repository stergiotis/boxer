package jackstay

import (
	"os"
	"path/filepath"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// FilesI is where a plan and its journal are kept, by name. The CLI keeps
// them at paths ([OsFiles]); the window keeps them in the data area the
// runtime's fs broker gives it, where a name is all it holds. Both give the
// guarantees the journal's resume rests on (ADR-0259 §SD5).
type FilesI interface {
	// ReadFile returns the whole file; a missing one is an error wrapping
	// fs.ErrNotExist.
	ReadFile(name string) (data []byte, err error)
	// WriteFile replaces the file atomically, durable when it returns.
	WriteFile(name string, data []byte) (err error)
	// AppendFile appends to the file, creating it, durable when it returns.
	AppendFile(name string, data []byte) (err error)
}

// OsFiles is [FilesI] over the local file system; a name is a path.
type OsFiles struct{}

var _ FilesI = OsFiles{}

func (OsFiles) ReadFile(name string) (data []byte, err error) {
	return os.ReadFile(name)
}

// WriteFile writes to a temporary file in the same directory, syncs it, and
// renames it over name.
func (OsFiles) WriteFile(name string, data []byte) (err error) {
	dir := filepath.Dir(name)
	var f *os.File
	f, err = os.CreateTemp(dir, ".jackstay-plan-*")
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to create temporary file: %w", err)
		return
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, name)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return
}

func (OsFiles) AppendFile(name string, data []byte) (err error) {
	var f *os.File
	f, err = os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return
}
