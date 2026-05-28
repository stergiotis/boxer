package bindings

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

type ApplicationState struct {
	idStack      *WidgetIdStack
	StateManager *StateManager
}

func NewApplicationState() *ApplicationState {
	return &ApplicationState{
		idStack:      NewWidgetIdStack(),
		StateManager: NewStateManager(),
	}
}
func (inst *ApplicationState) GetIdStack() *WidgetIdStack {
	return inst.idStack
}

func (inst *ApplicationState) StartServersideFrame() {
	metrics.Current.BeginFrame()
	inst.idStack.pushIdToStackLabeled(0, "<startServersideFrame>")
	PrepareNextFrame()
}
func (inst *ApplicationState) FinishServersideFrame() {
	End()
	inst.StateManager.Reset()
	metrics.Current.BeforeSync()
	inst.StateManager.Sync()
	if inst.idStack.Depth() != 1 {
		log.Warn().Msg("nesting id stack contains items at end of frame (incorrect nesting/id handling)")
	}
	inst.idStack.Reset()
	metrics.Current.EndFrame()
}

var CurrentApplicationState = NewApplicationState()
