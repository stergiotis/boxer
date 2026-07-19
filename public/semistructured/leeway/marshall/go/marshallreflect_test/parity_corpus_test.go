package marshallreflect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// The front-end parity corpus (ADR-0113, Verification): the byte-identity
// invariant is only defined over DTOs BOTH front-ends accept, so the accept
// sets themselves need a mechanical gate — parity asserted by mirrored
// comments has already drifted twice (the `*S` and unexported-field
// acceptance defects, ADR-0113 review fallout).
//
// Every corpus DTO lives in its own source file in THIS package, so one
// declaration serves both front-ends: the compiler hands
// marshallreflect.PlanFor its type, and marshallgen.ParsePlan parses the very
// same file (go test runs with the package directory as cwd). The gate
// asserts identical accept / reject decisions and, where both accept, equal
// plans. A DELIBERATE divergence must cite its documentation via asymmetry;
// an undocumented divergence fails.
type parityCase struct {
	name string
	file string                            // corpus source file, also compiled into this package
	plan func() (*mappingplan.Plan, error) // marshallreflect.PlanFor[T] for the file's DTO

	genErr     string // "" ⇒ marshallgen must accept; else a substring of its error
	reflectErr string // "" ⇒ marshallreflect must accept; else a substring of its error
	asymmetry  string // doc reference; required iff exactly one front-end rejects
}

var parityCases = []parityCase{
	{
		name: "simple-subset",
		file: "parity_dto_simple_test.go",
		plan: func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[paritySimple]() },
	},
	{
		name: "flag-tokens",
		file: "parity_dto_flags_test.go",
		plan: func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityFlags]() },
	},
	{
		name: "dynamic-membership-tuple",
		file: "parity_dto_tuple_test.go",
		plan: func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityTupleDoc]() },
	},
	{
		name: "nested-cardinalities",
		file: "parity_dto_nested_test.go",
		plan: func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityNested]() },
	},
	{
		name:       "reject-unexported-tagged-field",
		file:       "parity_dto_reject_unexported_test.go",
		plan:       func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityRejectUnexported]() },
		genErr:     "unexported field carries an `lw:` tag",
		reflectErr: "unexported field carries an `lw:` tag",
	},
	{
		name:       "reject-untagged-field",
		file:       "parity_dto_reject_untagged_test.go",
		plan:       func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityRejectUntagged]() },
		genErr:     "untagged DTO field",
		reflectErr: "missing `lw:` tag",
	},
	{
		name:       "reject-scalar-pointer",
		file:       "parity_dto_reject_scalarptr_test.go",
		plan:       func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityRejectScalarPtr]() },
		genErr:     "pointer types forbidden",
		reflectErr: "pointer types forbidden",
	},
	{
		name:      "asym-star-nested-optional",
		file:      "parity_dto_asym_starnested_test.go",
		plan:      func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityAsymStarNested]() },
		genErr:    "pointer types forbidden",
		asymmetry: "marshalling how-to, deferred surfaces: reflect accepts `*S` as Optional; codegen rejects it (scalar-pointer policy — its Optional emit arms assume option.Option[S]; ADR-0113 review fallout)",
	},
	{
		name:      "asym-entity-level-lane-marker",
		file:      "parity_dto_asym_lane_test.go",
		plan:      func() (*mappingplan.Plan, error) { return marshallreflect.PlanFor[parityAsymLane]() },
		genErr:    "not yet supported by the codegen front-end",
		asymmetry: "value-marker bridge is reflect-only (deferred surface, ADR-0113 D3): reflect relabels the canonical from the lw lane type; codegen names the gap explicitly",
	},
}

func TestFrontEndParity_Corpus(t *testing.T) {
	for _, c := range parityCases {
		t.Run(c.name, func(t *testing.T) {
			genPlan, genErr := marshallgen.ParsePlan(c.file)
			refPlan, refErr := c.plan()

			// Table sanity: a divergent expectation must cite documentation;
			// a symmetric one must not carry an asymmetry note.
			if (c.genErr == "") != (c.reflectErr == "") {
				require.NotEmpty(t, c.asymmetry, "front-end accept/reject divergence must cite its documentation")
			} else {
				require.Empty(t, c.asymmetry, "asymmetry note on a non-divergent case")
			}

			if c.genErr == "" {
				require.NoErrorf(t, genErr, "marshallgen must accept %s", c.file)
			} else {
				require.Errorf(t, genErr, "marshallgen must reject %s", c.file)
				require.ErrorContains(t, genErr, c.genErr)
			}
			if c.reflectErr == "" {
				require.NoErrorf(t, refErr, "marshallreflect must accept %s", c.file)
			} else {
				require.Errorf(t, refErr, "marshallreflect must reject %s", c.file)
				require.ErrorContains(t, refErr, c.reflectErr)
			}

			if c.genErr == "" && c.reflectErr == "" {
				g, r := *genPlan, *refPlan
				// The origin strings legitimately differ: gen records the
				// source-file path and the file's package clause, reflect the
				// package path + type name — and this directory holds only
				// _test.go files, so its types compile under the external
				// test-package path (`…_test`). Everything else must be equal.
				g.InputPath, r.InputPath = "", ""
				g.PackageName, r.PackageName = "", ""
				require.Equal(t, g, r, "front-ends must produce equal plans for %s", c.file)
			}
		})
	}
}
