package repo

import (
	"context"
	"iter"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// FileAuthorChange is one (surviving file, developer) authorship fact: how many
// changes the developer made to the file and whether they created it. It is the
// feature-extraction output of TruckFactorAnalyzer — the raw material a
// degree-of-authorship (DOA) truck-factor computation consumes, deliberately
// stopping short of the DOA math itself.
//
// The Avelino et al. (ICPC 2016) truck factor scores each such fact with
//
//	DOA = 3.293 + 1.098·FA + 0.164·DL − 0.321·ln(1 + AC)
//
// where FA is FirstAuthor (0/1), DL is OwnChanges, and AC is the file's total
// changes minus OwnChanges (every other developer's changes). Keeping that
// weighting, the normalized/absolute author thresholds, and the greedy removal
// out of this package lets the consumer evaluate them however it likes — this
// repository runs them as SQL over the emitted facts in clickhouse-local — while
// Go retains the parts that genuinely need it: git traversal, rename following,
// and identity canonicalization.
type FileAuthorChange struct {
	// Path is the file's current (HEAD) path; renames are already folded so a
	// moved file's whole history lands on this one key.
	Path string `json:"path"`
	// Email is the developer's mailmap-canonicalized, lower-cased address.
	Email string `json:"email"`
	// Name is the developer's most recent author name (display only).
	Name string `json:"name"`
	// OwnChanges is DL: the number of non-merge commits by this developer that
	// touched the file.
	OwnChanges int32 `json:"ownChanges"`
	// FirstAuthor is FA: true when this developer created the file. A file
	// created by an excluded developer (a bot) has no first author among the
	// emitted facts.
	FirstAuthor bool `json:"firstAuthor"`
}

// TruckFactorAnalyzer extracts the per-(file, developer) authorship facts a
// degree-of-authorship truck-factor estimate is built from. It replays the full
// non-merge history to learn, for every file that survives at HEAD, who created
// it and how many changes each developer made, then emits one FileAuthorChange
// per (file, developer) pair.
//
// The zero value is usable. Cleaning follows the truck-factor literature and is
// done here, in Go, because it needs git semantics or a local identity model:
// merges are excluded (--no-merges); renames are followed (-M) so a file's
// history is not fragmented across its names; author identities are folded
// through the injected Mailmap; and PathFilter/AuthorExcluder drop vendored
// paths and bot identities. Full history is always replayed (no since/until
// window) because first authorship is undefined once a file's creating commit
// falls outside the window.
type TruckFactorAnalyzer struct {
	// Mailmap canonicalizes author identities before they are keyed, folding a
	// person's variant emails into one developer. nil lower-cases the email
	// only (the pre-mailmap behaviour).
	Mailmap *Mailmap
	// PathFilter limits which tracked files are emitted; nil accepts all. This
	// is the highest-leverage cleaning knob: vendored and generated trees swamp
	// the estimate — the literature's canonical example is Homebrew's truck
	// factor of 250 with its formula folder versus 2 without it.
	PathFilter func(path string) bool
	// AuthorExcluder reports developers to drop entirely: their changes count
	// toward no one, not even as other developers' changes, and they can never
	// be a file's first author. Intended for high-precision bot-identity
	// matching. nil keeps everyone.
	AuthorExcluder func(email string, name string) bool
}

// ExtractChangeFacts replays history and returns one FileAuthorChange per
// (surviving tracked file, contributing developer), sorted by path then email
// for deterministic output. Files deleted before HEAD, files filtered out by
// PathFilter, and files whose only changes came from excluded developers do not
// appear. totalFiles is the size of the tracked, path-filtered universe — the
// denominator for "what fraction of files have an identifiable author", which
// the emitted facts alone cannot report (authorless files leave no fact).
func (inst *TruckFactorAnalyzer) ExtractChangeFacts(ctx context.Context, git *GitRunner) (facts []FileAuthorChange, totalFiles int, err error) {
	tracked, err := inst.trackedFiles(ctx, git)
	if err != nil {
		return
	}
	totalFiles = len(tracked)
	histories, names, err := inst.replayHistory(ctx, git)
	if err != nil {
		return
	}

	facts = make([]FileAuthorChange, 0, len(tracked))
	for path, h := range histories {
		if _, ok := tracked[path]; !ok {
			continue // deleted before HEAD, or filtered out of the tracked universe
		}
		for dev, n := range h.changes {
			facts = append(facts, FileAuthorChange{
				Path:        path,
				Email:       dev,
				Name:        names[dev],
				OwnChanges:  int32(n),
				FirstAuthor: dev == h.creator,
			})
		}
	}
	sort.Slice(facts, func(i, j int) bool {
		if facts[i].Path != facts[j].Path {
			return facts[i].Path < facts[j].Path
		}
		return facts[i].Email < facts[j].Email
	})
	return
}

// Run streams the change facts (the ExtractChangeFacts result, yielded in the
// same deterministic order), so the extraction is consumable like the other
// analyzers' record streams — e.g. by the CLI's universal formatter.
func (inst *TruckFactorAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[FileAuthorChange, error] {
	return func(yield func(FileAuthorChange, error) bool) {
		facts, _, err := inst.ExtractChangeFacts(ctx, git)
		if err != nil {
			yield(FileAuthorChange{}, err)
			return
		}
		for _, f := range facts {
			if !yield(f, nil) {
				return
			}
		}
	}
}

// fileHistory accumulates one file's authorship inputs as history replays
// oldest-first.
type fileHistory struct {
	// creator is the canonical email of the developer who added the file. It
	// may name an excluded developer (a bot), in which case no kept developer
	// earns first authorship for the file.
	creator string
	// changes maps a kept developer's canonical email to their change count on
	// the file. Excluded developers never appear.
	changes map[string]int
}

// trackedFiles returns the set of tracked, path-filtered files — the universe a
// file must belong to for its history to be emitted (so files deleted before
// HEAD, and vendored/generated paths, drop out).
func (inst *TruckFactorAnalyzer) trackedFiles(ctx context.Context, git *GitRunner) (files map[string]struct{}, err error) {
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

// replayHistory walks the full non-merge history oldest-first, accumulating per
// file the first author and each developer's change count. Renames are followed
// (-M) by migrating the old path's accumulated history onto the new path, so a
// file that moved keeps one continuous authorship record keyed by its current
// name. names carries each developer's most recent author name for display.
func (inst *TruckFactorAnalyzer) replayHistory(ctx context.Context, git *GitRunner) (histories map[string]*fileHistory, names map[string]string, err error) {
	histories = make(map[string]*fileHistory, 1024)
	names = make(map[string]string, 32)

	const headerSep = "\x01"
	const fieldSep = "\x1f"

	var curDev string // canonical email of the current commit's author ("" = unparsed)
	var curKeep bool  // whether the current author is credited (not excluded)

	touch := func(path string) *fileHistory {
		h := histories[path]
		if h == nil {
			h = &fileHistory{changes: make(map[string]int, 4)}
			histories[path] = h
		}
		return h
	}
	credit := func(path string) {
		if !curKeep {
			return
		}
		touch(path).changes[curDev]++
	}

	// --reverse walks oldest-first so first authorship is the first add seen and
	// a rename always finds the old path already populated. --name-status with
	// -M reports A/M/D plus R<score> old new (and C<score> for copies).
	for line, iterErr := range git.RunLines(ctx, "log", "--reverse", "--no-merges", "-M", "--name-status",
		"--format="+headerSep+"%H"+fieldSep+"%ae"+fieldSep+"%an") {
		if iterErr != nil {
			err = eh.Errorf("unable to read git log: %w", iterErr)
			return
		}
		if strings.HasPrefix(line, headerSep) {
			parts := strings.SplitN(line[len(headerSep):], fieldSep, 3)
			if len(parts) != 3 {
				curDev, curKeep = "", false
				continue
			}
			name, email := inst.Mailmap.Resolve(parts[2], parts[1])
			curDev = email
			curKeep = email != "" && (inst.AuthorExcluder == nil || !inst.AuthorExcluder(email, name))
			if curKeep && name != "" {
				names[email] = name // oldest-first replay ⇒ latest commit's name wins
			}
			continue
		}
		if curDev == "" { // blank separators, or status lines of an unparsed header
			continue
		}
		status, paths := parseNameStatus(line)
		if len(paths) == 0 {
			continue
		}
		switch status {
		case 'A': // add: record first authorship, then credit the change
			p := paths[0]
			if h := touch(p); h.creator == "" {
				h.creator = curDev
			}
			credit(p)
		case 'R', 'C': // rename/copy: fold source history onto the destination
			if len(paths) >= 2 {
				migrateHistory(histories, paths[0], paths[1])
				credit(paths[1])
			} else {
				credit(paths[len(paths)-1])
			}
		case 'D': // delete: the file no longer survives; drop its history
			delete(histories, paths[0])
		default: // M, T, U, …: an ordinary change to the (last) path
			credit(paths[len(paths)-1])
		}
	}
	return
}

// migrateHistory moves oldPath's accumulated authorship onto newPath so a
// rename does not fragment a file's history. If newPath already carries history
// (a rare delete-then-recreate-then-rename), the counts are merged and the
// earliest recorded creator is kept.
func migrateHistory(histories map[string]*fileHistory, oldPath string, newPath string) {
	src := histories[oldPath]
	if src == nil {
		return
	}
	delete(histories, oldPath)
	dst := histories[newPath]
	if dst == nil {
		histories[newPath] = src
		return
	}
	for dev, n := range src.changes {
		dst.changes[dev] += n
	}
	if dst.creator == "" {
		dst.creator = src.creator
	}
}

// parseNameStatus splits one `git log --name-status` line into its status byte
// and following path(s). Renames and copies carry a similarity score after the
// letter (e.g. "R100") and two tab-separated paths; adds, modifies and deletes
// carry one. Only the leading status byte is returned — the score is unused.
func parseNameStatus(line string) (status byte, paths []string) {
	tab := strings.IndexByte(line, '\t')
	if tab < 1 {
		return 0, nil
	}
	status = line[0]
	for p := range strings.SplitSeq(line[tab+1:], "\t") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return
}
