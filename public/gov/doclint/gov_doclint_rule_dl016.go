package doclint

import (
	"iter"
	"os"
	"regexp"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL016 — a backticked source location pinned by line number.
//
// Implements DOCUMENTATION_STANDARD §4 *What to leave out*: `file.go:244`
// is stale on the next edit to that file and nothing flags it, because the
// line still exists — it just holds something else. The durable form names
// the symbol (a search either resolves it or shows that it is gone) and
// links the file (DL007 checks the link).
//
// The rule matches an inline code span of the shape `<path>.<ext>:<digits>`
// with an optional `-<digits>` range, where <ext> is one of the source and
// document extensions in-repo pins are written against. The extension
// allow-list is what keeps `localhost:8080` and `example.com:443` out.
//
// Severity is info: the shape is ubiquitous in existing docs (291 under
// boxer's doc/ on 2026-08-27, per the standard), and a rule that starts
// at warn would drown the gate's output in findings nobody is about to
// fix in one sweep. The count is reported so the owner can raise it when
// the backlog is cleared. The path half of the pin is checked separately
// by DL017: a pin into a file that does not exist is a phantom citation,
// not a stale one.
type RuleDL016 struct{}

func NewRuleDL016() (inst *RuleDL016) {
	inst = &RuleDL016{}
	return
}

func (inst *RuleDL016) Id() (id string) {
	id = "DL016"
	return
}

func (inst *RuleDL016) Check(roots []string) iter.Seq2[Finding, error] {
	return runMarkdownCheck("DL016", roots, checkOneDL016)
}

// linePinRe matches `path.ext:NNN` and `path.ext:NNN-MMM`. The path may
// carry directories but no whitespace; the extension is restricted to the
// kinds of files a doc pins into (see RuleDL016).
var linePinRe = regexp.MustCompile(
	`^([^\s:` + "`" + `]+\.(?:go|md|rs|proto|sh|bash|sql|py|c|h|cc|cpp|hpp|ts|tsx|js|jsx|toml|yaml|yml|json|txt|dhall|nix|zig|kt|java|rb|el|lua|hs|ml|mli|swift)):(\d+)(?:-(\d+))?$`)

// matchLinePin reports whether tok is a line pin and, if so, the path half.
func matchLinePin(tok string) (path string, pinned bool) {
	m := linePinRe.FindStringSubmatch(tok)
	if m == nil {
		return
	}
	path = m[1]
	pinned = true
	return
}

func checkOneDL016(filePath string, _ map[string]struct{}, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(filePath)
	if err != nil {
		err = eb.Build().Str("path", filePath).Errorf("DL016 read: %w", err)
		return
	}
	_, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		body = data
	}
	lineOffset := frontMatterLineOffset(data, body)

	forEachBacktickToken(body, func(tok backtickToken) bool {
		if _, pinned := matchLinePin(tok.Text); !pinned {
			return true
		}
		f := Finding{
			RuleId:   "DL016",
			Severity: FindingSeverityInfo,
			Path:     filePath,
			Line:     tok.Line + lineOffset,
			Col:      1,
			Message: "'" + tok.Text + "' pins a source location by line number, which goes stale on the next edit to that file; " +
				"name the symbol instead and link the file",
		}
		cont = yield(f, nil)
		return cont
	})
	return
}
