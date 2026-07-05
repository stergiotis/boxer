package identsql_test

import (
	"fmt"

	"github.com/stergiotis/boxer/public/identity/identsql"
)

// The constant-tag form folds into a sargable range at expansion time: no bit
// arithmetic reaches the query, and primary-index analysis can prune parts.
func ExampleExpandPass() {
	out, err := identsql.ExpandPass.Run("SELECT LW_ID_HAS_TAG(id, 12) FROM t")
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output: SELECT ((id) BETWEEN 12393906174523604992 AND 12682136550675316735) FROM t
}

// Every LW_ID_* macro has a CREATE FUNCTION twin with the same name and body,
// so unexpanded SQL runs against a server that has them installed (emit them
// with `app leeway id udf`). The validity probe is the smallest one.
func ExampleUdfDdlStatements() {
	fmt.Println(identsql.UdfDdlStatements()[0])
	// Output: CREATE OR REPLACE FUNCTION LW_ID_IS_VALID AS (x) -> (bitAnd(x, bitShiftLeft(x, 1)) != 0)
}
