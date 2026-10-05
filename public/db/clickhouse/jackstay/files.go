package jackstay

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
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

// LockerI is a [FilesI] that can hold an exclusive lock on a name. A sync
// run locks its plan for as long as it runs (ADR-0259 §SD5), so two runs of
// one plan cannot interleave their clears and journal lines. A FilesI that is
// not a LockerI runs unlocked: [OsFiles] locks, the wizard's fs broker data
// area does not, since the broker offers no exclusive create.
type LockerI interface {
	// Lock takes the lock on name, or fails naming its holder. unlock
	// releases it.
	Lock(name string) (unlock func() (err error), err error)
}

// OsFiles is [FilesI] over the local file system; a name is a path.
type OsFiles struct{}

var _ FilesI = OsFiles{}
var _ LockerI = OsFiles{}

func (OsFiles) ReadFile(name string) (data []byte, err error) {
	return os.ReadFile(name)
}

// WriteFile writes to a temporary file in the same directory, syncs it,
// renames it over name, and syncs the directory.
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
		return
	}
	syncDir(dir)
	return
}

// AppendFile appends and syncs; a file it creates has its directory synced
// too, so the file's name is as durable as its first line.
func (OsFiles) AppendFile(name string, data []byte) (err error) {
	created := false
	var f *os.File
	f, err = os.OpenFile(name, os.O_WRONLY|os.O_APPEND, 0)
	if errors.Is(err, fs.ErrNotExist) {
		f, err = os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		created = true
	}
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
	if err == nil && created {
		syncDir(filepath.Dir(name))
	}
	return
}

// syncDir makes a rename or a creation in dir durable. It is best effort: a
// file system that cannot sync a directory has nothing more to offer.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// lockHolder is the content of a lock file.
type lockHolder struct {
	Pid  int       `json:"pid"`
	Host string    `json:"host"`
	At   time.Time `json:"at"`
}

// Lock creates name.lock exclusively, holding the process id, the host and
// the time. A lock left by a process of this host that no longer runs is
// taken over. Decision: a lock is never taken over by age, since a sync may
// run for days, nor from another host, whose processes cannot be seen; the
// refusal names the file, which the operator removes once sure no run holds
// it.
func (OsFiles) Lock(name string) (unlock func() (err error), err error) {
	path := name + ".lock"
	host, _ := os.Hostname()
	var content []byte
	content, err = json.Marshal(lockHolder{Pid: os.Getpid(), Host: host, At: time.Now().UTC()})
	if err != nil {
		err = eh.Errorf("unable to encode lock: %w", err)
		return
	}
	for attempt := 0; attempt < 2; attempt++ {
		err = createExclusive(path, content)
		if err == nil {
			unlock = func() (err error) {
				held, rerr := os.ReadFile(path)
				if rerr != nil || !bytes.Equal(held, content) {
					// Taken over: no longer this run's to remove.
					return
				}
				err = os.Remove(path)
				if err != nil {
					err = eb.Build().Str("lock", path).Errorf("unable to release lock: %w", err)
				}
				return
			}
			return
		}
		if !errors.Is(err, fs.ErrExist) {
			err = eb.Build().Str("lock", path).Errorf("unable to create lock: %w", err)
			return
		}
		var h lockHolder
		data, rerr := os.ReadFile(path)
		if rerr == nil {
			rerr = json.Unmarshal(data, &h)
		}
		if rerr != nil || h.Host != host || isProcessRunning(h.Pid) || attempt > 0 {
			err = eb.Build().Str("lock", path).Int("pid", h.Pid).Str("host", h.Host).Time("since", h.At).
				Errorf("another run holds the plan; wait for it, or remove the lock file once sure none runs")
			return
		}
		// Left by a process of this host that has ended. It is moved aside
		// rather than removed, so of two runs taking it over only the one
		// that moved the dead holder's lock goes on; one that moved a lock
		// the other has just made puts it back and refuses.
		aside := path + ".stale-" + strconv.Itoa(os.Getpid())
		if os.Rename(path, aside) != nil {
			continue
		}
		moved, _ := os.ReadFile(aside)
		if !bytes.Equal(moved, data) {
			_ = os.Link(aside, path)
			_ = os.Remove(aside)
			err = eb.Build().Str("lock", path).Errorf("another run took the plan's lock over at the same time")
			return
		}
		_ = os.Remove(aside)
	}
	return
}

func createExclusive(path string, content []byte) (err error) {
	var f *os.File
	f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, err = f.Write(content)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return
	}
	syncDir(filepath.Dir(path))
	return
}

// isProcessRunning reports whether pid runs on this host; when that cannot be
// told, it says it does.
func isProcessRunning(pid int) (running bool) {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	err = p.Signal(syscall.Signal(0))
	return !errors.Is(err, os.ErrProcessDone)
}
