package applink

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// The client half of the host's link services (ADR-0409 §SD8): a surface
// follows a click and asks for a hover over these subjects, declaring them
// in its manifest. The host attributes the surface by the bus envelope.

// FollowSubject is the request/reply subject a surface follows a clicked
// link on.
const FollowSubject = "runtime.link.follow"

// DescribeSubject is the request/reply subject a surface asks the host's
// reading of a link on, for its hover.
const DescribeSubject = "runtime.link.describe"

// FollowCap is the manifest capability Follow needs.
func FollowCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: FollowSubject, Direction: app.CapDirectionPub, Reason: reason}
}

// DescribeCap is the manifest capability Describe needs.
func DescribeCap(reason string) (c app.SubjectFilter) {
	return app.SubjectFilter{Pattern: DescribeSubject, Direction: app.CapDirectionPub, Reason: reason}
}

// Caps are both capabilities, for a surface that renders links.
func Caps(reason string) (c []app.SubjectFilter) {
	return []app.SubjectFilter{FollowCap(reason), DescribeCap(reason)}
}

// Author is the label of the text a link was found in (ADR-0311 §SD1):
// agentrun.LevelE and TierE as numbers, so a surface need not link the
// agent runtime.
type Author struct {
	Level uint8 `json:"level"`
	Tier  uint8 `json:"tier"`
}

// The integrity levels a surface names an author by (ADR-0311 §SD1).
const (
	LevelPerson      uint8 = 0
	LevelSystem      uint8 = 1
	LevelExternal    uint8 = 2
	LevelOwnModel    uint8 = 3
	LevelOtherModel  uint8 = 4
	levelUnknownText       = LevelExternal
)

// UnknownAuthor is the label of text whose author the surface cannot say:
// external content, as an unrecognised paste is (ADR-0311 §SD5).
var UnknownAuthor = Author{Level: levelUnknownText}

// FollowRequest is the payload on FollowSubject.
type FollowRequest struct {
	V      uint8  `json:"v"`
	Link   string `json:"link"`
	Author Author `json:"author"`
}

// The decisions a follow reports (ADR-0409 §SD9).
const (
	DecisionApplied  = "applied"
	DecisionStaged   = "staged"
	DecisionProposed = "proposed"
	DecisionConflict = "conflict"
	DecisionRefused  = "refused"
	// DecisionReached is a link with no call: the window opened or was
	// raised, and nothing more was asked.
	DecisionReached = "reached"
)

// FollowReply answers a FollowRequest. Reason is set when the link was not
// followed as written; Note says, in a line for the surface's action line,
// what happened.
type FollowReply struct {
	V        uint8  `json:"v"`
	Decision string `json:"decision"`
	Window   uint64 `json:"window,omitempty"`
	// Authorless is set when a minted link's grant no longer authorised
	// it and the authorless rules applied (ADR-0409 §SD6).
	Authorless bool   `json:"authorless,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Note       string `json:"note,omitempty"`
}

// DescribeRequest is the payload on DescribeSubject.
type DescribeRequest struct {
	V      uint8  `json:"v"`
	Link   string `json:"link"`
	Author Author `json:"author"`
}

// DescribeReply is the host's reading of a link: what a hover shows. A
// link the host or the codec refuses has Refused set and the rest as far
// as it was read.
type DescribeReply struct {
	V         uint8  `json:"v"`
	Display   string `json:"display,omitempty"`
	App       string `json:"app,omitempty"`
	Operation string `json:"operation,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Effect    string `json:"effect,omitempty"`
	Target    string `json:"target,omitempty"`
	// Args is the arguments as the codec read them, as JSON.
	Args         string `json:"args,omitempty"`
	Author       string `json:"author,omitempty"`
	Task         string `json:"task,omitempty"`
	Conversation string `json:"conversation,omitempty"`
	// Would says what a click would do now, as a FollowReply's Decision.
	Would   string `json:"would,omitempty"`
	Refused string `json:"refused,omitempty"`
}

// Text is the reply as hover lines.
func (inst DescribeReply) Text() (s string) {
	if inst.Display == "" && inst.Refused != "" {
		return "boxer link — does not open: " + inst.Refused
	}
	s = inst.Display
	if inst.Summary != "" {
		s += " — " + inst.Summary
	}
	if inst.Target != "" {
		s += "\n" + inst.Target
	}
	if inst.Args != "" && inst.Args != "{}" {
		s += "\nwith " + inst.Args
	}
	if inst.Effect != "" {
		s += "\neffect: " + inst.Effect
	}
	if inst.Author != "" {
		s += "\nwritten by: " + inst.Author
	}
	if inst.Task != "" {
		s += "\nunder the grant of " + inst.Task
		if inst.Conversation != "" {
			s += " (conversation " + inst.Conversation + ")"
		}
	}
	switch {
	case inst.Refused != "":
		s += "\ndoes not open: " + inst.Refused
	case inst.Would != "":
		s += "\na click: " + inst.Would
	}
	return
}

// followWait bounds a follow: an open waits for the window to load and the
// call to apply.
const followWait = 15 * time.Second

// Follow asks the host to follow a clicked link. It blocks on the bus
// round trip, so call it off the render goroutine.
func Follow(ctx context.Context, bus app.BusI, link string, author Author) (rep FollowReply, err error) {
	payload, err := buscodec.Encode(FollowRequest{V: 1, Link: link, Author: author})
	if err != nil {
		return
	}
	raw, err := app.RequestWithin(ctx, bus, followWait, FollowSubject, payload)
	if err != nil {
		err = eh.Errorf("applink: follow: %w", err)
		return
	}
	rep, err = buscodec.Decode[FollowReply](raw)
	return
}

// Describe asks the host's reading of a link, for a hover. It blocks on
// the bus round trip, so call it off the render goroutine.
func Describe(ctx context.Context, bus app.BusI, link string, author Author) (rep DescribeReply, err error) {
	payload, err := buscodec.Encode(DescribeRequest{V: 1, Link: link, Author: author})
	if err != nil {
		return
	}
	raw, err := app.RequestWithin(ctx, bus, 5*time.Second, DescribeSubject, payload)
	if err != nil {
		err = eh.Errorf("applink: describe: %w", err)
		return
	}
	rep, err = buscodec.Decode[DescribeReply](raw)
	return
}

// FollowLogged is Follow off the caller's goroutine, logging what came of
// it: for a surface with no status line of its own to report to.
//
// spawn starts the follow: an app passes the Go of its window's lifetime
// scope, or its own wrapper over it (ADR-0322), so the host's drain counts
// the bus round trip. A refused start — the scope retired, the window is
// leaving — follows nothing. A nil spawn follows nothing and says so.
func FollowLogged(bus app.BusI, logger zerolog.Logger, link string, author Author, spawn func(f func(ctx context.Context)) (started bool)) {
	if spawn == nil || bus == nil {
		logger.Warn().Msg("applink: no bus or no spawn given, the link was not followed")
		return
	}
	started := spawn(func(ctx context.Context) {
		rep, err := Follow(ctx, bus, link, author)
		switch {
		case err != nil:
			logger.Warn().Err(err).Msg("applink: the link was not followed")
		case rep.Decision == DecisionRefused:
			logger.Warn().Str("reason", rep.Reason).Msg("applink: the link was not followed")
		default:
			logger.Info().Str("note", rep.Note).Msg("applink: followed a link")
		}
	})
	if !started {
		logger.Debug().Msg("applink: the window is closing, the link was not followed")
	}
}
