package agentrun

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
)

// The writer lease (ADR-0319 §SD7) is held by a run — one process of the
// host — and every writer of a journal is on the one host the deployment
// runs (ADR-0319 §SD1): there is no cross-host writer. Whether a holder is
// alive is therefore a local question with an exact answer, the process,
// not a heartbeat's age.

// Holder names who holds a journal run's lease: the runtime run id of the
// process, and its pid.
type Holder struct {
	Run string
	Pid int
}

// ThisProcess is the holder this process writes as: its runtime run id
// when the run is initialised, a random one otherwise (a test, a tool),
// and its pid.
func ThisProcess() (h Holder) {
	h.Pid = os.Getpid()
	if ri, err := runinfo.Get(); err == nil && ri != nil && ri.RunId != "" {
		h.Run = ri.RunId
		return
	}
	var b [12]byte
	_, _ = rand.Read(b[:])
	h.Run = "proc-" + hex.EncodeToString(b[:])
	return
}

// HolderLivenessI answers whether a holder still runs.
type HolderLivenessI interface {
	Alive(ctx context.Context, h Holder) (alive bool, err error)
}

// LocalLiveness is HolderLivenessI on the one host: a holder is alive
// while a process with its pid runs, unless that process's environment
// names another runtime run — a pid the system reused for a later run of
// the host does not keep a dead holder's lease. A process's environment
// is the one it was started with, so a host that minted its own run id
// does not show it there; an existing process without another run's id
// therefore counts as alive, and a run is refused rather than taken from a
// writer that may still be there.
type LocalLiveness struct{}

var _ HolderLivenessI = LocalLiveness{}

func (inst LocalLiveness) Alive(_ context.Context, h Holder) (alive bool, err error) {
	if h.Pid <= 0 {
		return false, nil
	}
	if h.Pid == os.Getpid() {
		return true, nil
	}
	if !processExists(h.Pid) {
		return false, nil
	}
	env, rerr := os.ReadFile("/proc/" + strconv.Itoa(h.Pid) + "/environ")
	if rerr != nil {
		return true, nil
	}
	return !namesAnotherRun(env, h.Run), nil
}

// namesAnotherRun reports whether a NUL-separated environment carries a
// runtime run id other than run.
func namesAnotherRun(env []byte, run string) bool {
	prefix := []byte(runinfo.EnvVar + "=")
	for _, kv := range bytes.Split(env, []byte{0}) {
		if v, ok := bytes.CutPrefix(kv, prefix); ok {
			return string(v) != run
		}
	}
	return false
}
