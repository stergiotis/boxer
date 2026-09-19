//go:build unix

package scene

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the host in a process group of its own. The Go host spawns the
// Rust client, and a teardown that signals only the parent leaves the child
// holding the carrier's port — which is how the next scene ends up attached to
// the previous one.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, sig)
	}
}

func terminateGroup(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGTERM) }
func killGroup(cmd *exec.Cmd)      { signalGroup(cmd, syscall.SIGKILL) }
