//go:build llm_generated_opus46

package commitdigest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

var ErrNotAGitRepo = errors.New("not a git repository")
var ErrNoCommits = errors.New("git repository has no commits")

type RepoDigest struct {
	RepoPath string
	RepoName string
	Commits  []CommitEntry
}

type CommitEntry struct {
	Hash    string
	Author  string
	Date    string
	Subject string
	Body    string
	Stat    string
}

func CollectDigest(ctx context.Context, repoPath string, since string, author string, noStat bool) (digest RepoDigest, err error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		err = eh.Errorf("unable to resolve repo path %q: %w", repoPath, err)
		return
	}
	digest.RepoPath = absPath
	digest.RepoName = filepath.Base(absPath)

	// check if the directory is actually a git repository
	check := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	check.Dir = absPath
	if checkErr := check.Run(); checkErr != nil {
		err = fmt.Errorf("%q: %w", absPath, ErrNotAGitRepo)
		return
	}

	// check if the repo has any commits at all
	headCheck := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "HEAD")
	headCheck.Dir = absPath
	if checkErr := headCheck.Run(); checkErr != nil {
		err = fmt.Errorf("%q: %w", absPath, ErrNoCommits)
		return
	}

	// %x01 separates records, %x00 separates fields within a record.
	// %b (body) may contain newlines, so line-based splitting is not possible.
	args := []string{"log", "--pretty=format:%x01%H%x00%an%x00%ai%x00%s%x00%b"}
	if since != "" {
		args = append(args, "--since="+normalizeSince(since))
	}
	if author != "" {
		args = append(args, "--author="+author)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = absPath
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		err = eh.Errorf("git log failed in %q: %w", absPath, cmdErr)
		return
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return
	}

	records := strings.Split(raw, "\x01")
	digest.Commits = make([]CommitEntry, 0, len(records))
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x00", 5)
		if len(parts) != 5 {
			continue
		}
		digest.Commits = append(digest.Commits, CommitEntry{
			Hash:    parts[0],
			Author:  parts[1],
			Date:    parts[2],
			Subject: parts[3],
			Body:    strings.TrimSpace(parts[4]),
		})
	}

	if !noStat {
		for i := range digest.Commits {
			digest.Commits[i].Stat, err = commitStat(ctx, absPath, digest.Commits[i].Hash)
			if err != nil {
				err = eh.Errorf("unable to get stat for %s: %w", digest.Commits[i].Hash, err)
				return
			}
		}
	}
	return
}

// normalizeSince expands shorthand duration suffixes (e.g. "4h", "2d", "30m")
// into git-compatible date strings ("4 hours ago", "2 days ago", "30 minutes ago").
// Git silently misparses bare suffixes like "4h ago", producing wrong results.
var shorthandRe = regexp.MustCompile(`^(\d+)\s*(m|min|h|hr|d|w)\s*(ago)?$`)

func normalizeSince(s string) string {
	m := shorthandRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return s
	}
	num := m[1]
	var unit string
	switch m[2] {
	case "m", "min":
		unit = "minutes"
	case "h", "hr":
		unit = "hours"
	case "d":
		unit = "days"
	case "w":
		unit = "weeks"
	default:
		return s
	}
	return num + " " + unit + " ago"
}

func commitStat(ctx context.Context, repoDir string, hash string) (stat string, err error) {
	cmd := exec.CommandContext(ctx, "git", "diff-tree", "--stat", "--no-commit-id", hash)
	cmd.Dir = repoDir
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		err = eh.Errorf("git diff-tree failed: %w", cmdErr)
		return
	}
	stat = strings.TrimSpace(string(out))
	return
}

func WriteDigest(w io.Writer, digests []RepoDigest) (err error) {
	bw := bufio.NewWriter(w)
	for i, d := range digests {
		if i > 0 {
			_, err = fmt.Fprintln(bw)
			if err != nil {
				return
			}
		}
		_, err = fmt.Fprintf(bw, "# %s\n", d.RepoName)
		if err != nil {
			return
		}
		if len(d.Commits) == 0 {
			_, err = fmt.Fprintln(bw, "(no commits in range)")
			if err != nil {
				return
			}
			continue
		}
		for _, c := range d.Commits {
			_, err = fmt.Fprintf(bw, "\n## %s — %s (%s)\n%s\n", c.Hash[:12], c.Subject, c.Author, c.Date)
			if err != nil {
				return
			}
			if c.Body != "" {
				_, err = fmt.Fprintf(bw, "\n%s\n", c.Body)
				if err != nil {
					return
				}
			}
			if c.Stat != "" {
				_, err = fmt.Fprintf(bw, "```\n%s\n```\n", c.Stat)
				if err != nil {
					return
				}
			}
		}
	}
	err = bw.Flush()
	return
}
