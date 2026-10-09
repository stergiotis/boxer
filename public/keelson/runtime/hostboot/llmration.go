package hostboot

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/moderator"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// llmWindowClass answers the model service's queue (ADR-0300 §SD6) from
// the window host's last completed frame: the focused window first, then
// the shown ones, then the collapsed, the not yet shown and the closed.
type llmWindowClass struct{ host *windowhost.Inst }

func (inst llmWindowClass) WindowClass(instance uint64) (c ration.ClassE) {
	if uint64(inst.host.DesktopInfo().ActiveKey) == instance {
		return ration.ClassFocused
	}
	for _, w := range inst.host.WindowInfos() {
		if uint64(w.Key) != instance {
			continue
		}
		if w.Geom.Shown && !w.Geom.Collapsed {
			return ration.ClassShown
		}
		break
	}
	return ration.ClassBackground
}

// llmAsker puts a moderator's question to the person in the agent
// dispatcher's dialog (ADR-0300 §SD9), the host's one place for a decision
// the person owes.
type llmAsker struct{ svc *agent.Service }

func (inst llmAsker) AskPerson(ctx context.Context, q llm.Question) (granted bool, decided bool, err error) {
	return inst.svc.AskPerson(ctx, q.Moderator, q.Text, q.Rule)
}

// withModerator lists the built-in moderator among the model service's
// moderators when it is on (ADR-0302 §SD1); the agent dispatcher takes the
// same list.
func withModerator(cfg llm.Config) (out llm.Config) {
	out = cfg
	if moderator.Enabled.Get() && !slices.Contains(out.Moderators, string(moderator.AppId)) {
		out.Moderators = append(slices.Clone(out.Moderators), string(moderator.AppId))
	}
	return
}

// bootModerator starts the built-in moderator after the model service and
// the agent dispatcher it acts through.
func (rt *Runtime) bootModerator() {
	if !moderator.Enabled.Get() {
		return
	}
	logger := rt.opts.Log
	if rt.LLM == nil {
		logger.Warn().Msg("moderator: BOXER_MODERATOR is set but the llm service is off; no moderator")
		return
	}
	cfg := moderator.DefaultConfig()
	cfg.Trail = rt.Trail
	svc, err := moderator.NewService(rt.Bus, logger, cfg)
	if err != nil {
		logger.Warn().Err(err).Msg("moderator: start failed; no moderator")
		return
	}
	rt.Moderator = svc
	rt.cleanups = append(rt.cleanups, svc.Close)
	if rt.Agent == nil {
		logger.Warn().Msg("moderator: the agent dispatcher is off; the moderator slows and denies but cannot stop tasks")
	}
	logger.Info().Msg("moderator: watching model calls and agent actions (ADR-0302)")
}
