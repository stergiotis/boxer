package chat

// Links in what the chat renders (ADR-0409, proposed, which takes over
// ADR-0347 §SD5's per-surface claim): a message body or the artefact may
// carry a boxer: link or an alias such as mdedit:// — written by the model,
// or pasted by the person — and a click asks the host to follow it, naming
// who wrote the text it was in. The host decides what the click may do; the
// line above the composer says what it did.

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// linkClaims is the link claim a render site passes: every link the host
// follows when the window has a bus to ask over, nil otherwise, so a link
// the window could not follow stays a hyperlink rather than a dead click —
// the same rule play, tally and the help center apply.
func (inst *App) linkClaims() func(url string) bool {
	if inst.bus == nil {
		return nil
	}
	return applink.Claimed
}

// linkHover is the hover of a link in text author wrote: the host's
// reading of it, asked for off the render goroutine (ADR-0409 §SD8); nil
// without a bus.
func (inst *App) linkHover(author applink.Author) func(url string) string {
	if inst.bus == nil {
		return nil
	}
	if inst.linkHovers == nil {
		spawn := func(f func(ctx context.Context)) bool {
			if inst.scope == nil {
				go f(context.Background())
				return true
			}
			return inst.scope.Go(f)
		}
		inst.linkHovers = applink.NewHoverSet(inst.bus, spawn)
	}
	return inst.linkHovers.For(author)
}

// authorOfEntry is the label of a message's text (ADR-0311 §SD1): the
// person's own words, external content when images went with them, and
// model output for a reply.
func authorOfEntry(e *entry) applink.Author {
	switch {
	case e.speaker == speakerUser && len(e.sent) > 0:
		return applink.Author{Level: applink.LevelExternal}
	case e.speaker == speakerUser:
		return applink.Author{Level: applink.LevelPerson}
	}
	return applink.Author{Level: applink.LevelOwnModel}
}

// artefactAuthor is the label of the artefact's text: model output, as the
// journal labels the artefact's head.
var artefactAuthor = applink.Author{Level: applink.LevelOwnModel}

// followLinks follows every claimed link clicked in a rendered document
// this frame.
func (inst *App) followLinks(res markdown.Result, author applink.Author) {
	for _, l := range res.Links {
		inst.followLink(l.URL, author)
	}
}

// followLink follows one link, off the render thread, reporting on the
// action line.
func (inst *App) followLink(link string, author applink.Author) {
	bus := inst.bus
	inst.runAction("chat-follow-link", "follow a link", func(ctx context.Context) (note string, err error) {
		rep, err := applink.Follow(ctx, bus, link, author)
		switch {
		case err != nil:
			return
		case rep.Decision == applink.DecisionRefused, rep.Decision == applink.DecisionConflict:
			// Not followed: the action line says so as a failure.
			return "", eh.New(rep.Note)
		}
		return rep.Note, nil
	}, nil)
}
