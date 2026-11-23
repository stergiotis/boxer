package decision

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// OriginE is who started the act the question is about. The call site
// states it; the drawing does not depend on it, since the affirmative
// leads under either (ADR-0404 SD4).
type OriginE uint8

const (
	// OriginRequest: another party (a model, an agent, the harness, an app)
	// asks the person for something it may not do on its own.
	OriginRequest OriginE = iota
	// OriginConfirm: the person started the act and is asked whether to go
	// through with it.
	OriginConfirm
)

// RoleE is what an option does to the question.
type RoleE uint8

const (
	// RoleAffirm grants or goes through with what is asked. At most one
	// option has it.
	RoleAffirm RoleE = iota
	// RoleDecline refuses what is asked.
	RoleDecline
	// RoleOther answers the question some other way (Skip, Later).
	RoleOther
)

// Option is one button of the decision.
type Option struct {
	// Key identifies the option in [Result.Chosen] and keys its button's id.
	// Required and unique within the decision.
	Key string
	// Label is the button's text and accessible name. Required.
	Label string
	// Icon is a glyph drawn before the label, part of the button's text; ""
	// for none.
	Icon string
	// Tooltip is shown on hover; "" for none.
	Tooltip string
	// Role is what the option does to the question.
	Role RoleE
	// Destroys marks an option whose effect cannot be taken back. It is
	// drawn Danger, or DangerGhost for a RoleOther option.
	Destroys bool
	// Disabled draws the option but does not let it be chosen.
	Disabled bool
	// Why is the sentence that says why a disabled option cannot be chosen,
	// drawn under the options (keelson-ux C4). Required when Disabled.
	Why string
}

// Note is a sentence about the question itself: that it has gone stale,
// that what it asks cannot be granted, that its source is not trusted.
type Note struct {
	Text string
	// Tone colours the bar beside the note; the text stays body text.
	Tone styletokens.Tone
}

// Input is one frame's declaration of a decision.
type Input struct {
	// Ids is the host's id stack; the decision opens one scope under it.
	Ids *c.WidgetIdStack
	// ScopeKey tells two decisions under one host apart; "" takes a default.
	ScopeKey string
	// Origin is who started the act.
	Origin OriginE
	// Title is what is asked, as a sentence. Required.
	Title string
	// Subject is the thing it is asked about, in one line; "" for none.
	Subject string
	// Follows says what follows from answering, one sentence per entry.
	Follows []string
	// Notes are drawn after Follows.
	Notes []Note
	// Options are drawn in one row, in this order. At least one.
	Options []Option
	// Form draws what the answer needs and the person reads before
	// answering: inputs, or a bounded text the question is about. It is
	// drawn before the options, under a scope of its own on Ids. nil for
	// none.
	Form func()
	// Detail draws material that supports the answer (a diff, a preview,
	// the asker's stated reason). It is drawn after the options, under a
	// scope of its own on Ids. nil for none.
	Detail func()
	// Unframed leaves out the decision's own frame, for a host whose
	// container already frames it (a card, a modal).
	Unframed bool
}

// Result is what the frame's input did.
type Result struct {
	// Chosen is the key of the option clicked; "" when none was.
	Chosen string
	// Has is true when an option was clicked.
	Has bool
	// Err is a structurally broken Input, drawn in place of the decision.
	Err error
}

// Validate reports what makes the Input impossible to draw as a decision.
func (in Input) Validate() (err error) {
	switch {
	case in.Ids == nil:
		return eb.Build().Errorf("decision: Input.Ids is nil")
	case strings.TrimSpace(in.Title) == "":
		return eb.Build().Errorf("decision: Input.Title is empty")
	case in.Origin > OriginConfirm:
		return eb.Build().Uint8("origin", uint8(in.Origin)).Errorf("decision: unknown origin")
	case len(in.Options) == 0:
		return eb.Build().Str("title", in.Title).Errorf("decision: no options")
	}
	affirm := ""
	seen := make(map[string]bool, len(in.Options))
	for i, o := range in.Options {
		switch {
		case o.Key == "":
			return eb.Build().Int("option", i).Errorf("decision: an option has no key")
		case seen[o.Key]:
			return eb.Build().Str("key", o.Key).Errorf("decision: two options share a key")
		case strings.TrimSpace(o.Label) == "":
			return eb.Build().Str("key", o.Key).Errorf("decision: an option has no label")
		case o.Role > RoleOther:
			return eb.Build().Str("key", o.Key).Uint8("role", uint8(o.Role)).Errorf("decision: unknown option role")
		case o.Disabled && strings.TrimSpace(o.Why) == "":
			return eb.Build().Str("key", o.Key).Errorf("decision: a disabled option does not say why")
		case o.Role == RoleAffirm && affirm != "":
			return eb.Build().Str("key", o.Key).Str("other", affirm).Errorf("decision: two affirmative options")
		}
		seen[o.Key] = true
		if o.Role == RoleAffirm {
			affirm = o.Key
		}
	}
	return nil
}

// kindOf is the button kind of an option (ADR-0404 SD4). It reads the
// option alone, so the same answer is drawn the same in every question:
// a note beside it, whatever its tone, does not change it.
func kindOf(o Option) (kind c.ButtonKindE) {
	switch o.Role {
	case RoleAffirm:
		kind = c.ButtonKindPrimary
		if o.Destroys {
			kind = c.ButtonKindDanger
		}
	case RoleDecline:
		// Never weaker than a plain button.
		kind = c.ButtonKindSecondary
		if o.Destroys {
			kind = c.ButtonKindDanger
		}
	default:
		kind = c.ButtonKindGhost
		if o.Destroys {
			kind = c.ButtonKindDangerGhost
		}
	}
	return
}
