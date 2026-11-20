package main

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestEveryCommandHasAUsage walks the whole command tree and fails for a
// command whose Usage — the one line `--help` prints beside its name — is
// empty. A command that is listed without a word about what it does is one a
// reader has to run to find out.
func TestEveryCommandHasAUsage(t *testing.T) {
	var walk func(prefix string, cmds []*cli.Command)
	walk = func(prefix string, cmds []*cli.Command) {
		for _, c := range cmds {
			if c == nil || c.Hidden {
				continue
			}
			path := strings.TrimSpace(prefix + " " + c.Name)
			if strings.TrimSpace(c.Usage) == "" {
				t.Errorf("command %q has no Usage", path)
			}
			walk(path, c.Commands)
		}
	}
	app := newApp()
	if strings.TrimSpace(app.Usage) == "" {
		t.Errorf("the root command has no Usage")
	}
	walk("", app.Commands)
}
