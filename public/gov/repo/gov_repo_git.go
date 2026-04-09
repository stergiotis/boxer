//go:build llm_generated_opus46

package repo

import (
	"bufio"
	"context"
	"iter"
	"os/exec"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type GitRunner struct {
	RepoPath string
}

func (inst *GitRunner) repoDir() (dir string) {
	dir = inst.RepoPath
	if dir == "" {
		dir = "."
	}
	return
}

func (inst *GitRunner) RunLines(ctx context.Context, args ...string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = inst.repoDir()

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			yield("", eh.Errorf("unable to create stdout pipe: %w", err))
			return
		}

		err = cmd.Start()
		if err != nil {
			yield("", eh.Errorf("unable to start git command: %w", err))
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if !yield(scanner.Text(), nil) {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return
			}
		}

		err = scanner.Err()
		if err != nil {
			yield("", eh.Errorf("error reading git output: %w", err))
			return
		}

		err = cmd.Wait()
		if err != nil {
			yield("", eh.Errorf("git command failed: %w", err))
			return
		}
	}
}

func (inst *GitRunner) buildLogArgs(format string, since string, until string, extraArgs ...string) (args []string) {
	args = make([]string, 0, 8+len(extraArgs))
	args = append(args, "log")
	args = append(args, "--pretty=format:"+format)
	if since != "" {
		args = append(args, "--since="+since)
	}
	if until != "" {
		args = append(args, "--until="+until)
	}
	args = append(args, extraArgs...)
	return
}
