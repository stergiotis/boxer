//go:build llm_generated_opus47

package codelint

import (
	"golang.org/x/tools/go/analysis"
)

// RuleI is implemented by every codelint rule.
//
// Each rule exposes a go/analysis Analyzer that the driver runs once per
// loaded package. Severity is rule-supplied so the driver can label the
// translated Finding without rule-specific glue.
type RuleI interface {
	Id() (id string)
	DefaultSeverity() (sev FindingSeverityE)
	Analyzer() (a *analysis.Analyzer)
}
