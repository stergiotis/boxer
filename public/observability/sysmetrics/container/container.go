package container

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// DetectorI is the public surface a container detector implements.
type DetectorI interface {
	Detect(ctx context.Context) (info sysmsnap.ContainerInfo, err error)
}

// Options configures a [Detector].
type Options struct {
	// Proc, when non-nil, overrides the procfs.Reader the Detector
	// reads /proc/1/cgroup from. Defaults to procfs.New("").
	Proc *procfs.Reader

	// MarkerRoot, when non-empty, is prepended to absolute paths that
	// detection probes (/.dockerenv, /run/.containerenv,
	// /run/systemd/container). Defaults to "/" — the live filesystem.
	// Tests redirect this to a t.TempDir() to fake marker presence.
	MarkerRoot string
}

// Detector probes the live filesystem for container runtime markers.
type Detector struct {
	proc       *procfs.Reader
	markerRoot string
}

// New returns a Detector configured by opts. The returned error is
// always nil today; the signature reserves the slot for forward-
// compatibility.
func New(opts Options) (inst *Detector, err error) {
	if opts.Proc == nil {
		opts.Proc = procfs.New("")
	}
	if opts.MarkerRoot == "" {
		opts.MarkerRoot = "/"
	}
	inst = &Detector{
		proc:       opts.Proc,
		markerRoot: opts.MarkerRoot,
	}
	return
}

var _ DetectorI = (*Detector)(nil)

// Detect classifies the host according to the precedence documented on
// the package. Hard errors (ctx cancellation, /proc not present)
// propagate; absent marker files are not errors.
func (inst *Detector) Detect(ctx context.Context) (info sysmsnap.ContainerInfo, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	// 1. Podman
	if pathExists(inst.markerPath("run/.containerenv")) {
		info.Engine = sysmsnap.EnginePodman
		return
	}
	// 2. Docker (legacy /.dockerenv)
	if pathExists(inst.markerPath(".dockerenv")) {
		info.Engine = sysmsnap.EngineDocker
		return
	}
	// 3. systemd-nspawn / vendor-set runtime
	if data, rerr := os.ReadFile(inst.markerPath("run/systemd/container")); rerr == nil {
		s := strings.TrimSpace(string(data))
		info.Detail = s
		switch s {
		case "systemd-nspawn":
			info.Engine = sysmsnap.EngineSystemdNspawn
		case "docker":
			info.Engine = sysmsnap.EngineDocker
		case "podman":
			info.Engine = sysmsnap.EnginePodman
		case "lxc", "lxc-libvirt":
			info.Engine = sysmsnap.EngineLXC
		default:
			info.Engine = sysmsnap.EngineUnknown
		}
		return
	} else if !errors.Is(rerr, fs.ErrNotExist) {
		err = eh.Errorf("read /run/systemd/container: %w", rerr)
		return
	}

	// 4. cgroup substring matching. Read errors (ENOENT, EPERM) are
	// silently ignored — unprivileged user-namespace containers commonly
	// hide /proc/1/cgroup, and absent procfs is fine on a sandboxed test.
	if cgroupRaw, cerr := inst.proc.ReadFile("1/cgroup"); cerr == nil {
		info = classifyCgroup(cgroupRaw)
		if info.Engine != sysmsnap.EngineNone {
			return
		}
	}

	info.Engine = sysmsnap.EngineNone
	return
}

func (inst *Detector) markerPath(rel string) (path string) {
	return filepath.Join(inst.markerRoot, rel)
}

func pathExists(path string) (yes bool) {
	_, err := os.Stat(path)
	return err == nil
}

// classifyCgroup walks /proc/1/cgroup lines and matches known runtime
// substrings. The file shape is "<hierarchy-id>:<controller>:<path>"
// per kernel docs; we match on the path field's substrings.
func classifyCgroup(content []byte) (info sysmsnap.ContainerInfo) {
	for line := range procfs.IterLines(content) {
		path := lastColonField(line)
		if len(path) == 0 {
			continue
		}
		switch {
		case bytes.Contains(path, []byte("kubepods")):
			info.Engine = sysmsnap.EngineKubernetes
			info.Detail = string(path)
			return
		case bytes.Contains(path, []byte("/docker/")), bytes.Contains(path, []byte("docker-")):
			info.Engine = sysmsnap.EngineDocker
			info.Detail = string(path)
			return
		case bytes.Contains(path, []byte("/podman/")), bytes.Contains(path, []byte("podman-")):
			info.Engine = sysmsnap.EnginePodman
			info.Detail = string(path)
			return
		case bytes.Contains(path, []byte("/lxc/")), bytes.Contains(path, []byte("/lxc.payload")):
			info.Engine = sysmsnap.EngineLXC
			info.Detail = string(path)
			return
		}
	}
	return
}

// lastColonField returns the substring after the last ':' in line.
// /proc/[pid]/cgroup is "id:controller:path" per kernel docs; we want
// path. cgroup v2 lines are "0::/path".
func lastColonField(line []byte) (path []byte) {
	idx := bytes.LastIndexByte(line, ':')
	if idx < 0 {
		return nil
	}
	return line[idx+1:]
}
