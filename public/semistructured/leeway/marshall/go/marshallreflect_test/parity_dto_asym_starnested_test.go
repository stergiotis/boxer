package marshallreflect_test

import "time"

// parityStarWindow / parityAsymStarNested: the `*S` nested-Optional spelling —
// a DOCUMENTED front-end asymmetry (marshalling how-to, deferred surfaces).
// Reflect accepts the pointer as a second Optional spelling; codegen rejects
// it under the scalar-pointer policy, because its Optional emit arms assume
// option.Option[S] and would emit non-compiling code for a pointer
// (ADR-0113 review fallout).
type parityStarWindow struct {
	Begin time.Time `lw:"beginIncl"`
	End   time.Time `lw:"endExcl"`
}

type parityAsymStarNested struct {
	_   struct{}          `kind:"parityAsymStarNested"`
	ID  uint64            `lw:",id"`
	Win *parityStarWindow `lw:"win,timeRange"`
}
