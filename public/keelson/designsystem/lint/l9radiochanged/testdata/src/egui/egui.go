// Stand-in for the egui2 Ctx + widget surface used by analysistest fixtures.
package egui

type RadioButtonFluid struct{ id uint64 }

type CheckboxFluid struct{ id uint64 }

type ResponseFlags uint64

func (RadioButtonFluid) SendRespVal(val *bool) (r ResponseFlags) { _ = val; return }
func (RadioButtonFluid) Send()                                   {}

func (CheckboxFluid) SendRespVal(val *bool) (r ResponseFlags) { _ = val; return }
func (CheckboxFluid) Send()                                   {}

func (ResponseFlags) HasChanged() (b bool)        { return }
func (ResponseFlags) HasPrimaryClicked() (b bool) { return }

type Ctx struct{}

func (Ctx) RadioButton(id uint64, sel int, label string) (r RadioButtonFluid) {
	_, _, _ = id, sel, label
	return
}

func (Ctx) Checkbox(id uint64, cur bool, label string) (c CheckboxFluid) {
	_, _, _ = id, cur, label
	return
}

var C = Ctx{}
