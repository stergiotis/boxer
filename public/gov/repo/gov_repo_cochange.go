package repo

import (
	"context"
	"iter"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// CoChangeFact is one (commit, file) membership: file Path was changed in the
// commit identified by the dense CommitId. It is the feature-extraction output
// for change-coupling analysis — which files change together — deliberately
// stopping short of the pair counting and coupling-degree math, which the
// consumer runs (this repository evaluates it as SQL over the emitted facts in
// clickhouse-local: a self-join on CommitId turns co-membership into coupled
// pairs, the kind of query that is one line in SQL and a nested loop in Go).
type CoChangeFact struct {
	CommitId int32  `json:"commitId"`
	Path     string `json:"path"`
}

// DefaultMaxChangesetSize bounds a commit's file count before it is treated as
// a mass change (reformat, license-header sweep, generated-code regen) that
// would couple unrelated files. code-maat uses 30; the health doc notes
// >100-file commits are overwhelmingly such noise.
const DefaultMaxChangesetSize = 30

// CoChangeAnalyzer extracts per-(commit, surviving file) membership for
// change-coupling analysis: which files change together. It walks the non-merge
// history and, per commit, emits the tracked files it touched — but only for
// commits touching between 2 and MaxChangesetSize files, so a single-file
// commit (no pair to couple) and a mass change (spurious coupling across
// everything it touches) are both excluded.
//
// The zero value is usable. Renames are detected (-M) so a rename contributes
// its new path; cross-history rename folding is NOT applied, so a file's
// pre-rename co-changes stay under its old name and fall out at the tracked-set
// intersection (the code-maat behaviour — the doc rates its downstream impact
// modest). Deleted files fall out the same way. Author identity is irrelevant
// to co-change and is not read.
type CoChangeAnalyzer struct {
	// PathFilter limits which tracked files participate; nil accepts all.
	PathFilter func(path string) bool
	// MaxChangesetSize drops commits touching more than this many tracked files.
	// Values below 1 select DefaultMaxChangesetSize.
	MaxChangesetSize int
}

func (inst *CoChangeAnalyzer) maxChangeset() (n int) {
	n = inst.MaxChangesetSize
	if n < 1 {
		n = DefaultMaxChangesetSize
	}
	return
}

// ExtractCoChanges walks history and returns the (commit, file) memberships for
// coupling analysis, plus the number of commits that contributed. Commits are
// re-indexed to a dense CommitId in walk order; only the co-membership matters
// downstream, so the specific ids carry no meaning beyond grouping.
func (inst *CoChangeAnalyzer) ExtractCoChanges(ctx context.Context, git *GitRunner) (facts []CoChangeFact, commits int, err error) {
	tracked, err := inst.trackedFiles(ctx, git)
	if err != nil {
		return
	}

	maxCS := inst.maxChangeset()
	const headerSep = "\x01"

	var cur []string                  // tracked files touched by the current commit
	seen := make(map[string]bool, 16) // per-commit dedup
	flush := func() {
		if n := len(cur); n >= 2 && n <= maxCS {
			id := int32(commits)
			commits++
			for _, p := range cur {
				facts = append(facts, CoChangeFact{CommitId: id, Path: p})
			}
		}
		cur = cur[:0]
		clear(seen)
	}

	for line, iterErr := range git.RunLines(ctx, "log", "--no-merges", "-M", "--name-status",
		"--format="+headerSep+"%H") {
		if iterErr != nil {
			err = eh.Errorf("unable to read git log: %w", iterErr)
			return
		}
		if strings.HasPrefix(line, headerSep) {
			flush() // finish the previous commit before starting this one
			continue
		}
		if line == "" {
			continue
		}
		status, paths := parseNameStatus(line)
		if len(paths) == 0 || status == 'D' {
			continue // header gap, or a delete (the file does not survive)
		}
		p := paths[len(paths)-1] // A/M: the path; R/C: the new path
		if _, ok := tracked[p]; !ok {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		cur = append(cur, p)
	}
	flush() // the last commit has no trailing header to trigger a flush
	return
}

// Run streams the co-change memberships (the ExtractCoChanges result), so the
// extraction is consumable like the other analyzers' record streams.
func (inst *CoChangeAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[CoChangeFact, error] {
	return func(yield func(CoChangeFact, error) bool) {
		facts, _, err := inst.ExtractCoChanges(ctx, git)
		if err != nil {
			yield(CoChangeFact{}, err)
			return
		}
		for _, f := range facts {
			if !yield(f, nil) {
				return
			}
		}
	}
}

// trackedFiles returns the tracked, path-filtered file set — the surviving
// universe a co-change must belong to.
func (inst *CoChangeAnalyzer) trackedFiles(ctx context.Context, git *GitRunner) (files map[string]struct{}, err error) {
	files = make(map[string]struct{}, 1024)
	for line, iterErr := range git.RunLines(ctx, "ls-files") {
		if iterErr != nil {
			err = eh.Errorf("unable to list tracked files: %w", iterErr)
			return
		}
		if line == "" {
			continue
		}
		if inst.PathFilter != nil && !inst.PathFilter(line) {
			continue
		}
		files[line] = struct{}{}
	}
	return
}
