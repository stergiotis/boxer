package doclint

import (
	"bytes"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RuleDL004 — draft banner present iff status is draft / proposed.
//
// Implements DOCUMENTATION_STANDARD §4: every doc whose status declares it
// pre-human-review must announce that fact with a leading blockquote. Docs
// that are stable / accepted must not carry such a banner. The detected
// state in the banner must match the front-matter status.
//
// Detection is prefix-based on the standard's recognisable opening
// "**Status: <state> — pre-human-review.**" so author-supplied trailing
// prose (e.g. "; migrated from FFFI.md") does not break the check.
//
// Files that DL001 would already flag are silently skipped to avoid
// derived noise.
type RuleDL004 struct{}

func NewRuleDL004() (inst *RuleDL004) {
	inst = &RuleDL004{}
	return
}

func (inst *RuleDL004) Id() (id string) {
	id = "DL004"
	return
}

func (inst *RuleDL004) Check(roots []string) iter.Seq2[Finding, error] {
	return func(yield func(Finding, error) bool) {
		for _, root := range roots {
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					if shouldSkipDir(d.Name()) {
						return filepath.SkipDir
					}
					return nil
				}
				base := filepath.Base(path)
				if !strings.HasSuffix(strings.ToLower(base), ".md") {
					return nil
				}
				if !IsInScopeForDL001(path, base) {
					return nil
				}
				cont, fErr := checkOneDL004(path, yield)
				if fErr != nil {
					return fErr
				}
				if !cont {
					return filepath.SkipAll
				}
				return nil
			})
			if err != nil {
				yield(Finding{}, eb.Build().Str("root", root).Errorf("DL004 walk: %w", err))
				return
			}
		}
	}
}

func checkOneDL004(path string, yield func(Finding, error) bool) (cont bool, err error) {
	cont = true
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("DL004 read: %w", err)
		return
	}
	meta, body, ok, parseErr := parseMdFrontMatter(data)
	if !ok || parseErr != nil {
		return
	}

	bannerExpected := false
	expectedState := ""
	switch meta.Status {
	case "draft", "proposed":
		bannerExpected = true
		expectedState = meta.Status
	case "stable", "accepted":
		bannerExpected = false
	default:
		// deprecated / superseded / unknown — DL004 does not constrain the
		// banner for these.
		return
	}

	bannerFound, foundState := DetectStatusBanner(body)

	if bannerExpected && !bannerFound {
		f := Finding{
			RuleId:   "DL004",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "status '" + meta.Status + "' requires a leading blockquote starting with '**Status: " + expectedState + " — pre-human-review.**'",
		}
		cont = yield(f, nil)
		return
	}

	if bannerExpected && bannerFound && foundState != expectedState {
		f := Finding{
			RuleId:   "DL004",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "banner announces '" + foundState + "' but front-matter status is '" + meta.Status + "'",
		}
		cont = yield(f, nil)
		return
	}

	if !bannerExpected && bannerFound {
		f := Finding{
			RuleId:   "DL004",
			Severity: FindingSeverityError,
			Path:     path,
			Line:     1,
			Col:      1,
			Message:  "status '" + meta.Status + "' must not display a draft/proposed banner; remove the leading '**Status: " + foundState + " — pre-human-review.**' blockquote",
		}
		cont = yield(f, nil)
	}
	return
}

// bannerStates lists the status words DetectStatusBanner will recognise as a
// pre-human-review banner. Only draft and proposed are operationally
// meaningful; the rest are accepted as detection-only so a wrong banner on
// a stable/accepted doc still surfaces as a banner rather than being
// silently invisible.
var bannerStates = []string{"draft", "proposed", "stable", "accepted", "deprecated", "superseded"}

// DetectStatusBanner inspects body for a leading status banner blockquote.
// It returns whether one was found and which state it announces.
//
// A banner is the first non-empty content line, must begin with '>', and
// after stripping leading '> '/whitespace must start with the canonical
// "**Status: <state> — pre-human-review.**" prefix.
func DetectStatusBanner(body []byte) (found bool, state string) {
	for _, raw := range bytes.Split(body, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte(">")) {
			return
		}
		text := bytes.TrimLeft(line, "> \t")
		for _, candidate := range bannerStates {
			prefix := []byte("**Status: " + candidate + " — pre-human-review.**")
			if bytes.HasPrefix(text, prefix) {
				found = true
				state = candidate
				return
			}
		}
		return
	}
	return
}
