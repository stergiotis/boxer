package sessiondata

import (
	"errors"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrUnattested refuses an agent-caused request whose on-behalf-of context
// the dispatcher does not confirm, or that no dispatcher can confirm.
var ErrUnattested = errors.New("on-behalf-of context not attested by the dispatcher")

// attest confirms c's agent call with the dispatcher: the call was routed
// to c's window, in a task live at its epoch. A request with no agent call
// needs nothing. A host whose trail must be durable and is not refuses
// agent-caused work, as the dataset service does.
func (inst *Service) attest(c Caller) (cc option.Option[app.CallContext], err error) {
	obo := c.OnBehalfOf
	if obo == nil || obo.Task == "" {
		return
	}
	ref := inst.dispatcher.Load()
	if ref == nil || ref.d == nil {
		return cc, eb.Build().Str("task", obo.Task).Errorf("no dispatcher to attest the call: %w", ErrUnattested)
	}
	got, ok, why := ref.d.CallContext(obo.Task, obo.Epoch, obo.Call, c.By.App, c.By.Instance)
	if !ok {
		return cc, eb.Build().Str("task", obo.Task).Str("call", obo.Call).Errorf("%w: %s", ErrUnattested, why) //boxer:lint disable=CS013 reason="the refusal crosses the bus as text; the dispatcher's reason is what the caller can act on"
	}
	if refuse := inst.trail.Admit(); refuse != nil {
		return cc, eb.Build().Str("task", obo.Task).Errorf("agent-caused session data: %w", refuse)
	}
	return option.Some(got), nil
}

// allow asks the dispatcher whether c's task's grant lists every
// destination, refusing with the first it does not. The person's request
// needs none.
func (inst *Service) allow(c Caller, destinations ...string) (err error) {
	obo := c.OnBehalfOf
	if obo == nil || obo.Task == "" {
		return
	}
	ref := inst.dispatcher.Load()
	if ref == nil || ref.d == nil {
		return eb.Build().Str("task", obo.Task).Errorf("no dispatcher to check the grant: %w", ErrUnattested)
	}
	for _, d := range destinations {
		if ok, why := ref.d.AllowDestination(obo.Task, obo.Epoch, d); !ok {
			return &GrantError{Destination: d, Reason: "the grant does not list " + d + " (" + why + ")"}
		}
	}
	return
}
