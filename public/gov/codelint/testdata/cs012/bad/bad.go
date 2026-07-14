package bad

import "os/exec"

func spawn() error {
	cmd := exec.Command("git", "status") // want a CS012 finding here
	return cmd.Run()
}

func look() (string, error) {
	return exec.LookPath("git") // want a CS012 finding here
}
