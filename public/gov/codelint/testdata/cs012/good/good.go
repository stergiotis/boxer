package good

import "os/exec"

// Referencing the exec.Cmd type — e.g. holding a command resolved by extbin —
// is fine. Only the resolving/spawning calls (Command/CommandContext/LookPath)
// are banned outside package extbin.
func holdsCmd(c *exec.Cmd) bool {
	return c != nil
}
