package runtime

import (
	"github.com/stergiotis/boxer/public/observability/eh"
)

var ErrInvalidStateTransition = eh.Errorf("invalid state transition")

// ErrSingleMembershipViolated reports an attribute closing with a membership
// count other than one on a channel its section declares single-instance
// (ADR-0213). Raised by the generated completeAttribute path, so it surfaces
// like every DML error: on CheckErrors and CommitEntity. Match with errors.Is.
var ErrSingleMembershipViolated = eh.Errorf("the schema declares exactly one membership per attribute on this channel")
