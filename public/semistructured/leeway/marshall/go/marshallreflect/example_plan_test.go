package marshallreflect_test

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// checkout is the logical shape of one event. The lw: tag names the
// membership (the attribute label), the section the value is stored in, and
// the membership channel; an empty membership marks a plain column.
type checkout struct {
	_     struct{}  `kind:"checkout"`
	Id    uint64    `lw:",id"`
	Ts    time.Time `lw:",ts"`
	Event string    `lw:"event,symbol,verbatim"`
	Geo   string    `lw:"user.geo,symbol,verbatim"`
	Total float64   `lw:"cart.total,f64,verbatim"`
}

// ExamplePlanFor derives the mapping plan of a DTO from its struct tags. The
// plan is the logical side of the mapping; it names sections and memberships
// but not physical columns, so the same DTO can be marshalled into any table
// that has those sections.
func ExamplePlanFor() {
	plan, err := marshallreflect.PlanFor[checkout]()
	if err != nil {
		panic(err)
	}
	fmt.Println("kind:", plan.KindName)
	for _, c := range plan.PlainCols {
		fmt.Println("plain:", c.Column)
	}
	for _, f := range plan.Fields {
		fmt.Println("tagged:", f.LWSection, f.LWMembership, f.Canonical)
	}
	// Output:
	// kind: checkout
	// plain: id
	// plain: ts
	// tagged: symbol event s
	// tagged: symbol user.geo s
	// tagged: f64 cart.total f64
}
