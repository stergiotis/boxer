package commitdigest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

var ErrNotAGitRepo = errors.New("not a git repository")
var ErrNoCommits = errors.New("git repository has no commits")

type RepoDigest struct {
	RepoPath string
	RepoName string
	Commits  []CommitEntry
}

type CommitEntry struct {
	Hash        string      `json:"hash"`
	Author      string      `json:"author"`
	Date        string      `json:"date"`
	Subject     string      `json:"subject"`
	Body        string      `json:"body,omitempty"`
	Stat        string      `json:"stat,omitempty"`
	StatEntries []StatEntry `json:"statEntries,omitempty"`
}

// StatEntry is the structured counterpart to the human-readable Stat string:
// per-file add/delete line counts from `git diff-tree --numstat`. Consumed by
// trend-mining SQL so queries do not need to parse the freeform Stat format.
// Adds/Dels are 0 for binary files (git reports "-" for those).
type StatEntry struct {
	Path string `json:"path"`
	Adds int32  `json:"adds"`
	Dels int32  `json:"dels"`
}

// CollectDigest runs git log in repoPath. If fromHash is non-empty, commits
// after that hash up to HEAD are returned (since is ignored); this is how
// seamless resume works. Otherwise commits matching since/author are returned.
func CollectDigest(ctx context.Context, repoPath string, since string, author string, noStat bool, fromHash string) (digest RepoDigest, err error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		err = eb.Build().Str("path", repoPath).Errorf("unable to resolve repo path: %w", err)
		return
	}
	digest.RepoPath = absPath
	digest.RepoName = filepath.Base(absPath)

	// check if the directory is actually a git repository
	if checkErr := extbin.Git.Run(ctx, extbin.Opts{Dir: absPath}, "rev-parse", "--git-dir"); checkErr != nil {
		err = eb.Build().Str("path", absPath).Errorf("not a git repository: %w", ErrNotAGitRepo)
		return
	}

	// check if the repo has any commits at all
	if checkErr := extbin.Git.Run(ctx, extbin.Opts{Dir: absPath}, "rev-parse", "--verify", "HEAD"); checkErr != nil {
		err = eb.Build().Str("path", absPath).Errorf("git repository has no commits: %w", ErrNoCommits)
		return
	}

	// %x01 separates records, %x00 separates fields within a record.
	// %b (body) may contain newlines, so line-based splitting is not possible.
	args := []string{"log", "--pretty=format:%x01%H%x00%an <%ae>%x00%ai%x00%s%x00%b", "--reverse"}
	if author != "" {
		args = append(args, "--author="+author)
	}
	if fromHash != "" {
		args = append(args, fromHash+"..HEAD")
	} else if since != "" {
		args = append(args, "--since="+normalizeSince(since))
	}

	out, cmdErr := extbin.Git.Output(ctx, extbin.Opts{Dir: absPath}, args...)
	if cmdErr != nil {
		err = eb.Build().Str("path", absPath).Errorf("git log failed: %w", cmdErr)
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
				err = eb.Build().Str("hash", digest.Commits[i].Hash).Errorf("unable to get stat: %w", err)
				return
			}
			digest.Commits[i].StatEntries, err = commitNumstat(ctx, absPath, digest.Commits[i].Hash)
			if err != nil {
				err = eb.Build().Str("hash", digest.Commits[i].Hash).Errorf("unable to get numstat: %w", err)
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
	out, cmdErr := extbin.Git.Output(ctx, extbin.Opts{Dir: repoDir}, "diff-tree", "--stat", "--no-commit-id", hash)
	if cmdErr != nil {
		err = eh.Errorf("git diff-tree failed: %w", cmdErr)
		return
	}
	stat = strings.TrimSpace(string(out))
	return
}

// commitNumstat returns per-file add/delete counts for hash. Binary files
// appear in numstat output with "-" for both counts; we report them as 0/0.
// Rename/copy entries use git's "{old => new}" path syntax verbatim — callers
// that care can post-process; for trend mining the new path is what matters.
func commitNumstat(ctx context.Context, repoDir string, hash string) (entries []StatEntry, err error) {
	out, cmdErr := extbin.Git.Output(ctx, extbin.Opts{Dir: repoDir}, "diff-tree", "--numstat", "--no-commit-id", "-r", hash)
	if cmdErr != nil {
		err = eh.Errorf("git diff-tree --numstat failed: %w", cmdErr)
		return
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return
	}
	lines := strings.Split(raw, "\n")
	entries = make([]StatEntry, 0, len(lines))
	for _, line := range lines {
		entry, ok := parseNumstatLine(line)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return
}

// parseNumstatLine extracts one StatEntry from a single git diff-tree --numstat
// record. The expected format is "<adds>\t<dels>\t<path>". Adds/dels are "-"
// for binary files; parseNumstatLine emits 0/0 in that case. Returns ok=false
// for malformed lines so callers skip them.
func parseNumstatLine(line string) (entry StatEntry, ok bool) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return
	}
	if parts[0] != "-" {
		adds, convErr := strconv.ParseInt(parts[0], 10, 32)
		if convErr != nil {
			return
		}
		entry.Adds = int32(adds)
	}
	if parts[1] != "-" {
		dels, convErr := strconv.ParseInt(parts[1], 10, 32)
		if convErr != nil {
			return
		}
		entry.Dels = int32(dels)
	}
	entry.Path = parts[2]
	if entry.Path == "" {
		return
	}
	ok = true
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
			_, err = fmt.Fprintf(bw, "\n## %s — %s (%s)\n%s\n", shortHash(c.Hash), c.Subject, c.Author, c.Date)
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
