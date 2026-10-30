//go:build unix

package lane

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the child in a process group of its own, so a signal to
// the group reaches whatever the job's handler started — an external
// program through extbin — and a Ctrl-C at the host's terminal does not
// reach the child (ADR-0344 §SD4).
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
