package chat

// The artefact in the journal, for a resume (ADR-0282 §SD7): its
// revisions live in memory, so the coordinator writes the head as an
// extension-private entry the projection skips (ADR-0308 §SD2,
// KindIgnorable) whenever it moved — at a turn's end, and at a revert, a
// rewind or a restore between turns. A resumed conversation takes the last
// such entry as its artefact (chat_resume.go). The screenshots are not
// written: their bytes are sealed to the process by design (ADR-0284), and
// a restored artefact has none.

import (
	"context"
	"strconv"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// artefactEntryName names the artefact's entries; the forms say what
// moved it.
const (
	artefactEntryName   = "artefact"
	artefactFormTurn    = "turn"
	artefactFormRevert  = "revert"
	artefactFormRewind  = "rewind"
	artefactFormRestore = "restore"
)

// syncArtefactJournal writes the artefact's head when it differs from the
// head last written — by revision or by text, since a rewind and a restore
// move the number without the text being new. A write that fails is
// logged, not the turn's failure: the record keeps the acts, and the
// snapshot is for a resume.
func (inst *coordinator) syncArtefactJournal(ctx context.Context, w *agentrun.Writer, turn string, form string) {
	art := inst.artefactOf()
	if art == nil || w == nil {
		return
	}
	n, text := art.head()
	inst.mu.Lock()
	// Nothing written yet stands for revision 0, the empty document every
	// artefact starts as: a turn that left it empty writes nothing.
	same := inst.artJournaled == n && inst.artJournaledText == text
	if !same {
		inst.artJournaled, inst.artJournaledText = n, text
	}
	inst.mu.Unlock()
	if same {
		return
	}
	e := agentrun.Entry{Kind: agentrun.KindIgnorable, Name: artefactEntryName, Form: form, Target: uint32(max(n, 0)), Body: text, Turn: turn,
		Label: inst.replyLabel(), WriterRun: w.Run().Id, WriterTurn: turn}
	if _, err := inst.append(ctx, w, e); err != nil {
		log.Warn().Err(err).Str("conversation", inst.conversation).Int("revision", n).Msg("chat: the artefact was not written to the journal")
	}
}

// artefactOfEntries is the artefact's head as the journal last wrote it:
// the revision and the text, and whether any was written.
func artefactOfEntries(entries []agentrun.Entry) (n int, text string, ok bool) {
	for _, e := range entries {
		if e.Kind == agentrun.KindIgnorable && e.Name == artefactEntryName {
			n, text, ok = int(e.Target), e.Body, true
		}
	}
	return
}

// artefactResumeLine says what a resume made of the artefact, for the
// person and the model.
func artefactResumeLine(n int) string {
	return "the artefact stands at revision " + strconv.Itoa(n) + ", restored from the journal; its screenshots did not survive the host"
}
