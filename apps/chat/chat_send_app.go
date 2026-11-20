package chat

// The window's side of ADR-0385: Add image, which reads the one file the
// person picks in the host's file dialog into the artefact's set; the
// choice of images for the next message, made in the Images tab and shown
// above the composer; and the images a message took with it, drawn under it.

import (
	"context"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The operations of the handle the file dialog's reply carries.
const (
	opHandleRead  = ".read"
	opHandleClose = ".close"
)

const (
	// sentThumbBox bounds a sent image's thumbnail under its message, in
	// logical points.
	sentThumbBox uint32 = 96

	tipImageAdd    = "Add a PNG or JPEG file to the artefact's images, chosen in the host's file dialog. The artefact keeps its pixels, turned upright, and nothing else of the file: no location, time or device, and not its name. The model sees the pixels only as Pixels allows, or when you send the image with a message."
	tipImageSend   = "Send this image to the model with your next message, for that turn. It is recorded as shown by you, and what was sent cannot be called back."
	tipImageUnsend = "Take this image out of your next message. It stays in the artefact."
)

var (
	atomsImageAdd = c.Atoms().Text(icons.PhPlus + " Add image").Keep()
	// Worded apart from the composer's Send, which scenes click by name.
	atomsImageSend   = c.Atoms().Text(icons.PhPaperPlaneRight + " Attach to next message").Keep()
	atomsImageUnsend = c.Atoms().Text(icons.PhCheck + " Attached to next message").Keep()
)

// pickFile opens the host's file dialog and reads the one file chosen,
// through the handle the reply carries, which is closed whether or not the
// read worked: no authority outlives the gesture (ADR-0026 §SD7). picked is
// false when the person closed the dialog. It blocks on the bus: call it off
// the frame.
func pickFile(bus app.BusI) (data []byte, fileName string, picked bool, err error) {
	reply, err := bus.RequestWithTimeout(fsbroker.SubjectDialogRead, nil, fsbroker.DialogTimeout)
	if err != nil {
		return nil, "", false, eh.Errorf("the file dialog did not answer: %w", err)
	}
	dr, err := fsbroker.UnmarshalDialogReply(reply)
	if err != nil {
		return nil, "", false, eh.Errorf("the file dialog's reply was not understood: %w", err)
	}
	if !dr.Granted {
		return nil, "", false, nil
	}
	data, err = bus.RequestWithTimeout(dr.HandleSubjectPrefix+opHandleRead, nil, fsbroker.HandleOpTimeout)
	_, _ = bus.RequestWithTimeout(dr.HandleSubjectPrefix+opHandleClose, nil, fsbroker.HandleOpTimeout)
	if err != nil {
		return nil, "", false, eh.Errorf("%s could not be read: %w", dr.DisplayName, err)
	}
	return data, dr.DisplayName, true, nil
}

// addImage is Add image (ADR-0385 §SD1): the file dialog, then the file as
// a revision of the person's. It has a job of its own, so a dialog left open
// holds up no copy and no open.
func (inst *App) addImage(art *artefact) {
	bus := inst.bus
	if bus == nil {
		inst.setNote("add an image: this host gives the app no bus, so no file dialog", true)
		return
	}
	ok := inst.adding.Start(inst.tasks, bgjob.Spec{Kind: "chat-add-image", Title: "add an image to the artefact"}, func(ctx context.Context) (note *string, err error) {
		data, fileName, picked, err := pickFile(bus)
		if err != nil {
			return nil, err
		}
		n := "no file was chosen"
		if !picked {
			return &n, nil
		}
		name, _, err := art.addUpload(data, fileName)
		if err != nil {
			// What is neither format may be the broker's refusal, which
			// arrives in the place of the content.
			if refused, rerr := fsbroker.UnmarshalDialogReply(data); rerr == nil && !refused.Granted && refused.Reason != "" {
				return nil, eh.Errorf("%s was not added: %s", fileName, refused.Reason)
			}
			return nil, eh.Errorf("%s was not added: %w", fileName, err)
		}
		n = "added " + fileName + " to the artefact's images as " + name
		return &n, nil
	})
	if !ok {
		inst.setNote("add an image: the file dialog is already open", true)
	}
}

// drainAdding lands Add image's outcome on the status line.
func (inst *App) drainAdding() {
	if note, _, ok := inst.adding.TakeResult(); ok {
		inst.setNote(*note, false)
	} else if snap := inst.adding.Snapshot(); snap.State == bgjob.StateFailed {
		inst.adding.Invalidate()
		text := "the image was not added"
		if snap.Err != nil {
			text = snap.Err.Error()
		}
		inst.setNote(text, true)
	}
}

// sendOffer says why no image can go with a message now, "" when one can:
// the Pixels setting, under the host's cap, is the gate (ADR-0385 §SD4).
func (inst *App) sendOffer() string {
	p := inst.pixelPolicy()
	switch {
	case p.level != agent.PixelsNone && p.reaches():
		return ""
	case p.level != agent.PixelsNone:
		return "Only to a local model is on and this conversation's model is off this machine: no image is sent with a message."
	}
	if capLevel, _ := pixelsCap(); capLevel == agent.PixelsNone {
		return "This host lets no image's pixels reach a model: none is sent with a message."
	}
	return "Pixels is at Metadata only: raise it under Settings to send an image with a message."
}

// sendChoice is what is chosen for the next message, as the set holds it
// now: a name that left the set, or whose bytes are gone, leaves the choice.
func (inst *App) sendChoice() (out []artImage) {
	if len(inst.sendNames) == 0 || inst.conv == nil || inst.conv.art == nil {
		inst.sendNames = nil
		return nil
	}
	art := inst.conv.art
	set := art.headImages()
	kept := inst.sendNames[:0]
	for _, name := range inst.sendNames {
		if i := findImage(set, name); i >= 0 && !set[i].purged && art.store.has(set[i].hash) {
			kept = append(kept, name)
			out = append(out, set[i])
		}
	}
	inst.sendNames = kept
	return
}

// toggleSend puts name into the next message, or takes it out.
func (inst *App) toggleSend(name string) {
	if i := slices.Index(inst.sendNames, name); i >= 0 {
		inst.sendNames = slices.Delete(inst.sendNames, i, i+1)
		return
	}
	inst.sendNames = append(inst.sendNames, name)
}

// chooseSent chooses again what went with a message that is back in the
// composer (ADR-0385 §SD6); sendChoice keeps what the set still holds.
func (inst *App) chooseSent(sent []sentRef) {
	inst.sendNames = nil
	for _, s := range sent {
		inst.sendNames = append(inst.sendNames, s.name)
	}
}

// renderSendRow is the line above the composer naming what goes with the
// next message; each name is a button that takes it out.
func (inst *App) renderSendRow() {
	choice := inst.sendChoice()
	if len(choice) == 0 {
		return
	}
	for range c.HorizontalWrapped().KeepIter() {
		weak(icons.PhImage + " with this message:")
		for _, e := range choice {
			for range c.HoverText(tipImageUnsend).KeepIter() {
				if c.Button(inst.ids.PrepareStr("unsend-"+e.name), c.Atoms().Text(e.name+" "+icons.PhX).Keep()).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
					name := e.name
					inst.later = append(inst.later, func() { inst.toggleSend(name) })
				}
			}
		}
	}
	if why := inst.sendOffer(); why != "" {
		weak(why)
	}
}

// renderSent is what went with a message of the person's: the names, and
// the thumbnails of those the artefact still holds.
func (inst *App) renderSent(e *entry) {
	if len(e.sent) == 0 {
		return
	}
	names := make([]string, 0, len(e.sent))
	for _, s := range e.sent {
		names = append(names, s.name)
	}
	weak(icons.PhImage + " sent with this message: " + strings.Join(names, ", "))
	art := inst.conv.art
	if art == nil {
		return
	}
	for range c.HorizontalWrapped().KeepIter() {
		for _, s := range e.sent {
			t, ok := art.store.thumbnail(s.hash)
			if !ok || len(t.Pixels) == 0 {
				continue
			}
			id := inst.ids.PrepareStr("sent-" + s.hash).Derive()
			if inst.artView.thumbs == nil {
				inst.artView.thumbs = c.NewImageVersionTracker[uint64]()
			}
			px := inst.artView.thumbs.PixelsToSendFor(id, id, 1, t.Pixels)
			c.Image(c.MakeAbsoluteIdHighEntropy(id), t.WidthPx, t.HeightPx, 1, uint8(c.FitAspectMaxE), sentThumbBox, sentThumbBox,
				uint8(c.FilterLinearE), c.TintNoneRgba, px).Send()
		}
	}
}

// sentRefs are names as the transcript keeps them, with the bytes each
// names now.
func sentRefs(set []artImage, names []string) (out []sentRef) {
	for _, name := range names {
		if i := findImage(set, name); i >= 0 {
			out = append(out, sentRef{name: name, hash: set[i].hash})
		}
	}
	return
}
