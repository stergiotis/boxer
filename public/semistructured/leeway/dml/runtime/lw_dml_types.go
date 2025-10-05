package runtime

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

type EntityStateE uint8

var _ fmt.Stringer = EntityStateE(0)

type InAttributeMembershipHighCardRefPI interface {
	AddMembershipHighCardRefP(highCardRef uint64)
}
type InAttributeMembershipHighCardRefParametrizedPI interface {
	AddMembershipHighCardRefParametrizedP(highCardRefParametrized []byte)
}
type InAttributeMembershipHighCardVerbatimPI interface {
	AddMembershipHighCardVerbatimP(highCardVerbatim []byte)
}
type InAttributeMembershipLowCardRefPI interface {
	AddMembershipLowCardRefP(lowCardRef uint64)
}
type InAttributeMembershipLowCardRefParametrizedPI interface {
	AddMembershipLowCardRefParametrizedP(lowCardRefParametrized []byte)
}
type InAttributeMembershipLowCardVerbatimPI interface {
	AddMembershipLowCardVerbatimP(lowCardVerbatim []byte)
}
type InAttributeMembershipMixedLowCardRefPI interface {
	AddMembershipMixedLowCardRefP(lowCardRef uint64, params []byte)
}
type InAttributeMembershipMixedLowCardVerbatimPI interface {
	AddMembershipMixedLowCardVerbatimP(lowCardVerbatim uint64, params []byte)
}

type InAttributeMembershipHighCardRefI[A any] interface {
	AddMembershipHighCardRef(highCardRef uint64) A
}
type InAttributeMembershipHighCardRefParametrizedI[A any] interface {
	AddMembershipHighCardRefParametrized(highCardRefParametrized []byte) A
}
type InAttributeMembershipHighCardVerbatimI[A any] interface {
	AddMembershipHighCardRef(highCardVerbatim []byte) A
}
type InAttributeMembershipLowCardRefI[A any] interface {
	AddMembershipLowCardRef(lowCardRef uint64) A
}
type InAttributeMembershipLowCardRefParametrizedI[A any] interface {
	AddMembershipLowCardRefParametrized(lowCardRefParametrized []byte) A
}
type InAttributeMembershipLowCardVerbatimI[A any] interface {
	AddMembershipLowCardVerbatim(lowCardVerbatim []byte) A
}
type InAttributeMembershipMixedLowCardRefI[A any] interface {
	AddMembershipMixedLowCardRef(lowCardRef uint64, params []byte) A
}
type InAttributeMembershipMixedLowCardVerbatimI[A any] interface {
	AddMembershipMixedLowCardVerbatim(lowCardVerbatim uint64, params []byte) A
}
type ErrorAppenderI interface {
	AppendError(err error)
}
type ErrorCheckerI interface {
	CheckErrors() (err error)
}
type ErrorHandlingI interface {
	ErrorAppenderI
	ErrorCheckerI
}

type InAttributeI[E any, S any, A any] interface {
	EndAttribute() S
	EndSection() E
}
type InSectionI[E any, S any] interface {
	ErrorHandlingI

	EndSection() E
}
type InEntity[E any] interface {
	ErrorHandlingI

	CommitEntity() error
	RollbackEntity() error

	TransferRecords(recordsIn []arrow.Record) (recordsOut []arrow.Record, err error)
	GetSchema() (schema *arrow.Schema)
}
