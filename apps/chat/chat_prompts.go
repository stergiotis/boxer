package chat

import (
	"embed"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The chat's prompt book (ADR-0310 §SD6 and §SD11): every
// word the host puts in the model's context on its own account is a
// document under prompts/, one section each — the system prompt's parts in
// the order the model reads them, and the notes the host adds mid-turn,
// which are volatile and never part of the stable head. The book is
// registered with promptbook, so keelson('llm_prompts') lists it; the chat
// reads it from the embedded corpus itself, so the sections are there
// before any init runs. The rendered sections and the tool registry hash
// to one fingerprint, promptFingerprint, which the profile's fingerprint
// folds in (chat_profile.go) and every run row and call row carries
// (ADR-0308 §SD3, ADR-0310 §SD7).

//go:embed prompts
var promptsFS embed.FS

// promptBookId names the book; keelson('llm_calls').purpose joins it as
// `chat/<slug>`.
const promptBookId = "chat"

func init() {
	if err := promptbook.Register(promptBookId, help.MustSub(promptsFS, "prompts")); err != nil {
		log.Warn().Err(err).Msg("chat: failed to register the prompt book")
	}
}

// promptSection is one section of the system prompt, and which
// conversations carry it.
type promptSection struct {
	slug string
	when func(apps bool, questions bool, artefact bool) bool
}

// promptSections are the system prompt's sections in the order the model
// reads them; promptNotes the volatile sections, last.
var (
	promptSections = []promptSection{
		{slug: "coordinator", when: func(apps bool, _ bool, _ bool) bool { return apps }},
		{slug: "questions", when: func(_ bool, questions bool, _ bool) bool { return questions }},
		{slug: "artefact", when: func(_ bool, _ bool, artefact bool) bool { return artefact }},
	}
	promptNotes = []string{"last-round", "unattended", "summariser"}
)

// chatPrompts parses the embedded book once: slug to body. A document that
// does not parse is a programming error the corpus gate test catches.
var chatPrompts = sync.OnceValue(func() (texts map[string]string) {
	defs, errs := promptbook.ParseBook(promptBookId, help.MustSub(promptsFS, "prompts"))
	if len(errs) > 0 {
		panic("chat: the prompt book does not parse: " + errs[0].Error())
	}
	texts = make(map[string]string, len(defs))
	for _, d := range defs {
		texts[d.Slug] = d.System
	}
	return
})

// promptText is the book's section by slug.
func promptText(slug string) (s string) {
	s, ok := chatPrompts()[slug]
	if !ok {
		panic("chat: no prompt section " + slug)
	}
	return
}

// The notes the host adds to the model's context mid-turn.
var (
	// lastRoundNote is the host's word to the model on the last round.
	lastRoundNote = promptText("last-round")
	// unattendedNote tells the model the host granted access in the
	// person's place (ADR-0298): what fits the settings is approved at
	// once, and what is left to the person may wait for hours.
	unattendedNote = promptText("unattended")
	// summariserNote is the host's instruction to the summariser when a
	// conversation is compacted (ADR-0316).
	summariserNote = promptText("summariser")
	// coordinatorPrompt is the system message of a conversation with Apps
	// on.
	coordinatorPrompt = promptText("coordinator")
)

// systemPrompt is the first message of a conversation with Apps or
// Questions on.
func systemPrompt(apps bool, questions bool) (s string) {
	return systemPromptOf(apps, questions, false)
}

// systemPromptOf is the first message of a conversation with Apps,
// Questions or the artefact on: each one's section, in the book's order.
func systemPromptOf(apps bool, questions bool, artefact bool) (s string) {
	parts := make([]string, 0, len(promptSections))
	for _, sec := range promptSections {
		if sec.when(apps, questions, artefact) {
			parts = append(parts, promptText(sec.slug))
		}
	}
	return strings.Join(parts, "\n\n")
}

// promptFingerprint is the digest of the host's own words to the model:
// the book's sections in declared order, the notes, and every tool of the
// registry as the model sees it. The same build renders the same
// fingerprint; a changed section or tool changes it.
func promptFingerprint() string {
	sections := make([]string, 0, len(promptSections)+len(promptNotes))
	for _, sec := range promptSections {
		sections = append(sections, sec.slug)
	}
	sections = append(sections, promptNotes...)
	tools := make([]openaichat.Tool, 0, len(coordinatorTools))
	for i := range coordinatorTools {
		tools = append(tools, coordinatorTools[i].tool())
	}
	return fingerprintOf(sections, promptText, tools)
}

// fingerprintOf is promptFingerprint over the given sections and tools.
func fingerprintOf(sections []string, text func(slug string) string, tools []openaichat.Tool) string {
	h := blake3.New(32, nil)
	for _, slug := range sections {
		_, _ = h.Write([]byte("section\x00" + slug + "\x00" + text(slug) + "\x00"))
	}
	for _, t := range tools {
		_, _ = h.Write([]byte("tool\x00" + t.Name + "\x00" + t.Description + "\x00" + string(t.Parameters) + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
