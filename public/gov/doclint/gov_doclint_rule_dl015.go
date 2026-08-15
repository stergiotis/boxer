package doclint

import (
	"context"
	"iter"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/gov/repo"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL015 — a `status: stable` document edited after the date it says it was
// reviewed.
//
// The stamp is a claim about verification: "somebody read this on that date and
// it was true". Every commit after it weakens the claim without touching it,
// and nothing in the document says so — the reader sees a stable banner and a
// date, not the six commits since. The class is common enough to have bitten
// this repository's own marshalling how-to, whose stamp predated later edits
// that added statements about component overlap (ADR-0183 D7).
//
// The rule cannot tell a typo fix from a semantic change and does not try. It
// reports that the claim is older than the content, which is a fact; whether it
// matters is the author's call, and the fix is either a re-read plus a new date
// or an honest demotion to draft.
//
// It compares at DAY granularity, so the commit that restamps a document does
// not report the document it just restamped.
//
// Scope and degradation: only `status: stable` with a real `reviewed-date` is
// checked — a draft claims nothing, and a template placeholder is not a date.
// The last-edit date comes from git, so a file with no history (untracked, or
// a checkout without a repository) reports nothing rather than guessing. That
// makes the rule silent where it cannot be right, which is the behaviour a
// linter published to other repositories needs.
type RuleDL015 struct {
	// lastEdit is the seam: the default asks git, and a test supplies the
	// answer directly. Without it this rule could only be tested from inside
	// a repository whose history contained the fixtures, which would mean the
	// fixture and the assertion could not land in the same commit.
	lastEdit func(path string) (date string, has bool)
}

func NewRuleDL015() (inst *RuleDL015) {
	inst = &RuleDL015{lastEdit: gitLastEditDate}
	return
}

// newRuleDL015With builds the rule over a stated last-edit oracle.
func newRuleDL015With(lastEdit func(path string) (string, bool)) (inst *RuleDL015) {
	inst = &RuleDL015{lastEdit: lastEdit}
	return
}

func (inst *RuleDL015) Id() (id string) {
	id = "DL015"
	return
}

func (inst *RuleDL015) Check(roots []string) iter.Seq2[Finding, error] {
	return runMarkdownCheck("DL015", roots, inst.checkOne)
}

func (inst *RuleDL015) checkOne(path string, _ map[string]struct{}, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("DL015 read: %w", err)
		return
	}
	meta, _, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		return
	}
	if meta.Status != "stable" {
		return
	}
	reviewed := strings.TrimSpace(meta.ReviewedDate)
	if !isIsoDate(reviewed) {
		return
	}
	edited, has := inst.lastEdit(path)
	if !has || edited <= reviewed {
		return
	}
	f := Finding{
		RuleId:   "DL015",
		Severity: FindingSeverityWarn,
		Path:     path,
		Line:     1,
		Col:      1,
		Message:  "status: stable, reviewed-date " + reviewed + ", but the file was last edited " + edited + " — re-read and restamp it, or set status: draft",
	}
	cont = yield(f, nil)
	return
}

// gitLastEditDate reports the commit date of the last commit touching path, as
// YYYY-MM-DD. has is false when git cannot answer — no repository, no history
// for the file, or no git at all.
func gitLastEditDate(path string) (date string, has bool) {
	git := &repo.GitRunner{}
	for line, err := range git.RunLines(context.Background(), "log", "-1", "--format=%cs", "--", path) {
		if err != nil {
			return "", false
		}
		line = strings.TrimSpace(line)
		if isIsoDate(line) {
			return line, true
		}
	}
	return
}

// isIsoDate reports whether s is a YYYY-MM-DD date rather than empty or a
// template placeholder.
func isIsoDate(s string) bool {
	if len(s) != len("2006-01-02") {
		return false
	}
	for i, c := range s {
		switch i {
		case 4, 7:
			if c != '-' {
				return false
			}
		default:
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
