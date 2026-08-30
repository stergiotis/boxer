// Package buildtags verifies a repository's ./tags file against the build-tag
// contract boxer publishes through its module pin.
//
// The problem it solves is drift in the one layer of boxer's standards that a
// consuming repository must copy rather than import. Standards text, document
// templates, lint rules and library conventions all reach a consumer through
// the go.mod pin and cannot go stale; ./tags is a hand-copied file, and two
// consumer repositories were observed carrying tag families boxer had retired
// months earlier — one of them mandating the retired marker in its own accepted
// ADR. See doc/adr-background-work/downstream-adoption-skeleton.md.
//
// This package closes the build-tag half of that problem. ADR-0179 decides the
// other two halves — `gov gate` for the lint step list and `gov skeleton` for
// the remaining copied files — and names this package's tests in its
// verification plan; it does not otherwise govern the code here.
//
// The contract has three sets. Required tags must be present or boxer's own
// packages will not compile in the consumer. Optional tags are recognised
// opt-ins that a consumer may set or omit. Retired tags must not be present at
// all; each carries the ADR that retired it, so a finding names the decision
// rather than merely the symptom. Anything else is repo-local and passes
// silently — a consumer's own tags are none of boxer's business.
//
// The sets live here rather than in a file beside ./tags so that a consumer
// gets them from the module pin without resolving boxer's on-disk root.
// TestRequiredAndOptionalMatchTagsFile keeps them honest against boxer's own
// ./tags.
package buildtags

import (
	"iter"
	"path"
	"strings"
)

// RequiredTags must appear in every consumer's tag set. A tag belongs here only
// when boxer's own packages fail to compile without it.
//
// It is empty, and has been since encoding/json/v2 graduated in Go 1.27
// (ADR-0199): a consumer needs no build tags at all, which is what opens `go
// tool` delivery of boxer's own CLI to one. The set is kept rather than deleted
// because the mechanism is not retired with its last member — a future
// experiment gate would land here again, and [Check] must keep reporting a
// missing one. TestCheckReportsMissingRequired substitutes a set for exactly
// that reason.
var RequiredTags = []string{}

// OptionalTags are recognised opt-ins a consumer may set or omit.
//
// It is empty, and has been since ADR-0212 split the pprof HTTP listener into
// its own package and retired boxer_enable_profiling. As with RequiredTags the
// set is kept rather than deleted: the next feature whose cost is worth
// compiling out lands here, and [Check] must go on accepting a declared opt-in
// silently. TestCheckAcceptsOptional substitutes a set for that reason.
var OptionalTags = []string{}

// RetiredTag names a tag boxer no longer honours, together with the decision
// that retired it.
//
// Pattern is matched with [path.Match], so a family retired as a whole is one
// entry (e.g. "llm_generated_*") rather than one per member.
type RetiredTag struct {
	Pattern string
	Adr     string
	Retired string
}

// RetiredTags is append-only: an entry is removed only if a retirement is
// itself reversed, which would be a new decision.
var RetiredTags = []RetiredTag{
	{
		Pattern: "identifier_tag_fixed*",
		Adr:     "ADR-0106",
		Retired: "2026-07",
	},
	{
		Pattern: "llm_generated_*",
		Adr:     "ADR-0083",
		Retired: "2026-06",
	},
	{
		Pattern: "goexperiment.jsonv2",
		Adr:     "ADR-0199",
		Retired: "2026-08",
	},
	{
		Pattern: "boxer_enable_profiling",
		Adr:     "ADR-0212",
		Retired: "2026-08",
	},
}

type FindingKindE uint8

const (
	FindingKindMissing FindingKindE = 1
	FindingKindRetired FindingKindE = 2
)

var AllFindingKinds = []FindingKindE{
	FindingKindMissing,
	FindingKindRetired,
}

func (inst FindingKindE) String() (s string) {
	switch inst {
	case FindingKindMissing:
		s = "missing"
	case FindingKindRetired:
		s = "retired"
	default:
		s = "unknown"
	}
	return
}

// Finding is one violation of the tag contract.
//
// Adr and Retired are populated for FindingKindRetired only.
type Finding struct {
	Kind    FindingKindE
	Tag     string
	Adr     string
	Retired string
}

func (inst Finding) Message() (s string) {
	var b strings.Builder
	b.Grow(96)
	switch inst.Kind {
	case FindingKindMissing:
		b.WriteString("missing required tag ")
		b.WriteString(inst.Tag)
		b.WriteString(" — boxer will not compile without it")
	case FindingKindRetired:
		b.WriteString("retired tag ")
		b.WriteString(inst.Tag)
		b.WriteString(" — retired by ")
		b.WriteString(inst.Adr)
		b.WriteString(" (")
		b.WriteString(inst.Retired)
		b.WriteString("); remove it")
	}
	s = b.String()
	return
}

// ParseTags splits the contents of a ./tags file into its members.
//
// The file is a single comma-separated line by convention, but a trailing
// newline, surrounding whitespace and empty members are all tolerated so that a
// hand-edited file does not fail on formatting.
func ParseTags(contents string) (tags []string) {
	raw := strings.Split(contents, ",")
	tags = make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		tags = append(tags, t)
	}
	return
}

// Check yields every violation of the tag contract in tags.
//
// Findings are yielded missing-first, then retired in RetiredTags order, so
// output is stable across runs regardless of the order of the input.
func Check(tags []string) iter.Seq[Finding] {
	return func(yield func(Finding) bool) {
		present := make(map[string]struct{}, len(tags))
		for _, t := range tags {
			present[t] = struct{}{}
		}

		for _, req := range RequiredTags {
			if _, ok := present[req]; ok {
				continue
			}
			if !yield(Finding{Kind: FindingKindMissing, Tag: req}) {
				return
			}
		}

		for _, r := range RetiredTags {
			for _, t := range tags {
				// A malformed pattern cannot match, which is the safe
				// direction: a bad entry under-reports rather than
				// failing every consumer's build.
				matched, err := path.Match(r.Pattern, t)
				if err != nil || !matched {
					continue
				}
				f := Finding{
					Kind:    FindingKindRetired,
					Tag:     t,
					Adr:     r.Adr,
					Retired: r.Retired,
				}
				if !yield(f) {
					return
				}
			}
		}
	}
}

// GoFlags renders tags as a GOFLAGS assignment, the form gopls, direnv and CI
// consume. The ./tags file stays the source of truth; this is the derived form.
func GoFlags(tags []string) (s string) {
	return "GOFLAGS=-tags=" + strings.Join(tags, ",")
}
