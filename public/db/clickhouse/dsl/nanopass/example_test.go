package nanopass_test

import (
	"fmt"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// ExampleSequence composes passes into a pipeline that takes SQL text and
// returns SQL text, each pass parsing what the last produced. Passes rewrite
// tokens and leave the surrounding whitespace, so a removed comment leaves
// its spaces behind.
func ExampleSequence() {
	pipeline := nanopass.Sequence("tidy",
		passes.StripComments,
		passes.CanonicalizeKeywordCase,
		passes.QualifyTables("analytics"),
	)
	out, err := pipeline.Run("select a /* the key */ from events where b = 1")
	if err != nil {
		panic(err)
	}
	fmt.Println(out)
	// Output:
	// SELECT a   FROM analytics.events WHERE b = 1
}

// ExampleParse returns the concrete syntax tree of a statement, which the
// analysis package reads; a syntax error is an error, not a partial tree.
func ExampleParse() {
	pr, err := nanopass.Parse("SELECT count() FROM t GROUP BY k")
	if err != nil {
		panic(err)
	}
	fmt.Println(pr.Tree != nil)
	_, err = nanopass.Parse("SELECT FROM WHERE")
	fmt.Println(err != nil)
	// Output:
	// true
	// true
}
