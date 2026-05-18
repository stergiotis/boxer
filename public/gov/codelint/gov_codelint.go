//go:build llm_generated_opus47

package codelint

// FindingSeverityE classifies a codelint finding. Mirrors doclint's
// vocabulary so the lint.sh aggregator can apply the same warn/fail
// trailer rules.
type FindingSeverityE uint8

const (
	FindingSeverityInfo  FindingSeverityE = 1
	FindingSeverityWarn  FindingSeverityE = 2
	FindingSeverityError FindingSeverityE = 3
)

var AllFindingSeverities = []FindingSeverityE{
	FindingSeverityInfo,
	FindingSeverityWarn,
	FindingSeverityError,
}

func (inst FindingSeverityE) String() (s string) {
	switch inst {
	case FindingSeverityInfo:
		s = "info"
	case FindingSeverityWarn:
		s = "warn"
	case FindingSeverityError:
		s = "error"
	default:
		s = "unknown"
	}
	return
}

// Finding is a single rule violation discovered during a codelint pass.
//
// Line and Col are 1-based; zero means "not pinpointed within the file".
// The shape matches doclint.Finding so reporters can be shared structurally.
type Finding struct {
	RuleId   string           `json:"rule"`
	Severity FindingSeverityE `json:"severity"`
	Path     string           `json:"path"`
	Line     int32            `json:"line,omitempty"`
	Col      int32            `json:"col,omitempty"`
	Message  string           `json:"message"`
}
