package hostboot

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
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
