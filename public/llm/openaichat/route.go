package openaichat

// Route is one model endpoint's declared capabilities (ADR-0310 §SD4, §SD11):
// what the rest of the tier reads instead of guessing
// from the provider's name. The zero value is what this package did
// unconditionally before routes existed, so a client without a declared
// route behaves as before; a second value of any field exists so a route
// can differ, and nothing selects one until a profile does.
type Route struct {
	// Id names the route; it lands on the trail's call rows once the
	// profile record is taken up, and is empty until then.
	Id string
	// ContextTokens is the context size the route declares; zero declares
	// nothing, and the llm service's probe of the endpoint's model list is
	// the fallback.
	ContextTokens int32
	// ReasoningEcho says how the model's reasoning comes back.
	ReasoningEcho ReasoningEchoE
	// ImagesInToolResults says a tool result may carry images; false
	// means images travel in a user turn after the round's results, which
	// is where the chat puts them.
	ImagesInToolResults bool
	// LateSystem says how a system message after the first user turn
	// travels.
	LateSystem LateSystemE
	// Streaming says the route streams its replies; false means a reply
	// arrives whole.
	Streaming bool
}

// ReasoningEchoE is where a route puts the model's reasoning.
type ReasoningEchoE uint8

const (
	// ReasoningEchoFieldsAndTags: the reasoning_content and reasoning
	// fields, and inline <think> and <thought> blocks in the content, which
	// are taken out of it.
	ReasoningEchoFieldsAndTags ReasoningEchoE = iota
	// ReasoningEchoFields: the fields only; inline tags stay in the
	// content as the model wrote them.
	ReasoningEchoFields
)

// LateSystemE is how a system message after the first user turn travels.
type LateSystemE uint8

const (
	// LateSystemFold: the leading system messages join into one and every
	// later one moves into the next user turn as a marked note.
	LateSystemFold LateSystemE = iota
	// LateSystemInHistory: every system message is sent where it stands.
	LateSystemInHistory
)

// DefaultRoute is the route a client without a declaration runs under: what
// the package did unconditionally.
func DefaultRoute() (r Route) { return }

// WithRoute declares the client's route.
func WithRoute(r Route) Option {
	return func(inst *Client) { inst.route = r }
}

// Route is the client's declared route.
func (inst *Client) Route() (r Route) { return inst.route }

// messagesForWire are the messages as the route carries them.
func (inst Route) messagesForWire(msgs []Message) (out []Message) {
	if inst.LateSystem == LateSystemInHistory {
		return msgs
	}
	return foldLateSystemMessages(msgs)
}

// reasoningOf splits a reply's content and reasoning as the route echoes
// them: content without the inline blocks and every reasoning fragment
// joined, or the content as written with the fields alone.
func (inst Route) reasoningOf(content string, reasoningContent string, reasoning string) (clean string, thought string) {
	clean = content
	fields := joinReasoning(reasoningContent, reasoning)
	if inst.ReasoningEcho == ReasoningEchoFields {
		return clean, fields
	}
	var inline string
	clean, inline = extractInlineThought(content)
	return clean, joinReasoning(fields, inline)
}
