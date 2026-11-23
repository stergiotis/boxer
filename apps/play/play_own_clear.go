package play

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// Two properties of a run that reads sealed data are kept apart. Whether it
// names or attaches sealed data decides where it may go: the dispatch
// decision's sensitivity, the reach proof, the request's Sensitivity and
// the engine's own refusal, none of which asks who published the data.
// Whether its result is labelled as derived from sealed data is decided
// after the wall: a task that reads back what its own call published at
// TLP:CLEAR gets no label for it. Play does not decide that; the dataset
// service does, for the revision read, and play takes its answer.

// ownClearLookupTimeout bounds asking the dataset service whether a handle
// a run read is the reading task's own publication.
const ownClearLookupTimeout = 2 * time.Second

// ownClearRun reports whether everything sealed a run read is the reading
// task's own publication at TLP:CLEAR, so its result carries no label.
// sensitivity is what the request was sent as, residual the statement as
// it shipped, agent the task's call the run is work of and hold the
// binding of a run in a data session, nil otherwise.
//
// A run in a data session takes the session data service's answer, which
// covers the datasets it attached and the data sets it read. A run on this
// process's plane names each sealed dataset by its handle and asks the
// dataset service about each. Every other case keeps the label: the
// person's run, a run that read no sealed data, a sealed name that is no
// ad-hoc handle, a client without a bus, a service that does not answer.
func (inst *Client) ownClearRun(ctx context.Context, sensitivity queryengine.SensitivityE, residual string, agent *app.OnBehalfOf, hold *sessionHold) (ownClear bool) {
	if sensitivity != queryengine.SensitivityConfined || !hasTask(agent) {
		return false
	}
	names := labelledNames(residual)
	if hold != nil {
		// The binding replaced every keelson('…') it attached, so a sealed
		// name left in the bound statement is one the service did not
		// judge.
		return hold.b.OwnClear && len(names) == 0
	}
	return inst.ownClearHandles(ctx, names, agent)
}

// labelledNames are the names of residual a run's label is judged by:
// every table it names that is sealed on this process's plane now, and
// every name with the shape of an ad-hoc dataset's handle whether or not it
// is still registered. The label is asked for once the run has answered,
// and a handle is unregistered a grace after its dataset left: a run that
// read it meanwhile holds its rows, so the name still counts, and the
// dataset service answers that it no longer holds it.
func labelledNames(residual string) (names []string) {
	names = sealedNames(residual)
	for _, candidates := range [][]string{keelsonsql.References(residual), plainTables(residual)} {
		for _, name := range candidates {
			if adhocdata.IsHandle(name) && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return
}

// ownClearHandles asks the dataset service whether every name is an ad-hoc
// dataset's handle that is agent's task's own publication at TLP:CLEAR.
// No name, a name that is no handle and an answer that does not arrive are
// all false. It asks over the bus, so it is not called on the render
// goroutine.
func (inst *Client) ownClearHandles(ctx context.Context, names []string, agent *app.OnBehalfOf) (ownClear bool) {
	bus := inst.sessionBus()
	if len(names) == 0 || bus == nil || !hasTask(agent) {
		return false
	}
	for _, name := range names {
		if !adhocdata.IsHandle(name) {
			return false
		}
		rctx, cancel := context.WithTimeout(ctx, ownClearLookupTimeout)
		res, err := adhocdata.ReadLabelRequest(rctx, bus, name, agent)
		cancel()
		if err != nil || !res.OwnClear {
			return false
		}
	}
	return true
}

// ownClearError is the failure of a run that named nothing sealed but the
// reading task's own publications at TLP:CLEAR. It reads as the error it
// wraps; a caller that labels a failed run asks failedOwnClear.
type ownClearError struct {
	err error
}

func (inst *ownClearError) Error() string { return inst.err.Error() }

func (inst *ownClearError) Unwrap() error { return inst.err }

// failedOwnClear reports whether err is the failure of a run that named
// nothing sealed but the reading task's own publications at TLP:CLEAR, so
// the failure is not labelled. Any other error — a refusal before the run
// was placed, a run that named other sealed data, one whose label the
// services did not answer — leaves the label of the statement.
func failedOwnClear(err error) (ownClear bool) {
	var oc *ownClearError
	return errors.As(err, &oc)
}

// markOwnClearFailure is err marked for failedOwnClear when the run that
// failed with it would have been unlabelled had it succeeded; err itself
// otherwise.
func (inst *Client) markOwnClearFailure(ctx context.Context, err error, dec dispatchDecision, residual string, agent *app.OnBehalfOf, hold *sessionHold) (out error) {
	sensitivity := dec.sensitivity
	if hold != nil && hold.b.Sensitivity == queryengine.SensitivityConfined {
		sensitivity = queryengine.SensitivityConfined
	}
	if failedOwnClear(err) || !inst.ownClearRun(ctx, sensitivity, residual, agent, hold) {
		return err
	}
	return &ownClearError{err: err}
}
