//go:build !unix

package scene

import "os/exec"

func ownGroup(cmd *exec.Cmd) {}

func terminateGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func killGroup(cmd *exec.Cmd) { terminateGroup(cmd) }
