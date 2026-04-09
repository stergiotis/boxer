//go:build llm_generated_opus46

package repo

import (
	"context"
	"iter"
	"regexp"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type FirefightKindE uint8

const (
	FirefightKindRevert    FirefightKindE = 1
	FirefightKindHotfix    FirefightKindE = 2
	FirefightKindEmergency FirefightKindE = 3
)

var AllFirefightKinds = []FirefightKindE{FirefightKindRevert, FirefightKindHotfix, FirefightKindEmergency}

func (inst FirefightKindE) String() (s string) {
	switch inst {
	case FirefightKindRevert:
		s = "revert"
	case FirefightKindHotfix:
		s = "hotfix"
	case FirefightKindEmergency:
		s = "emergency"
	default:
		s = "unknown"
	}
	return
}

type FirefightRecord struct {
	Hash    string
	Author  string
	Date    string
	Subject string
	Kind    FirefightKindE
}

type FirefightAnalyzer struct {
	Since            string
	Until            string
	RevertPattern    string
	HotfixPattern    string
	EmergencyPattern string
}

type classifiedPattern struct {
	re   *regexp.Regexp
	kind FirefightKindE
}

func (inst *FirefightAnalyzer) compilePatterns() (patterns []classifiedPattern, err error) {
	type entry struct {
		raw      string
		fallback string
		kind     FirefightKindE
	}
	entries := []entry{
		{inst.RevertPattern, `(?i)revert`, FirefightKindRevert},
		{inst.HotfixPattern, `(?i)hotfix`, FirefightKindHotfix},
		{inst.EmergencyPattern, `(?i)(emergency|urgent|critical|rollback)`, FirefightKindEmergency},
	}

	patterns = make([]classifiedPattern, 0, len(entries))
	for _, e := range entries {
		p := e.raw
		if p == "" {
			p = e.fallback
		}
		var re *regexp.Regexp
		re, err = regexp.Compile(p)
		if err != nil {
			err = eh.Errorf("unable to compile pattern %q: %w", p, err)
			return
		}
		patterns = append(patterns, classifiedPattern{re: re, kind: e.kind})
	}
	return
}

func (inst *FirefightAnalyzer) Run(ctx context.Context, git *GitRunner) iter.Seq2[FirefightRecord, error] {
	return func(yield func(FirefightRecord, error) bool) {
		patterns, err := inst.compilePatterns()
		if err != nil {
			yield(FirefightRecord{}, err)
			return
		}

		const sep = "\x1f" // ASCII unit separator
		format := "%H" + sep + "%an" + sep + "%ad" + sep + "%s"
		args := git.buildLogArgs(format, inst.Since, inst.Until, "--date=short")
		for line, lineErr := range git.RunLines(ctx, args...) {
			if lineErr != nil {
				yield(FirefightRecord{}, eh.Errorf("unable to read git log: %w", lineErr))
				return
			}
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, sep, 4)
			if len(parts) != 4 {
				continue
			}
			subject := parts[3]
			for _, cp := range patterns {
				if cp.re.MatchString(subject) {
					if !yield(FirefightRecord{
						Hash:    parts[0],
						Author:  parts[1],
						Date:    parts[2],
						Subject: subject,
						Kind:    cp.kind,
					}, nil) {
						return
					}
					break
				}
			}
		}
	}
}
