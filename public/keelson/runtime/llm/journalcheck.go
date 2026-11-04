package llm

import (
	"context"
	"encoding/json/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A model call names the journal entry written before it (ADR-0357): the
// caller appends a request entry whose name is the call's content digest,
// sends the entry's key with the call, and the service refuses a call whose
// key names no such entry in an open run of the sender's. What it shows is
// that the request was recorded before it left, with what content; that the
// journal was the request's only source is the harness's to hold.

// JournalKey is the key a call carries for its request entry, in the form
// of an approval key.
func JournalKey(run uint64, ordinal uint32) string { return cell.ApprovalKey(run, ordinal) }

// JournalCheckerI judges the request entry a call names by key. requestRun
// is the run the request names, zero for none. It returns the entry's tagged
// id, the reference the call's row carries, or why the entry does not cover
// the call.
type JournalCheckerI interface {
	CheckRequest(ctx context.Context, sender string, requestRun uint64, key string, digest string) (entryId uint64, err error)
}

// StoreJournal is a checker over the run journal's store.
func StoreJournal(store agentrun.StoreI) JournalCheckerI { return storeJournal{store: store} }

type storeJournal struct{ store agentrun.StoreI }

func (inst storeJournal) CheckRequest(ctx context.Context, sender string, requestRun uint64, key string, digest string) (entryId uint64, err error) {
	if key == "" {
		return 0, eh.Errorf("the call names no journal entry; write a request entry before the call leaves and send its key (ADR-0357)")
	}
	run, ordinal, ok := cell.ParseApprovalKey(key)
	if !ok {
		return 0, eb.Build().Str("key", key).Errorf("the journal key is not <run>#<ordinal>")
	}
	if requestRun != 0 && requestRun != run {
		return 0, eb.Build().Uint64("key", run).Uint64("request", requestRun).Errorf("the journal key is of another run than the request names")
	}
	r, found, err := inst.store.GetRun(ctx, run)
	if err != nil {
		return 0, eb.Build().Uint64("run", run).Errorf("load the call's run: %w", err)
	}
	switch {
	case !found, r.OwnerApp != sender:
		// Another app's run is not found, as the journal's service answers.
		return 0, eb.Build().Uint64("run", run).Errorf("no such run")
	case r.State != agentrun.StateOpen:
		return 0, eb.Build().Uint64("run", run).Str("state", r.State.String()).Errorf("the run is not open")
	}
	entries, err := inst.store.Entries(ctx, run, ordinal)
	if err != nil {
		return 0, eb.Build().Uint64("run", run).Uint32("ordinal", ordinal).Errorf("load the request entry: %w", err)
	}
	for _, e := range entries {
		if e.Ordinal != ordinal {
			continue
		}
		switch {
		case e.Kind != agentrun.KindRequest:
			return 0, eb.Build().Str("kind", e.Kind.String()).Errorf("the entry is not a request")
		case e.Name != digest:
			return 0, eh.Errorf("the call is not the request the entry recorded: its content differs")
		}
		return e.EntryId, nil
	}
	return 0, eb.Build().Uint64("run", run).Uint32("ordinal", ordinal).Errorf("no such entry")
}

// AcceptAnyJournal is a checker that accepts every call, naming no entry.
// It is for tests of what lies behind the check; a host wires StoreJournal.
type AcceptAnyJournal struct{}

// CheckRequest accepts the call.
func (AcceptAnyJournal) CheckRequest(context.Context, string, uint64, string, string) (uint64, error) {
	return 0, nil
}

// requestContent is what a call asks of the model: what its digest is over.
// Who sends it, on whose behalf and under which approval are the call's
// routing, and an approved resend keeps its digest.
type requestContent struct {
	Messages       []openaichat.Message       `json:"messages"`
	Temperature    *float32                   `json:"temperature,omitempty"`
	MaxTokens      int32                      `json:"max_tokens,omitempty"`
	Seed           *int64                     `json:"seed,omitempty"`
	Stop           []string                   `json:"stop,omitempty"`
	EnableThinking bool                       `json:"enable_thinking,omitempty"`
	Tools          []openaichat.Tool          `json:"tools,omitempty"`
	ToolChoice     string                     `json:"tool_choice,omitempty"`
	ResponseFormat *openaichat.ResponseFormat `json:"response_format,omitempty"`
	Extra          []byte                     `json:"extra,omitempty"`
}

// digest is the content's digest, over the bus codec's encoding: the form
// that reaches the service, so both sides compute over the same values.
func (inst requestContent) digest() (d string, err error) {
	b, err := buscodec.Encode(inst)
	if err != nil {
		return "", eh.Errorf("llm: encode the request's content: %w", err)
	}
	return agentrun.ContentDigest(string(b)), nil
}

func (inst wireRequest) content() requestContent {
	return requestContent{Messages: inst.Messages, Temperature: inst.Temperature, MaxTokens: inst.MaxTokens, Seed: inst.Seed, Stop: inst.Stop,
		EnableThinking: inst.EnableThinking, Tools: inst.Tools, ToolChoice: inst.ToolChoice, ResponseFormat: inst.ResponseFormat, Extra: inst.Extra}
}

// encodeExtra is the wire form of a request's provider-specific members:
// deterministic, since the content digest covers it.
func encodeExtra(extra map[string]any) (b []byte, err error) {
	if len(extra) == 0 {
		return nil, nil
	}
	if b, err = json.Marshal(extra, json.Deterministic(true)); err != nil {
		return nil, eh.Errorf("llm: encode extra: %w", err)
	}
	return
}

// ContentDigest is the digest a request entry records under its name
// (ADR-0357 §SD3). It covers the messages, the tools and the parameters, and
// must be taken from the request as it will be sent.
func (inst Request) ContentDigest() (digest string, err error) {
	extra, err := encodeExtra(inst.Extra)
	if err != nil {
		return "", err
	}
	return requestContent{Messages: inst.Messages, Temperature: inst.Temperature, MaxTokens: inst.MaxTokens, Seed: inst.Seed, Stop: inst.Stop,
		EnableThinking: inst.EnableThinking, Tools: inst.Tools, ToolChoice: inst.ToolChoice, ResponseFormat: inst.ResponseFormat, Extra: extra}.digest()
}

// verifyJournal says whether the entry the request names covers it, and
// returns the entry's id for the call's row.
func (inst *Service) verifyJournal(req wireRequest, sender app.AppIdT) (entryId uint64, err error) {
	if inst.cfg.Journal == nil {
		inst.journalUnwired.Do(func() {
			inst.log.Error().Msg("llm: no run journal is wired to the model service; every completion is refused")
		})
		return 0, eh.Errorf("no run journal is wired to the model service")
	}
	digest, err := req.content().digest()
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(inst.base, approvalWait)
	defer cancel()
	return inst.cfg.Journal.CheckRequest(ctx, string(sender), req.Run, req.Journal, digest)
}
