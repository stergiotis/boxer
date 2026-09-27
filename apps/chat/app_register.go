package chat

import (
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
)

// ManifestId is this app's identity — its Go import path (ADR-0026 id rule).
const ManifestId app.AppIdT = "github.com/stergiotis/boxer/apps/chat"

// manifest declares the two grants and nothing else (ADR-0265 §SD1): the
// app sends text to the host's model and may ask for its conversations to
// be kept; it reads nothing back.
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
	Caps: append(
		llm.ClientCaps("chat: send the conversation to the host's model"),
		llm.RetainCaps("chat: keep the conversation on boxer.facts where the host's BOXER_LLM_RETAIN allows (ADR-0264)")...,
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

func init() {
	err := app.DefaultRegistry.RegisterFactory(manifest, func() (a app.AppI, ctorErr error) {
		a = newApp()
		return
	})
	if err != nil {
		log.Warn().Err(err).Msg("chat: failed to register factory")
	}
}
