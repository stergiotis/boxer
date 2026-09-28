package fsbroker

// appdata.go — the app data area: one directory per app, owned by the
// runtime, which an app reaches by file NAME over fs.appdata.{op}. It is the
// Powerbox's answer for state an app keeps for itself rather than a file the
// user hands it: nothing is picked, so nothing waits on a person, and the
// authority is the manifest's `fs.appdata.>` declaration, which the bus
// enforces on publish. The broker keys the directory on the message's
// Sender — the bus's record of who published, not a payload claim — so an
// app can name only files in its own directory, and a name is a single path
// component, so it cannot name anything outside it.
//
// The ops are the ones a durable record needs and the handle ops do not give:
// an atomic replace (temporary file, fsync, rename), an append that is on
// disk when the reply lands, a stat and a listing.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// SubjectAppDataPrefix is followed by the op: read, write, append, stat, list.
// An app declares `fs.appdata.>` (Pub) to use its data area.
const SubjectAppDataPrefix = "fs.appdata."

const (
	AppDataOpRead   = "read"
	AppDataOpWrite  = "write"
	AppDataOpAppend = "append"
	AppDataOpStat   = "stat"
	AppDataOpList   = "list"
)

// AppDataDirEnv overrides where the app data areas live.
var AppDataDirEnv = env.NewPath(env.Spec{
	Name:        "BOXER_FS_APPDATA_DIR",
	Description: "fs broker: root of the per-app data areas (fs.appdata.*); empty uses <user config dir>/boxer/appdata",
	Category:    env.CategorySystem,
})

// maxAppDataNameLen bounds a file name in a data area.
const maxAppDataNameLen = 128

// AppDataRequest is the payload of every fs.appdata.{op} request. Like
// [DialogRequest] it is an ephemeral control-plane value and rides the
// buscodec default (canonical CBOR) path.
type AppDataRequest struct {
	// Name is the file within the app's data area: one path component of
	// letters, digits, '.', '_' and '-', not starting with '.'. Unused by list.
	Name string `cbor:"name"`
	// Data is what write stores and append adds.
	Data []byte `cbor:"data,omitempty"`
}

// AppDataEntry describes one file of a data area.
type AppDataEntry struct {
	Name    string `cbor:"name"`
	Size    int64  `cbor:"size"`
	ModTime int64  `cbor:"modTime"` // unix nanoseconds
}

// AppDataReply answers every fs.appdata.{op} request.
type AppDataReply struct {
	Ok     bool   `cbor:"ok"`
	Reason string `cbor:"reason,omitempty"`
	// NotExist is set when the named file is absent: a failed read, or a
	// stat that found nothing (Ok stays true for a stat).
	NotExist bool `cbor:"notExist,omitempty"`
	// Data is what read returned.
	Data []byte `cbor:"data,omitempty"`
	// Entry describes the file after write, append and stat.
	Entry AppDataEntry `cbor:"entry"`
	// Entries is what list returned, by name.
	Entries []AppDataEntry `cbor:"entries,omitempty"`
	// Location is the file's host path after write, append and stat. It is
	// for display — the app shows it so an operator can find the file from
	// a shell — and never needed to address the file.
	Location string `cbor:"location,omitempty"`
}

func MarshalAppDataRequest(r AppDataRequest) (b []byte, err error) {
	b, err = buscodec.Encode(r)
	if err != nil {
		err = eh.Errorf("fsbroker: marshal appdata request: %w", err)
	}
	return
}

func UnmarshalAppDataRequest(b []byte) (r AppDataRequest, err error) {
	if len(b) == 0 {
		return
	}
	r, err = buscodec.Decode[AppDataRequest](b)
	if err != nil {
		err = eh.Errorf("fsbroker: unmarshal appdata request: %w", err)
	}
	return
}

func MarshalAppDataReply(r AppDataReply) (b []byte, err error) {
	b, err = buscodec.Encode(r)
	if err != nil {
		err = eh.Errorf("fsbroker: marshal appdata reply: %w", err)
	}
	return
}

func UnmarshalAppDataReply(b []byte) (r AppDataReply, err error) {
	r, err = buscodec.Decode[AppDataReply](b)
	if err != nil {
		err = eh.Errorf("fsbroker: unmarshal appdata reply: %w", err)
	}
	return
}

// defaultAppDataRoot is [AppDataDirEnv], else <user config dir>/boxer/appdata:
// the areas hold records meant to survive a reboot, which rules the cache
// directory out. Empty when the host has neither, and the ops then refuse.
func defaultAppDataRoot() (root string) {
	if root = AppDataDirEnv.Get(); root != "" {
		return
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		root = filepath.Join(cfg, "boxer", "appdata")
	}
	return
}

// SetAppDataRoot sets the root of the data areas; empty disables them.
func (inst *Service) SetAppDataRoot(root string) {
	inst.mu.Lock()
	inst.appDataRoot = root
	inst.mu.Unlock()
}

// appDataDir is the data area of appId: the root, then one directory per
// segment of the id, so the tree reads as the ids do.
func (inst *Service) appDataDir(appId app.AppIdT) (dir string, err error) {
	inst.mu.Lock()
	root := inst.appDataRoot
	inst.mu.Unlock()
	if root == "" {
		err = errors.New("no app data directory on this host")
		return
	}
	segs := strings.Split(string(appId), "/")
	for _, s := range segs {
		if s == "" || strings.HasPrefix(s, ".") || strings.ContainsAny(s, "\\\x00") {
			err = fmt.Errorf("app id %q does not name a data area", appId)
			return
		}
	}
	dir = filepath.Join(append([]string{root}, segs...)...)
	return
}

// ValidAppDataName reports whether name can name a file in a data area.
func ValidAppDataName(name string) (ok bool) {
	if name == "" || len(name) > maxAppDataNameLen || name[0] == '.' {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func (inst *Service) handleAppData(msg *app.Msg) {
	op := strings.TrimPrefix(msg.Subject, SubjectAppDataPrefix)
	r, err := inst.appData(msg.Sender, op, msg.Payload)
	if err != nil {
		r = AppDataReply{Reason: err.Error()}
	}
	payload, merr := MarshalAppDataReply(r)
	if merr != nil {
		inst.log.Warn().Err(merr).Str("op", op).Msg("fsbroker: appdata reply marshal")
		return
	}
	_ = inst.busClient.Publish(msg.Reply, payload)
}

func (inst *Service) appData(sender app.AppIdT, op string, payload []byte) (r AppDataReply, err error) {
	var req AppDataRequest
	req, err = UnmarshalAppDataRequest(payload)
	if err != nil {
		return
	}
	var dir string
	dir, err = inst.appDataDir(sender)
	if err != nil {
		return
	}
	if op == AppDataOpList {
		r.Entries, err = listAppData(dir)
		r.Ok = err == nil
		return
	}
	if !ValidAppDataName(req.Name) {
		err = fmt.Errorf("%q is not a data area file name", req.Name)
		return
	}
	path := filepath.Join(dir, req.Name)
	switch op {
	case AppDataOpRead:
		inst.mu.Lock()
		max := inst.maxReadBytes
		inst.mu.Unlock()
		r.Data, err = readCapped(path, max)
		if errors.Is(err, fs.ErrNotExist) {
			return AppDataReply{NotExist: true, Reason: "no such file: " + req.Name}, nil
		}
	case AppDataOpWrite:
		err = replaceFile(dir, path, req.Data)
	case AppDataOpAppend:
		err = appendFile(dir, path, req.Data)
	case AppDataOpStat:
		// A stat of a missing file is an answer, not a failure.
	default:
		err = fmt.Errorf("unsupported appdata op: %s", op)
	}
	if err != nil {
		return
	}
	r.Ok = true
	if op != AppDataOpRead {
		st, serr := os.Stat(path)
		switch {
		case errors.Is(serr, fs.ErrNotExist):
			r.NotExist = true
		case serr != nil:
			return AppDataReply{}, serr
		default:
			r.Entry = AppDataEntry{Name: req.Name, Size: st.Size(), ModTime: st.ModTime().UnixNano()}
			r.Location = path
		}
	}
	return
}

func readCapped(path string, max int64) (data []byte, err error) {
	if max <= 0 {
		max = DefaultMaxReadBytes
	}
	var f *os.File
	f, err = os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	data, err = io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(data)) > max {
		data, err = nil, fmt.Errorf("file exceeds max read size (%d bytes)", max)
	}
	return
}

// replaceFile writes data to a temporary file beside path, syncs it, renames
// it over path and syncs the directory, so the name holds either the old
// bytes or the new ones, never a part.
func replaceFile(dir string, path string, data []byte) (err error) {
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	var f *os.File
	f, err = os.CreateTemp(dir, ".appdata-*")
	if err != nil {
		return
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return
	}
	syncDir(dir)
	return
}

// appendFile appends data to path and syncs it before returning.
func appendFile(dir string, path string, data []byte) (err error) {
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	var f *os.File
	f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return
}

// syncDir makes a rename in dir durable; best effort, as not every platform
// can open a directory for syncing.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// listAppData lists the files of a data area by name; a missing area is empty.
// Temporary files of an unfinished replace, and anything else no name could
// address, are left out.
func listAppData(dir string) (entries []AppDataEntry, err error) {
	var des []os.DirEntry
	des, err = os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return
	}
	for _, de := range des {
		if !de.Type().IsRegular() || !ValidAppDataName(de.Name()) {
			continue
		}
		info, ierr := de.Info()
		if ierr != nil {
			continue
		}
		entries = append(entries, AppDataEntry{Name: de.Name(), Size: info.Size(), ModTime: info.ModTime().UnixNano()})
	}
	return
}

// AppDataClient is an app's side of its data area. Its ReadFile, WriteFile
// and AppendFile have the shape a store over names wants (a missing file is
// an error wrapping fs.ErrNotExist). Every call is a bus request; call it off
// the frame goroutine.
type AppDataClient struct {
	bus     app.BusI
	timeout time.Duration
}

func NewAppDataClient(bus app.BusI) (inst *AppDataClient) {
	return &AppDataClient{bus: bus, timeout: HandleOpTimeout}
}

func (inst *AppDataClient) request(op string, req AppDataRequest) (r AppDataReply, err error) {
	var payload, body []byte
	payload, err = MarshalAppDataRequest(req)
	if err != nil {
		return
	}
	body, err = inst.bus.RequestWithTimeout(SubjectAppDataPrefix+op, payload, inst.timeout)
	if err != nil {
		return
	}
	r, err = UnmarshalAppDataReply(body)
	if err != nil {
		return
	}
	switch {
	case r.NotExist && !r.Ok:
		err = fmt.Errorf("fsbroker: appdata %s %q: %w", op, req.Name, fs.ErrNotExist)
	case !r.Ok:
		err = fmt.Errorf("fsbroker: appdata %s %q: %s", op, req.Name, r.Reason)
	}
	return
}

func (inst *AppDataClient) ReadFile(name string) (data []byte, err error) {
	var r AppDataReply
	r, err = inst.request(AppDataOpRead, AppDataRequest{Name: name})
	data = r.Data
	return
}

func (inst *AppDataClient) WriteFile(name string, data []byte) (err error) {
	_, err = inst.Write(name, data)
	return
}

func (inst *AppDataClient) AppendFile(name string, data []byte) (err error) {
	_, err = inst.request(AppDataOpAppend, AppDataRequest{Name: name, Data: data})
	return
}

// Write replaces name and returns what the broker reports of it afterwards.
func (inst *AppDataClient) Write(name string, data []byte) (r AppDataReply, err error) {
	return inst.request(AppDataOpWrite, AppDataRequest{Name: name, Data: data})
}

// Stat describes name; r.NotExist is set when it is absent.
func (inst *AppDataClient) Stat(name string) (r AppDataReply, err error) {
	return inst.request(AppDataOpStat, AppDataRequest{Name: name})
}

// List names the files of the data area.
func (inst *AppDataClient) List() (entries []AppDataEntry, err error) {
	var r AppDataReply
	r, err = inst.request(AppDataOpList, AppDataRequest{})
	entries = r.Entries
	return
}
