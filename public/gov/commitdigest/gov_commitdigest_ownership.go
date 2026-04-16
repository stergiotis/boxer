//go:build llm_generated_opus46

package commitdigest

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type BoundaryCrossing struct {
	File       string   `json:"file"`
	CommitHash string   `json:"commitHash"`
	Author     string   `json:"author"`
	Owners     []string `json:"owners"`
}

// BlameOwners returns the set of author and co-author emails for a file,
// derived from the current git blame snapshot.
func BlameOwners(ctx context.Context, repoDir string, filePath string, coAuthorCache map[string][]string) (owners []string, err error) {
	cmd := exec.CommandContext(ctx, "git", "blame", "--porcelain", "--", filePath)
	cmd.Dir = repoDir
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		// file may be deleted, binary, or empty — no owners
		return nil, nil
	}

	emailSet := make(map[string]struct{})
	commitHashes := make(map[string]struct{})

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "author-mail ") {
			email := strings.TrimPrefix(line, "author-mail ")
			email = strings.Trim(email, "<>")
			email = strings.ToLower(strings.TrimSpace(email))
			if email != "" && email != "not.committed.yet" {
				emailSet[email] = struct{}{}
			}
		}
		// collect unique commit hashes (40 hex chars at start of line)
		if len(line) >= 40 && isHexPrefix(line[:40]) {
			commitHashes[line[:40]] = struct{}{}
		}
	}

	// add co-authors from commit trailers
	for hash := range commitHashes {
		coAuthors, ok := coAuthorCache[hash]
		if !ok {
			coAuthors = commitCoAuthors(ctx, repoDir, hash)
			coAuthorCache[hash] = coAuthors
		}
		for _, email := range coAuthors {
			emailSet[email] = struct{}{}
		}
	}

	owners = make([]string, 0, len(emailSet))
	for email := range emailSet {
		owners = append(owners, email)
	}
	sort.Strings(owners)
	return
}

// commitCoAuthors extracts emails from Co-authored-by trailers in a commit body.
func commitCoAuthors(ctx context.Context, repoDir string, hash string) (emails []string) {
	cmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%b", hash)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "co-authored-by:") {
			email := extractCommitEmail(line)
			if email != "" {
				emails = append(emails, strings.ToLower(email))
			}
		}
	}
	return
}

// commitModifiedFiles returns file paths modified by a commit using diff-tree.
func commitModifiedFiles(ctx context.Context, repoDir string, hash string) (files []string, err error) {
	cmd := exec.CommandContext(ctx, "git", "diff-tree", "--no-commit-id", "--name-only", "-r", hash)
	cmd.Dir = repoDir
	out, cmdErr := cmd.Output()
	if cmdErr != nil {
		err = eb.Build().Str("hash", hash).Errorf("git diff-tree --name-only failed: %w", cmdErr)
		return
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return
	}
	files = strings.Split(raw, "\n")
	return
}

// DetectBoundaryCrossings identifies commits that modify files whose blame
// authors (and co-authors) do not include the commit author.
func DetectBoundaryCrossings(ctx context.Context, repoDir string, commits []CommitEntry) (crossings []BoundaryCrossing, err error) {
	// collect modified files per commit via diff-tree (independent of --no-stat)
	type commitFileEntry struct {
		commit CommitEntry
		files  []string
	}
	entries := make([]commitFileEntry, 0, len(commits))
	allFiles := make(map[string]struct{})
	for _, c := range commits {
		var files []string
		files, err = commitModifiedFiles(ctx, repoDir, c.Hash)
		if err != nil {
			return
		}
		entries = append(entries, commitFileEntry{commit: c, files: files})
		for _, f := range files {
			allFiles[f] = struct{}{}
		}
	}

	// blame each unique file (cached)
	coAuthorCache := make(map[string][]string)
	blameCache := make(map[string][]string, len(allFiles))
	for file := range allFiles {
		var owners []string
		owners, err = BlameOwners(ctx, repoDir, file, coAuthorCache)
		if err != nil {
			return
		}
		blameCache[file] = owners
	}

	// check each commit against blame owners
	crossings = make([]BoundaryCrossing, 0)
	for _, e := range entries {
		authorEmail := extractCommitEmail(e.commit.Author)
		for _, file := range e.files {
			owners := blameCache[file]
			if len(owners) == 0 {
				continue // new file or deleted — no ownership data
			}
			found := false
			for _, owner := range owners {
				if owner == authorEmail {
					found = true
					break
				}
			}
			if !found {
				crossings = append(crossings, BoundaryCrossing{
					File:       file,
					CommitHash: e.commit.Hash,
					Author:     e.commit.Author,
					Owners:     owners,
				})
			}
		}
	}
	return
}

func isHexPrefix(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
