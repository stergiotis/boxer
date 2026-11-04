package agent

import (
	"context"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A call a model asked for names the intent entry written before it
// (ADR-0358 §SD7), as a model call names its request (ADR-0357). The rule
// rests on the call stating a model call: a coordinator driven by hand runs
// no model and keeps no journal, and its calls stay legal. A harness that
// leaves the model call out to escape the check gives up the join to the
// model on the trail, which shows.

// IntentCheckerI judges the intent entry a call names by key.
type IntentCheckerI interface {
	CheckIntent(ctx context.Context, sender string, key string, callKey string) (err error)
}

// StoreIntents is a checker over the run journal's store.
func StoreIntents(store agentrun.StoreI) IntentCheckerI { return storeIntents{store: store} }

type storeIntents struct{ store agentrun.StoreI }

func (inst storeIntents) CheckIntent(ctx context.Context, sender string, key string, callKey string) (err error) {
	if key == "" {
		return eh.Errorf("the call states a model call and names no intent entry; write an intent before the call leaves and send its key (ADR-0358)")
	}
	run, ordinal, ok := cell.ParseApprovalKey(key)
	if !ok {
		return eb.Build().Str("key", key).Errorf("the journal key is not <run>#<ordinal>")
	}
	r, found, err := inst.store.GetRun(ctx, run)
	if err != nil {
		return eb.Build().Uint64("run", run).Errorf("load the call's run: %w", err)
	}
	switch {
	case !found, r.OwnerApp != sender:
		return eb.Build().Uint64("run", run).Errorf("no such run")
	case r.State != agentrun.StateOpen:
		return eb.Build().Uint64("run", run).Str("state", r.State.String()).Errorf("the run is not open")
	}
	entries, err := inst.store.Entries(ctx, run, ordinal)
	if err != nil {
		return eb.Build().Uint64("run", run).Uint32("ordinal", ordinal).Errorf("load the intent entry: %w", err)
	}
	for _, e := range entries {
		if e.Ordinal != ordinal {
			continue
		}
		switch {
		case e.Kind != agentrun.KindIntent:
			return eb.Build().Str("kind", e.Kind.String()).Errorf("the entry is not an intent")
		case !intentCovers(e.Key, callKey):
			return eb.Build().Str("intent", e.Key).Str("call", callKey).Errorf("the intent is of another call")
		}
		return nil
	}
	return eb.Build().Uint64("run", run).Uint32("ordinal", ordinal).Errorf("no such entry")
}

// intentCovers says whether an intent under key intent covers a call under
// callKey: its own, or one the tool makes on its way, keyed beneath it.
func intentCovers(intent string, callKey string) bool {
	return callKey == intent || strings.HasPrefix(callKey, intent+"/")
}

// AcceptAnyIntent is a checker that accepts every call. It is for tests of
// what lies behind the check; a host wires StoreIntents.
type AcceptAnyIntent struct{}

// CheckIntent accepts the call.
func (AcceptAnyIntent) CheckIntent(context.Context, string, string, string) error { return nil }

// intentCheckWait bounds the checker's load.
const intentCheckWait = 10 * time.Second

// verifyIntent is why a call a model asked for may not go, empty when it may.
func (inst *Service) verifyIntent(sender string, req wireCall) (why string) {
	if req.ModelCall == "" {
		return
	}
	if inst.cfg.Journal == nil {
		return "no run journal is wired to the dispatcher, so a call a model asked for cannot be checked"
	}
	ctx, cancel := context.WithTimeout(context.Background(), intentCheckWait)
	defer cancel()
	if err := inst.cfg.Journal.CheckIntent(ctx, sender, req.Journal, req.Key); err != nil {
		return "the journal does not cover the call: " + err.Error()
	}
	return
}
