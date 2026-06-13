package codelint

import (
	"fmt"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

// cs009Bans pairs each banned import path with the sanctioned
// replacement named in CODINGSTANDARDS.md "Packages to Use".
//
// Scope is deliberately narrow at v1: paths where the replacement is
// unambiguous and the cost of accidentally using the legacy form is
// real (cryptographic strength, JSON semantics, ID-collision profile,
// structured logging). Cases where multiple legitimate alternatives
// exist (`hash/fnv`, `flag`, raw `testing` assertions) are left out;
// they would benefit from per-file policy that this rule does not
// model.
//
// The env-var ban (os.Getenv / os.LookupEnv / syscall.Getenv vs
// public/config/env) is already enforced by env/lint_test.go and is
// not duplicated here.
var cs009Bans = map[string]string{
	"crypto/sha256":          "use lukechampine.com/blake3 (Packages to Use → cryptographic hash)",
	"crypto/sha1":            "use lukechampine.com/blake3 (Packages to Use → cryptographic hash)",
	"crypto/md5":             "use lukechampine.com/blake3 (Packages to Use → cryptographic hash)",
	"encoding/json":          "use encoding/json/v2 (Packages to Use → JSON)",
	"github.com/google/uuid": "use github.com/matoous/go-nanoid/v2 (Packages to Use → IDs)",
	"log":                    "use github.com/rs/zerolog (Packages to Use → structured logging)",
	"log/slog":               "use github.com/rs/zerolog (Packages to Use → structured logging)",
}

// RuleCS009 — banned imports.
type RuleCS009 struct{}

func NewRuleCS009() (inst *RuleCS009) {
	inst = &RuleCS009{}
	return
}

func (inst *RuleCS009) Id() (id string) {
	id = "CS009"
	return
}

func (inst *RuleCS009) DefaultSeverity() (sev FindingSeverityE) {
	sev = FindingSeverityWarn
	return
}

func (inst *RuleCS009) Analyzer() (a *analysis.Analyzer) {
	a = &analysis.Analyzer{
		Name: "cs009",
		Doc:  "CS009: import path is on the banned-list; use the sanctioned replacement",
		Run:  inst.run,
	}
	return
}

func (inst *RuleCS009) run(pass *analysis.Pass) (res any, err error) {
	for _, file := range pass.Files {
		for _, imp := range file.Imports {
			if imp.Path == nil {
				continue
			}
			path, perr := strconv.Unquote(imp.Path.Value)
			if perr != nil {
				continue
			}
			replacement, banned := cs009Bans[path]
			if !banned {
				continue
			}
			pass.Report(analysis.Diagnostic{
				Pos:     imp.Pos(),
				End:     imp.End(),
				Message: fmt.Sprintf("CS009: import %q is not allowed — %s", path, replacement),
			})
		}
	}
	return
}
