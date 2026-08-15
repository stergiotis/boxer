package registry

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
)

// Tag values for this package's tests. A claim is global to the binary, so
// they are declared once here rather than per test, and they sit in the
// width-32 class well above the five in-tree vocabularies — the vcs-managed
// contract requires that class, and no committed vocabulary can reach these.
var (
	testClaimVcs       = tagmint.MustClaim("namemintRegistryTestVcs", 2178400, 1<<10)
	testClaimEphemeral = tagmint.MustClaim("namemintRegistryTestEphemeral", 2178401, 1<<10)
	testClaimReview    = tagmint.MustClaim("namemintRegistryTestReview", 2178402, 1<<10)
	testClaimShapes    = tagmint.MustClaim("namemintRegistryTestShapes", 2178403, 1<<10)
)
