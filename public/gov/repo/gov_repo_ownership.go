package repo

import (
	"context"
	"iter"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// OwnerKindE distinguishes the two provenance classes ownership analysis
// attributes surviving lines to: human contributors (keyed by lower-cased
// author email) and code-generation models (keyed by a model tag derived
// from Co-Authored-By trailers).
type OwnerKindE uint8

const (
	OwnerKindHuman OwnerKindE = 1
	OwnerKindModel OwnerKindE = 2
)

// String returns a stable label for diagnostics and serialized output.
func (inst OwnerKindE) String() (s string) {
	switch inst {
	case OwnerKindHuman:
		s = "human"
	case OwnerKindModel:
		s = "model"
	default:
		s = "unknown"
	}
	return
}

// OwnerShare is one owner's surviving-line count within a file.
type OwnerShare struct {
	Kind  OwnerKindE `json:"kind"`
	Id    string     `json:"id"`
	Lines int        `json:"lines"`
}

// OwnershipRecord holds per-file surviving-line ownership: which owner the
// file's current lines blame back to. Ownership is a stock (who owns the
// code as it exists now), unlike AuthorshipRecord's cumulative flow of
// added lines. Owners is sorted by Lines descending (ties by kind then id),
// so Owners[0] is the dominant owner. Lines from uncommitted working-tree
// edits carry no provenance and are counted separately.
type OwnershipRecord struct {
	FilePath         string       `json:"filePath"`
	TotalLines       int          `json:"totalLines"`
	UncommittedLines int          `json:"uncommittedLines,omitempty"`
	Owners           []OwnerShare `json:"owners"`
}

// OwnerTotal is one owner's aggregate over every analyzed file.
type OwnerTotal struct {
	Kind OwnerKindE `json:"kind"`
	Id   string     `json:"id"`
	// Display is a human-readable name: the author name most recently
	// seen for the email (humans) or the model tag (models).
	Display       string `json:"display"`
	Lines         int    `json:"lines"`
	DominantFiles int    `json:"dominantFiles"`
}

// SponsorRecord counts how many commits co-authored by ModelId were
// committed under a given human author — the accountability side of model
// provenance: the person who ran the model and vouched for its output.
type SponsorRecord struct {
	ModelId     string `json:"modelId"`
	AuthorEmail string `json:"authorEmail"`
	AuthorName  string `json:"authorName"`
	Commits     int    `json:"commits"`
}

// OwnershipSummary is the whole-repository aggregate of an ownership run.
type OwnershipSummary struct {
	TotalLines       int               `json:"totalLines"`
	AttributedLines  int               `json:"attributedLines"`
	UncommittedLines int               `json:"uncommittedLines"`
	SkippedFiles     int               `json:"skippedFiles"`
	Files            []OwnershipRecord `json:"files"`
	Owners           []OwnerTotal      `json:"owners"`
	Sponsors         []SponsorRecord   `json:"sponsors"`
}

// OwnershipAnalyzer attributes every surviving line of every tracked file
// to an owner by joining `git blame --porcelain -w -M` with one upfront
// commit-metadata pass. A line whose commit carries a Co-Authored-By
// trailer naming a known code-generation model is owned by that model
// (provenance); every other line is owned by the commit's author email.
// The zero value is usable.
type OwnershipAnalyzer struct {
	// ModelMatcher classifies one Co-Authored-By trailer value and returns
	// the owning model's canonical tag. nil selects DefaultModelMatcher.
	// Callers with an identity registry (exact display names per model
	// revision) should inject a matcher built from it.
	ModelMatcher func(coauthor string) (tag string, ok bool)
	// PathFilter limits which tracked files are blamed; nil accepts all.
	PathFilter func(path string) bool
	// Parallelism bounds concurrent `git blame` child processes. Values
	// below 1 select a default of min(8, GOMAXPROCS).
	Parallelism int
	// Progress, when non-nil, observes the blame join's advancement: once
	// with (0, total) after the file list is known, then (done, total)
	// after each completed batch. The blame join dominates a run's wall
	// time (one git blame per tracked file), so this is the run's natural
	// progress signal. Called from the analyzer's coordinating goroutine,
	// never concurrently; implementations must return quickly and must
	// not call back into the analyzer.
	Progress func(done int, total int)
}

// DefaultModelMatcher matches Co-Authored-By trailer values by vendor
// family (the AuthorshipAnalyzer heuristic), yielding the family name as
// the tag. It keeps successive model revisions attributed without a
// registry to maintain; inject a registry-backed matcher for per-revision
// tags.
func DefaultModelMatcher(coauthor string) (tag string, ok bool) {
	s := strings.ToLower(coauthor)
	switch {
	case strings.Contains(s, "claude"):
		tag, ok = "claude", true
	case strings.Contains(s, "gemini"):
		tag, ok = "gemini", true
	}
	return
}

// CommitRecord is one commit with its ownership provenance: when it was
// authored, the accountable author, the subject, and the matched model
// tag (empty for purely human commits). Streamed newest-first (git log
// order).
type CommitRecord struct {
	Hash        string `json:"hash"`
	AuthorSec   int64  `json:"authorSec"` // author date, unix seconds UTC
	AuthorEmail string `json:"authorEmail"`
	AuthorName  string `json:"authorName"`
	Subject     string `json:"subject"`
	ModelTag    string `json:"modelTag,omitempty"`
}

// ownerKeyT is the map key for owner aggregation.
type ownerKeyT struct {
	kind OwnerKindE
	id   string
}

// ownershipCommitT carries the per-commit metadata needed to attribute a
// blamed line: the accountable author and the matched model tag (empty
// for purely human commits).
type ownershipCommitT struct {
	authorEmail string
	authorName  string
	modelTag    string
}

// Run yields one OwnershipRecord per tracked, non-empty file, in
// `git ls-files` order. Files whose blame fails (e.g. gitlink entries)
// are skipped; if every file fails, the first error is reported.
func (inst *OwnershipAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[OwnershipRecord, error] {
	return func(yield func(OwnershipRecord, error) bool) {
		commits, _, err := inst.scanCommits(ctx, git)
		if err != nil {
			yield(OwnershipRecord{}, err)
			return
		}
		inst.runFiles(ctx, git, commits, yield, nil)
	}
}

// RunSummary collects every record and aggregates owner totals (with
// display names) plus model sponsorship counts.
func (inst *OwnershipAnalyzer) RunSummary(ctx context.Context, git *GitRunner) (summary OwnershipSummary, err error) {
	commits, emailNames, scanErr := inst.scanCommits(ctx, git)
	if scanErr != nil {
		err = scanErr
		return
	}

	totals := make(map[ownerKeyT]*OwnerTotal, 64)
	inst.runFiles(ctx, git, commits, func(rec OwnershipRecord, recErr error) bool {
		if recErr != nil {
			err = recErr
			return false
		}
		summary.Files = append(summary.Files, rec)
		summary.TotalLines += rec.TotalLines
		summary.UncommittedLines += rec.UncommittedLines
		for i, share := range rec.Owners {
			key := ownerKeyT{kind: share.Kind, id: share.Id}
			total := totals[key]
			if total == nil {
				total = &OwnerTotal{Kind: share.Kind, Id: share.Id}
				totals[key] = total
			}
			total.Lines += share.Lines
			if i == 0 {
				total.DominantFiles++
			}
		}
		return true
	}, &summary.SkippedFiles)
	if err != nil {
		return
	}
	summary.AttributedLines = summary.TotalLines - summary.UncommittedLines

	summary.Owners = make([]OwnerTotal, 0, len(totals))
	for _, total := range totals {
		total.Display = total.Id
		if total.Kind == OwnerKindHuman {
			if name := emailNames[total.Id]; name != "" {
				total.Display = name
			}
		}
		summary.Owners = append(summary.Owners, *total)
	}
	sort.Slice(summary.Owners, func(i, j int) bool {
		a, b := summary.Owners[i], summary.Owners[j]
		if a.Lines != b.Lines {
			return a.Lines > b.Lines
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Id < b.Id
	})

	summary.Sponsors = collectSponsors(commits, emailNames)
	return
}

// RunCommits streams the provenance-classified commit log, newest first:
// one CommitRecord per commit reachable from HEAD, in a single `git log`
// pass. This is the flow-side companion to the blame join — use it for
// raw commit views and for joining precise author timestamps by hash.
func (inst *OwnershipAnalyzer) RunCommits(ctx context.Context, git *GitRunner) iter.Seq2[CommitRecord, error] {
	matcher := inst.ModelMatcher
	if matcher == nil {
		matcher = DefaultModelMatcher
	}
	const fieldSep = "\x1f"
	const coauthorSep = "\x1e"
	return func(yield func(CommitRecord, error) bool) {
		for line, iterErr := range git.RunLines(ctx, "log",
			"--format=%H"+fieldSep+"%at"+fieldSep+"%ae"+fieldSep+"%an"+fieldSep+"%s"+fieldSep+"%(trailers:key=Co-authored-by,valueonly,separator="+coauthorSep+")") {
			if iterErr != nil {
				yield(CommitRecord{}, eh.Errorf("unable to read git log: %w", iterErr))
				return
			}
			parts := strings.SplitN(line, fieldSep, 6)
			if len(parts) != 6 || parts[0] == "" {
				continue
			}
			rec := CommitRecord{
				Hash:        parts[0],
				AuthorEmail: strings.ToLower(parts[2]),
				AuthorName:  parts[3],
				Subject:     parts[4],
			}
			if sec, convErr := strconv.ParseInt(parts[1], 10, 64); convErr == nil {
				rec.AuthorSec = sec
			}
			if parts[5] != "" {
				for co := range strings.SplitSeq(parts[5], coauthorSep) {
					if tag, ok := matcher(strings.TrimSpace(co)); ok {
						rec.ModelTag = tag
						break
					}
				}
			}
			if !yield(rec, nil) {
				return
			}
		}
	}
}

// scanCommits folds the RunCommits stream into the blame-join lookup:
// hash → provenance, plus author email → most recently used author name
// (git log emits newest first).
func (inst *OwnershipAnalyzer) scanCommits(ctx context.Context, git *GitRunner) (commits map[string]ownershipCommitT, emailNames map[string]string, err error) {
	commits = make(map[string]ownershipCommitT, 1024)
	emailNames = make(map[string]string, 32)
	for rec, iterErr := range inst.RunCommits(ctx, git) {
		if iterErr != nil {
			err = iterErr
			return
		}
		commits[rec.Hash] = ownershipCommitT{
			authorEmail: rec.AuthorEmail,
			authorName:  rec.AuthorName,
			modelTag:    rec.ModelTag,
		}
		if _, seen := emailNames[rec.AuthorEmail]; !seen && rec.AuthorName != "" {
			emailNames[rec.AuthorEmail] = rec.AuthorName
		}
	}
	return
}

// runFiles blames every tracked file (batched Parallelism-wide) and feeds
// records to yield in `git ls-files` order. Per-file blame failures are
// counted into skipped (when non-nil) rather than aborting the run; if no
// file succeeds the first failure is surfaced.
func (inst *OwnershipAnalyzer) runFiles(ctx context.Context, git *GitRunner, commits map[string]ownershipCommitT, yield func(OwnershipRecord, error) bool, skipped *int) {
	files, err := inst.listFiles(ctx, git)
	if err != nil {
		yield(OwnershipRecord{}, err)
		return
	}

	parallelism := inst.Parallelism
	if parallelism < 1 {
		parallelism = min(8, runtime.GOMAXPROCS(0))
	}
	if inst.Progress != nil {
		inst.Progress(0, len(files))
	}

	yielded := 0
	var firstErr error
	for start := 0; start < len(files); start += parallelism {
		end := min(start+parallelism, len(files))
		batch := files[start:end]
		recs := make([]OwnershipRecord, len(batch))
		errs := make([]error, len(batch))
		var wg sync.WaitGroup
		for i, path := range batch {
			wg.Add(1)
			go func(i int, path string) {
				defer wg.Done()
				recs[i], errs[i] = inst.blameFile(ctx, git, commits, path)
			}(i, path)
		}
		wg.Wait()
		if ctx.Err() != nil {
			yield(OwnershipRecord{}, eh.Errorf("ownership analysis cancelled: %w", ctx.Err()))
			return
		}
		for i := range batch {
			if errs[i] != nil {
				if firstErr == nil {
					firstErr = errs[i]
				}
				if skipped != nil {
					*skipped++
				}
				continue
			}
			if recs[i].TotalLines == 0 {
				continue
			}
			yielded++
			if !yield(recs[i], nil) {
				return
			}
		}
		if inst.Progress != nil {
			inst.Progress(end, len(files))
		}
	}
	if yielded == 0 && firstErr != nil {
		yield(OwnershipRecord{}, eh.Errorf("every blame failed: %w", firstErr))
	}
}

// listFiles returns the tracked paths to analyze, in `git ls-files` order.
func (inst *OwnershipAnalyzer) listFiles(ctx context.Context, git *GitRunner) (files []string, err error) {
	files = make([]string, 0, 1024)
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
		files = append(files, line)
	}
	return
}

// blameFile attributes one file's surviving lines. Whitespace-insensitive
// with within-file move detection (-w -M) so reformat and refactor commits
// do not steal ownership.
func (inst *OwnershipAnalyzer) blameFile(ctx context.Context, git *GitRunner, commits map[string]ownershipCommitT, path string) (rec OwnershipRecord, err error) {
	rec.FilePath = path
	counts := make(map[ownerKeyT]int, 8)

	var curHash string
	for line, iterErr := range git.RunLines(ctx, "blame", "--porcelain", "-w", "-M", "--", path) {
		if iterErr != nil {
			err = eh.Errorf("blame failed for %q: %w", path, iterErr)
			return
		}
		if isBlameHeaderLine(line) {
			curHash = line[:40]
			continue
		}
		if !strings.HasPrefix(line, "\t") || curHash == "" {
			continue
		}
		rec.TotalLines++
		if isAllZeroHash(curHash) {
			rec.UncommittedLines++ // working-tree edit: no provenance yet
			continue
		}
		counts[ownerKeyFor(commits, curHash)]++
	}

	rec.Owners = make([]OwnerShare, 0, len(counts))
	for key, lines := range counts {
		rec.Owners = append(rec.Owners, OwnerShare{Kind: key.kind, Id: key.id, Lines: lines})
	}
	sort.Slice(rec.Owners, func(i, j int) bool {
		a, b := rec.Owners[i], rec.Owners[j]
		if a.Lines != b.Lines {
			return a.Lines > b.Lines
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Id < b.Id
	})
	return
}

// ownerKeyFor resolves a blamed commit to its owner: the matched model
// tag when the commit carries a model trailer, else the author email. A
// hash missing from the metadata map (should not happen for ancestors of
// HEAD) degrades to a stable placeholder id.
func ownerKeyFor(commits map[string]ownershipCommitT, hash string) (key ownerKeyT) {
	info, ok := commits[hash]
	if !ok {
		key = ownerKeyT{kind: OwnerKindHuman, id: "(unknown)"}
		return
	}
	if info.modelTag != "" {
		key = ownerKeyT{kind: OwnerKindModel, id: info.modelTag}
		return
	}
	id := info.authorEmail
	if id == "" {
		id = "(unknown)"
	}
	key = ownerKeyT{kind: OwnerKindHuman, id: id}
	return
}

// collectSponsors aggregates model-co-authored commits per (model, human
// author) pair, sorted by commit count descending (ties by model then
// email) for deterministic output.
func collectSponsors(commits map[string]ownershipCommitT, emailNames map[string]string) (sponsors []SponsorRecord) {
	type sponsorKeyT struct {
		model string
		email string
	}
	counts := make(map[sponsorKeyT]int, 16)
	for _, info := range commits {
		if info.modelTag == "" {
			continue
		}
		counts[sponsorKeyT{model: info.modelTag, email: info.authorEmail}]++
	}
	sponsors = make([]SponsorRecord, 0, len(counts))
	for key, n := range counts {
		sponsors = append(sponsors, SponsorRecord{
			ModelId:     key.model,
			AuthorEmail: key.email,
			AuthorName:  emailNames[key.email],
			Commits:     n,
		})
	}
	sort.Slice(sponsors, func(i, j int) bool {
		a, b := sponsors[i], sponsors[j]
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		if a.ModelId != b.ModelId {
			return a.ModelId < b.ModelId
		}
		return a.AuthorEmail < b.AuthorEmail
	})
	return
}

// isBlameHeaderLine reports whether a `git blame --porcelain` line starts
// a new line group: a 40-hex commit hash followed by line numbers.
func isBlameHeaderLine(line string) bool {
	if len(line) < 40 {
		return false
	}
	if len(line) > 40 && line[40] != ' ' {
		return false
	}
	for i := range 40 {
		c := line[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// isAllZeroHash reports the synthetic hash git blame uses for
// not-yet-committed lines.
func isAllZeroHash(hash string) bool {
	for i := range len(hash) {
		if hash[i] != '0' {
			return false
		}
	}
	return len(hash) > 0
}
