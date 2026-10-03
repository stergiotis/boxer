package chat

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// ManifestId is this app's identity — its Go import path (ADR-0026 id rule).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/chat"

// manifest declares the grants the chat uses (ADR-0265 §SD1): the
// app sends text to the host's model and may ask for its conversations to
// be kept; it reads nothing back. With Apps on it works in shared windows,
// and on request it publishes its statistics and opens play on them, on a
// code block's SQL, or on a failed call's record, opens mdedit on the
// conversation, and copies a message to the clipboard.
var manifest = app.Manifest{
	Id:           ManifestId,
	Version:      "0.1.0",
	Display:      "Chat",
	Title:        "Chat",
	Summary:      "Talk to the host's model; kept on boxer.facts where the host allows",
	Icon:         icons.PhChatCenteredDots,
	Topics:       []app.TopicT{app.TopicData},
	Keywords:     []string{"chat", "llm", "model", "conversation", "assistant"},
	Kind:         app.KindApp,
	Surface:      app.SurfaceWindowed,
	SurfaceHints: app.SurfaceHints{PreferredWidth: 760, PreferredHeight: 720},
	Caps: append(append(
		llm.ClientCaps("chat: send the conversation to the host's model"),
		llm.RetainCaps("chat: keep the conversation on boxer.facts where the host's BOXER_LLM_RETAIN allows (ADR-0264)")...),
		// The coordinator (ADR-0269): the model works in windows the person
		// shares, each call checked by the host's dispatcher. The person
		// registers the app as a coordinator (BOXER_AGENT_COORDINATORS).
		append(agent.ClientCaps("chat: work in windows the person shares with the model"),
			app.SubjectFilter{Pattern: adhocdata.SubjectPublish, Direction: app.CapDirectionPub,
				Reason: "chat: publish the window's token and answer statistics as ad-hoc datasets for Open in play (ADR-0240)"},
			app.SubjectFilter{Pattern: windowhost.OpenSubject, Direction: app.CapDirectionPub,
				Reason: "chat: Open in play — a play window on the statistics, a reply's SQL or a failed call's record — and Open in mdedit on the conversation (ADR-0135)"},
			app.SubjectFilter{Pattern: clipboardbroker.SubjectWrite, Direction: app.CapDirectionPub,
				Reason: "chat: copy a message, a code block or a failure's details to the clipboard"})...,
	),
}

// DraftSeed puts text in the composer of a new window (ADR-0009 seed
// variable, play's BOXER_PLAY_SQL shape): a headless scene cannot focus an
// empty unnamed text input, so it seeds the draft and presses Send.
var DraftSeed = env.NewString(env.Spec{
	Name:        "BOXER_CHAT_DRAFT",
	Description: "text in the chat app's composer when a window opens; for scenes and demos",
	Category:    env.CategoryE("boxer-chat"),
})

// AppsSeed turns Apps on in a new window (ADR-0009 seed variable): the
// model may then ask the person for windows to work in. A chat the person
// registered as a coordinator starts with Apps on without it.
var AppsSeed = env.NewBool(env.Spec{
	Name:        "BOXER_CHAT_APPS",
	Default:     "false",
	Description: "turn on Apps in a new chat window: the model may ask the person for windows to work in (ADR-0269); a chat listed in BOXER_AGENT_COORDINATORS starts with Apps on anyway",
	Category:    env.CategoryE("boxer-chat"),
})

// registeredCoordinator says the person registered the chat as a
// coordinator (BOXER_AGENT_COORDINATORS) — the intent Apps serves, so a new
// window starts with it on.
func registeredCoordinator() (yes bool) {
	for _, n := range agent.ParseCoordinators(agent.CoordinatorsEnv.Get()) {
		if n == string(ManifestId) || n == ManifestId.SubjectAlias() {
			return true
		}
	}
	return
}

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("chat: failed to register factory")
	}
}
