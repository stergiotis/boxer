//go:build llm_generated_opus48

package docstd

import (
	"slices"
	"strings"
)

// Diátaxis document types — the canonical `type:` front-matter values
// from DOCUMENTATION_STANDARD §4. The first four are the reader-facing
// content quadrants; [TypeADR] is design history, accepted repo-wide but
// not in operator-facing help corpora.
const (
	TypeReference   = "reference"
	TypeHowTo       = "how-to"
	TypeExplanation = "explanation"
	TypeTutorial    = "tutorial"
	TypeADR         = "adr"
)

// Descriptive-doc lifecycle statuses (README / EXPLANATION / HOWTO /
// TUTORIAL). ADRs use the [StatusProposed] set instead; [StatusDeprecated]
// and [StatusSuperseded] are shared by both.
const (
	StatusDraft      = "draft"
	StatusStable     = "stable"
	StatusDeprecated = "deprecated"
	StatusSuperseded = "superseded"
)

// ADR-only lifecycle statuses. [StatusDeprecated] and [StatusSuperseded]
// from the descriptive set are also valid for ADRs.
const (
	StatusProposed = "proposed"
	StatusAccepted = "accepted"
	StatusDeferred = "deferred"
)

// contentTypes is the reader-facing quadrant set (no adr), in the order
// used to render the "is not one of: …" message.
var contentTypes = []string{TypeReference, TypeHowTo, TypeExplanation, TypeTutorial}

// allTypes is contentTypes plus adr — the full repo-wide accepted set.
var allTypes = []string{TypeReference, TypeHowTo, TypeExplanation, TypeTutorial, TypeADR}

// descriptiveStatuses and adrStatuses are the lifecycle enums keyed by
// document type. deprecated/superseded appear in both.
var (
	descriptiveStatuses = []string{StatusDraft, StatusStable, StatusDeprecated, StatusSuperseded}
	adrStatuses         = []string{StatusProposed, StatusAccepted, StatusDeferred, StatusDeprecated, StatusSuperseded}
)

// IsContentType reports whether t is a Diátaxis content type — one of the
// reader-facing quadrants (reference, how-to, explanation, tutorial),
// excluding adr. Inline help corpora are restricted to these.
func IsContentType(t string) (ok bool) {
	ok = slices.Contains(contentTypes, t)
	return
}

// IsType reports whether t is any documentation-standard type, including
// adr. This is the full set repo-wide linting accepts.
func IsType(t string) (ok bool) {
	ok = slices.Contains(allTypes, t)
	return
}

// IsStatusForType reports whether status is a valid lifecycle state for a
// doc of the given type. ADRs ([TypeADR]) use the proposed/accepted/
// deferred set; every other type (including an empty or unknown one) uses
// the descriptive set, matching the standard's required-field precedence.
func IsStatusForType(docType string, status string) (ok bool) {
	if docType == TypeADR {
		ok = slices.Contains(adrStatuses, status)
		return
	}
	ok = slices.Contains(descriptiveStatuses, status)
	return
}

// Violation is one front-matter contract breach for a (type, status)
// pair, free of consumer-specific concerns like file path, line number,
// or severity. The two enforcers ([github.com/stergiotis/boxer/public/gov/doclint]
// and the keelson help library) map it onto their own finding type.
type Violation struct {
	// Field is the offending front-matter key: "type" or "status".
	Field string
	// Value is the offending value, or "" when the field is absent.
	Value string
	// Message is a self-contained, human-readable description of the
	// breach, suitable for display after a "<file>: " prefix.
	Message string
}

// ValidateFrontmatter checks a document's `type` and `status` front-matter
// values against DOCUMENTATION_STANDARD §4 and returns one [Violation] per
// breach. An empty result means the pair is conformant.
//
// allowADR selects the accepted type set: repository-wide doc linting
// passes true (adr is a valid type); operator-facing inline help passes
// false, since an ADR is design history, not help. The status enum is
// keyed on the document's type via [IsStatusForType].
//
// Violations are returned in field order (type, then status). A missing
// `status` short-circuits the status-enum check — the only thing to say
// about an absent value is that it is required.
func ValidateFrontmatter(docType string, status string, allowADR bool) (vs []Violation) {
	typeOK := IsContentType(docType) || (allowADR && docType == TypeADR)
	if docType == "" {
		vs = append(vs, Violation{
			Field:   "type",
			Message: "front-matter missing required field 'type'",
		})
	} else if !typeOK {
		vs = append(vs, Violation{
			Field:   "type",
			Value:   docType,
			Message: "front-matter 'type' value '" + docType + "' is not one of: " + typeList(allowADR),
		})
	}

	if status == "" {
		vs = append(vs, Violation{
			Field:   "status",
			Message: "front-matter missing required field 'status'",
		})
		return
	}
	if !IsStatusForType(docType, status) {
		vs = append(vs, Violation{
			Field:   "status",
			Value:   status,
			Message: "front-matter 'status' value '" + status + "' is not valid for type '" + docType + "'",
		})
	}
	return
}

// typeList renders the accepted type spellings for the "is not one of: …"
// message, including adr only when allowADR is set.
func typeList(allowADR bool) (s string) {
	if allowADR {
		s = strings.Join(allTypes, ", ")
		return
	}
	s = strings.Join(contentTypes, ", ")
	return
}
